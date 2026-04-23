"""Score videos and surface variable-length candidate clips.

Replaces the old fixed-30s sliding-window scorer. The actual signal /
segmentation work lives in `density.py`; this module is just the glue
that:

  1. Pulls the video's audio events + transcript out of the DB.
  2. Calls `density.build_density` → `density.multi_scale_segments` to
     get variable-length regions.
  3. Snaps each region's edges to nearby silence gaps for nicer cuts.
  4. **Diffs** the new regions against existing `candidate_clip` rows
     (greedy IoU match ≥ 0.5) and applies the diff:
        - matched: UPDATE start/end/score/features/transcript, set active=1.
          Crucially leaves `user_rating`, `llm_*`, `clip_path`,
          `thumb_path`, `title`, and `source` untouched.
        - unmatched new: INSERT (active=1).
        - unmatched existing: UPDATE active=0, but only for `source='auto'`.
          Manual moments are never deactivated by re-scoring.
      If the active set changes, clear `llm_rank` for the video and reset
      `video.rank_done=0` so the LLM re-orders. Per-clip
      `llm_title/desc/contents/tags` survive because they describe the
      moment, not the ordering.
"""

from __future__ import annotations

import json
import logging
from dataclasses import asdict

from rich.console import Console

from . import config, db, density

log = logging.getLogger(__name__)
console = Console()

WEIGHTS = density.WEIGHTS
KEYWORDS = density.KEYWORDS
KEYWORD_RE = density.KEYWORD_RE


class Window:
    """Legacy fixed-window placeholder — superseded by `density.Region`."""

    def __init__(self, start: float, end: float, text: str = "") -> None:
        self.start = start
        self.end = end
        self.text = text
        self.laugh = 0.0
        self.shout = 0.0
        self.cheer = 0.0
        self.gun = 0.0
        self.loud = 0.0
        self.talk = 0.0
        self.kw = 0
        self.spk = 0.0

    def score(self) -> float:
        return (
            WEIGHTS["Shout"]    * self.shout
            + WEIGHTS["Laughter"] * self.laugh
            + WEIGHTS["Cheer"]    * self.cheer
            + WEIGHTS["kw"]       * self.kw
            + WEIGHTS["LoudSpike"]* self.loud
            + WEIGHTS["talk_base"]* self.talk
            + WEIGHTS["talk_dens"]* self.spk
            + WEIGHTS["Gunfire"]  * self.gun
        )


def _non_max_suppress(windows, min_gap: float = 15.0, k: int = 30):
    def overlap(a0, a1, b0, b1):
        return max(0.0, min(a1, b1) - max(a0, b0))

    ranked = sorted(windows, key=lambda w: w.score(), reverse=True)
    kept: list = []
    for w in ranked:
        if any(overlap(w.start - min_gap, w.end + min_gap, kk.start, kk.end) > 0 for kk in kept):
            continue
        kept.append(w)
        if len(kept) >= k:
            break
    return sorted(kept, key=lambda w: w.start)


def _segments_for(conn, video_id: int) -> list[dict]:
    rows = conn.execute(
        "SELECT start_s, end_s, text FROM transcript_segment WHERE video_id=? ORDER BY start_s",
        (video_id,),
    ).fetchall()
    return [dict(r) for r in rows]


def _events_for(conn, video_id: int) -> list[dict]:
    rows = conn.execute(
        "SELECT start_s, end_s, label, score FROM audio_event WHERE video_id=? ORDER BY start_s",
        (video_id,),
    ).fetchall()
    return [dict(r) for r in rows]


def _vision_frames_for(conn, video_id: int) -> list[dict]:
    rows = conn.execute(
        "SELECT ts_s, sampled, caption FROM vision_frame WHERE video_id=? ORDER BY ts_s",
        (video_id,),
    ).fetchall()
    return [dict(r) for r in rows]


def _vision_ocr_for(conn, video_id: int) -> list[dict]:
    rows = conn.execute(
        "SELECT ts_s, text, area_frac FROM vision_ocr WHERE video_id=? ORDER BY ts_s",
        (video_id,),
    ).fetchall()
    return [dict(r) for r in rows]


def _vision_dets_for(conn, video_id: int) -> list[dict]:
    rows = conn.execute(
        "SELECT ts_s, label FROM vision_detection WHERE video_id=? ORDER BY ts_s",
        (video_id,),
    ).fetchall()
    return [dict(r) for r in rows]


def _transcript_text_for_region(segs: list[dict], start: float, end: float) -> str:
    parts = []
    for s in segs:
        if s["end_s"] < start or s["start_s"] > end:
            continue
        if s.get("text"):
            parts.append(s["text"].strip())
    text = " ".join(parts).strip()
    return text[:4000]


