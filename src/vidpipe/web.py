"""FastAPI JSON API + Vue SPA host for the vidpipe admin panel.

Clean split:

  /                    — index.html from the Vite build (Vue SPA)
  /assets/*            — hashed Vue build assets (js/css)
  /api/clips           — filter + sort the candidate_clip table (JSON)
  /api/games           — sidebar roll-up: one row per game with clip counts
  /api/rate/{id}?rating=N — set or clear (N=0) a 1..5 star rating
  /thumb/{id}.jpg      — lazy ffmpeg thumbnail render (cached on disk)
  /clip/{id}.mp4       — clip file, Range requests passed through for
                         HTML `<video>` scrubbing. `?download=1` adds a
                         Content-Disposition: attachment header with a
                         friendly "<game>_<date>_<mmss>_clip<id>.mp4" name.

The frontend is a separate Bun/Vite project in `frontend/`. Its build
drops into `src/vidpipe/static/app/` so the whole UI ships inside the
Python package.
"""

from __future__ import annotations

import json
import re
from pathlib import Path

from fastapi import FastAPI, HTTPException
from fastapi.responses import FileResponse, Response, JSONResponse
from fastapi.staticfiles import StaticFiles

from . import config, db, thumbs

ROOT = Path(__file__).resolve().parent
APP_DIR = ROOT / "static" / "app"     # vite build output lives here

app = FastAPI(title="vidpipe")


# ──────────────────────────────────────────────────────────────────────────
# Data helpers
# ──────────────────────────────────────────────────────────────────────────

SORT_OPTIONS = {
    "llm_rank":       ("c.llm_rank ASC NULLS LAST, c.score DESC",     "Best (LLM rank)"),
    "recorded_desc":  ("v.recorded_at DESC, c.start_s ASC",           "Newest recording"),
    "recorded_asc":   ("v.recorded_at ASC, c.start_s ASC",            "Oldest recording"),
    "rating_desc":    ("c.user_rating DESC NULLS LAST, c.score DESC", "Highest user rating"),
    "rating_asc":     ("c.user_rating ASC NULLS LAST, c.score DESC",  "Lowest user rating"),
    "score_desc":     ("c.score DESC",                                "Heuristic score"),
}

DEFAULT_SORT = "llm_rank"


def _clip_to_dict(row) -> dict:
    tags = []
    if row["llm_tags"]:
        try:
            tags = json.loads(row["llm_tags"])
        except json.JSONDecodeError:
            pass
    path = row["clip_path"] or ""
    return {
        "id":          row["id"],
        "video_id":    row["video_id"],
        "start_s":     row["start_s"],
        "end_s":       row["end_s"],
        "duration":    row["end_s"] - row["start_s"],
        "score":       row["score"],
        "llm_rank":    row["llm_rank"],
        "title":       row["llm_title"]
                       or f"Clip @ {int(row['start_s']//60)}:{int(row['start_s']%60):02d}",
        "reason":      row["llm_desc"] or "",
        "contents":    row["llm_contents"] or "",
        "transcript":  row["transcript"] or "",
        "tags":        tags,
        "rating":      row["user_rating"],
        "game":        row["game"] or "Unknown",
        "recorded_at": row["recorded_at"] or "",
        "video_name":  Path(row["video_path"]).stem if row["video_path"] else "",
        "thumb_url":   f"/thumb/{row['id']}.jpg",
        "video_url":   f"/clip/{row['id']}.mp4",
        "download_url":f"/clip/{row['id']}.mp4?download=1",
        "clip_name":   Path(path).name if path else "",
    }


def _query_clips(
    *,
    game: str | None = None,
    sort: str = DEFAULT_SORT,
    min_rating: int = 0,
) -> list[dict]:
    order_clause, _ = SORT_OPTIONS.get(sort, SORT_OPTIONS[DEFAULT_SORT])
    wheres = ["c.clip_path IS NOT NULL", "c.active = 1"]
    params: list = []
    if game:
        wheres.append("v.game = ?")
        params.append(game)
    if min_rating > 0:
        wheres.append("COALESCE(c.user_rating, 0) >= ?")
        params.append(min_rating)
    sql = f"""
        SELECT c.*, v.game, v.recorded_at, v.path AS video_path
        FROM candidate_clip c
        JOIN video v ON v.id = c.video_id
        WHERE {' AND '.join(wheres)}
        ORDER BY {order_clause}
    """
    with db.db() as conn:
        rows = conn.execute(sql, params).fetchall()
    return [_clip_to_dict(r) for r in rows]


def _game_counts() -> list[dict]:
    with db.db() as conn:
        rows = conn.execute(
            """SELECT v.game AS game,
                      COUNT(c.id) AS n,
                      AVG(c.user_rating) AS avg_rating,
                      SUM(CASE WHEN c.user_rating IS NOT NULL THEN 1 ELSE 0 END) AS rated
               FROM video v
               LEFT JOIN candidate_clip c
                      ON c.video_id = v.id AND c.clip_path IS NOT NULL AND c.active = 1
               WHERE v.game IS NOT NULL
               GROUP BY v.game
               ORDER BY n DESC, v.game ASC"""
        ).fetchall()
    return [dict(r) for r in rows]


# ──────────────────────────────────────────────────────────────────────────
# API
# ──────────────────────────────────────────────────────────────────────────


@app.get("/api/clips")
def api_clips(
    game: str | None = None,
    sort: str = DEFAULT_SORT,
    min_rating: int = 0,
):
    clips = _query_clips(game=game, sort=sort, min_rating=min_rating)
    return {
        "clips": clips,
        "sort_options": [{"key": k, "label": v[1]} for k, v in SORT_OPTIONS.items()],
        "default_sort": DEFAULT_SORT,
    }


@app.get("/api/games")
def api_games():
    return {"games": _game_counts()}


@app.post("/api/rate/{clip_id}")
def api_rate(clip_id: int, rating: int = 0):
    if rating < 0 or rating > 5:
        raise HTTPException(422, "rating out of range")
    with db.db() as conn:
        exists = conn.execute(
            "SELECT 1 FROM candidate_clip WHERE id=?", (clip_id,)
        ).fetchone()
        if not exists:
            raise HTTPException(404, "clip not found")
        new = None if rating == 0 else rating
        conn.execute("UPDATE candidate_clip SET user_rating=? WHERE id=?", (new, clip_id))
        row = conn.execute(
            """SELECT c.*, v.game, v.recorded_at, v.path AS video_path
               FROM candidate_clip c JOIN video v ON v.id=c.video_id
               WHERE c.id=?""", (clip_id,),
        ).fetchone()
    return _clip_to_dict(row)


# ──────────────────────────────────────────────────────────────────────────
# Media
# ──────────────────────────────────────────────────────────────────────────


@app.get("/thumb/{clip_id}.jpg")
def thumb(clip_id: int):
    path = thumbs.ensure_thumb(clip_id)
    if not path or not path.exists():
        # tiny placeholder so <img> doesn't show a broken icon mid-render
        return Response(
            content=b"\xff\xd8\xff\xdb\x00C\x00\x08" + b"\x00" * 60,
            media_type="image/jpeg",
        )
    return FileResponse(str(path), media_type="image/jpeg")


_FILENAME_SAFE = re.compile(r"[^A-Za-z0-9._-]+")


def _download_filename(row) -> str:
    """Build a friendly filename for the saved-as dialog.

    Pattern: ``<game>_<YYYY-MM-DD>_<MMSS>_clip<id>.mp4`` — sortable, tells
    the user which game/recording the clip came from at a glance, and
    avoids the internal ``clip_0042_xxxxx-xxxxx.mp4`` numbering.
    """
    game = (row["game"] or "clip").strip() or "clip"
    date = (row["recorded_at"] or "")[:10] or "undated"
    start = int(row["start_s"] or 0)
    mmss = f"{start // 60:02d}{start % 60:02d}"
    raw = f"{game}_{date}_{mmss}_clip{row['id']}.mp4"
    return _FILENAME_SAFE.sub("_", raw)


@app.get("/clip/{clip_id}.mp4")
def clip_mp4(clip_id: int, download: bool = False):
    with db.db() as conn:
        row = conn.execute(
            """SELECT c.clip_path, c.id, c.start_s, v.game, v.recorded_at
                 FROM candidate_clip c
                 JOIN video v ON v.id = c.video_id
                WHERE c.id = ?""",
            (clip_id,),
        ).fetchone()
    if not row or not row["clip_path"]:
        raise HTTPException(404, "clip not found")
    path = config.resolve_data(row["clip_path"])
    if not path.exists():
        raise HTTPException(404, "clip not found")
    headers = {"Accept-Ranges": "bytes"}
    if download:
        # `attachment` forces a Save As dialog instead of inline playback.
        # Quote the filename so spaces/punctuation that survived sanitization
        # don't break the header.
        fname = _download_filename(row)
        headers["Content-Disposition"] = f'attachment; filename="{fname}"'
    return FileResponse(str(path), media_type="video/mp4", headers=headers)


# ──────────────────────────────────────────────────────────────────────────
# SPA host
# ──────────────────────────────────────────────────────────────────────────

# Mount the built Vue app. `html=True` makes / serve index.html and enables
# SPA history-mode fallback. The mount is declared *last* so it doesn't
# shadow /api/ or /thumb/ etc.

if APP_DIR.exists():
    app.mount("/", StaticFiles(directory=str(APP_DIR), html=True), name="spa")
else:
    @app.get("/")
    def _no_build():
        return JSONResponse(
            {
                "error": "frontend not built",
                "hint": "run `cd frontend && bun install && bun run build`",
            },
            status_code=503,
        )