def _existing_clips(conn, video_id: int) -> list[dict]:
    """All auto clip rows for a video (manual moments excluded from diff)."""
    rows = conn.execute(
        """SELECT id, start_s, end_s, user_rating, llm_title, llm_desc,
                  llm_contents, llm_tags, clip_path, thumb_path, active
             FROM candidate_clip WHERE video_id=? AND source='auto'""",
        (video_id,),
    ).fetchall()
    return [dict(r) for r in rows]


def score_video(video_id: int, *, reset: bool = False) -> int:
    """Recompute candidate clips for a video. Returns count of active clips after."""
    with db.db() as conn:
        row = conn.execute(
            "SELECT duration_s, vision_done FROM video WHERE id=?", (video_id,)
        ).fetchone()
        if not row or not row["duration_s"]:
            return 0
        duration = float(row["duration_s"])

        segs = _segments_for(conn, video_id)
        evts = _events_for(conn, video_id)

        vf = vo = vd = None
        if config.VISION_SCORE_ENABLED and row["vision_done"]:
            vf = _vision_frames_for(conn, video_id)
            vo = _vision_ocr_for(conn, video_id)
            vd = _vision_dets_for(conn, video_id)

        I = density.build_density(
            duration_s=duration,
            audio_events=evts,
            transcript_segments=segs,
            sigma=config.DENSITY_SMOOTH_SIGMA,
            vision_frames=vf,
            vision_ocr=vo,
            vision_detections=vd,
            vision_enabled=config.VISION_SCORE_ENABLED,
        )

        regions = density.multi_scale_segments(
            I,
            scales=config.DENSITY_SCALES,
            gap_tol=config.DENSITY_GAP_TOLERANCE,
            min_len=int(config.CLIP_MIN_SECONDS),
            max_len=int(config.CLIP_MAX_SECONDS),
            top_n=config.TOP_N_CANDIDATES,
        )

        snapped = [
            density.snap_to_silence(r, segs, window_s=config.CLIP_SNAP_WINDOW)
            for r in regions
        ]
        new_regions = [r for r in snapped if r.score > 0.01]

        for r in new_regions:
            r.features["transcript_chars"] = len(
                _transcript_text_for_region(segs, r.start, r.end)
            )

        if reset:
            conn.execute("DELETE FROM candidate_clip WHERE video_id=? AND source='auto'", (video_id,))
            existing: list[dict] = []
        else:
            existing = _existing_clips(conn, video_id)

        matched, unmatched_new, unmatched_existing = density.match_existing(
            new_regions, existing, iou_threshold=0.5
        )

        prev_active_ids = {row["id"] for row in existing if row["active"]}
        new_active_ids: set[int] = set()

        for new, old in matched:
            transcript_text = _transcript_text_for_region(segs, new.start, new.end)
            conn.execute(
                """UPDATE candidate_clip
                   SET start_s=?, end_s=?, score=?, features=?, transcript=?, active=1
                   WHERE id=?""",
                (
                    float(new.start), float(new.end), float(new.score),
                    json.dumps(new.features), transcript_text, old["id"],
                ),
            )
            new_active_ids.add(old["id"])

        for new in unmatched_new:
            transcript_text = _transcript_text_for_region(segs, new.start, new.end)
            cur = conn.execute(
                """INSERT INTO candidate_clip
                   (video_id, start_s, end_s, score, features, transcript, active, source)
                   VALUES (?,?,?,?,?,?,1,'auto')""",
                (
                    video_id,
                    float(new.start), float(new.end), float(new.score),
                    json.dumps(new.features), transcript_text,
                ),
            )
            new_active_ids.add(cur.lastrowid)

        for old in unmatched_existing:
            if old["active"]:
                conn.execute(
                    "UPDATE candidate_clip SET active=0 WHERE id=?",
                    (old["id"],),
                )

        if new_active_ids != prev_active_ids and new_active_ids:
            conn.execute(
                "UPDATE candidate_clip SET llm_rank=NULL WHERE video_id=? AND active=1",
                (video_id,),
            )
            conn.execute(
                "UPDATE video SET rank_done=0 WHERE id=?", (video_id,)
            )

        conn.execute(
            "UPDATE video SET score_done=1, updated_at=datetime('now') WHERE id=?",
            (video_id,),
        )
        return len(new_active_ids)


def score_pending(force: bool = False, *, reset: bool = False) -> dict[int, int]:
    """Score every video that's transcribed + event-detected and not yet scored."""
    with db.db() as conn:
        if config.VISION_SCORE_ENABLED:
            rows = conn.execute(
                """SELECT id FROM video
                   WHERE transcribe_done = 1 AND events_done = 1
                     AND (vision_done = 0 OR score_done = 0 OR ? = 1)""",
                (1 if force else 0,),
            ).fetchall()
        else:
            rows = conn.execute(
                """SELECT id FROM video
                   WHERE transcribe_done = 1 AND events_done = 1
                     AND (score_done = 0 OR ? = 1)""",
                (1 if force else 0,),
            ).fetchall()
    return {row["id"]: score_video(row["id"], reset=reset) for row in rows}
