"""Scan a directory for video files and index them with ffprobe metadata.

Idempotent: re-scanning the same directory updates rows in place and
re-probes files whose size or mtime changed. The SHA1 prefix hash (first
16 MB) guards against unrelated files that happened to land at the same
size+mtime.

Parallel ingest: `ingest_paths_parallel` farms out ffprobe + hashing across
a thread pool (default 8 workers). Each worker owns its own SQLite
connection; SQLite WAL mode + a 30s busy timeout (set in `db.connect`)
serializes the tiny INSERT/UPDATEs without contention.
"""

from __future__ import annotations

import hashlib
import json
import logging
import re
import shutil
import subprocess
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timezone
from fnmatch import fnmatch
from pathlib import Path
from typing import Callable, Iterable, Iterator, Optional

from rich.console import Console

from . import db

log = logging.getLogger(__name__)
console = Console()

VIDEO_EXTS = {".mp4", ".mkv", ".mov", ".webm", ".avi", ".flv", ".m4v"}

HASH_PREFIX_BYTES = 16 * 1024 * 1024  # 16 MB is enough to disambiguate

# Rusty's recorder writes "YYYY-MM-DD_HH-MM-SS" (or "YYYY-MM-DD HH-MM-SS").
# We pull the original recording time from the filename because mtimes get
# reset whenever the archive is moved between disks or copied to the NAS.
_FILENAME_DT_RE = re.compile(r"(\d{4})-(\d{2})-(\d{2})[ _](\d{2})-(\d{2})-(\d{2})")

# Status strings returned by `_ingest_one` — surfaced in CLI summary counts.
STATUS_INSERTED = "inserted"
STATUS_UPDATED = "updated"
STATUS_UNCHANGED = "unchanged"
STATUS_ERRORED = "errored"
ALL_STATUSES = (STATUS_INSERTED, STATUS_UPDATED, STATUS_UNCHANGED, STATUS_ERRORED)


def _parse_recorded_at(path: Path, mtime: float) -> str:
    m = _FILENAME_DT_RE.search(path.stem)
    if m:
        y, mo, d, h, mi, se = (int(x) for x in m.groups())
        try:
            return datetime(y, mo, d, h, mi, se).isoformat()
        except ValueError:
            pass
    return datetime.fromtimestamp(mtime, tz=timezone.utc).isoformat()


def _game_from_path(path: Path) -> str:
    """Directory containing the video file, cleaned up for display.

    e.g. `/mnt/.../archive/Grand_Theft_Auto_V/x.mp4` → "Grand Theft Auto V"
    """
    raw = path.parent.name
    return raw.replace("_", " ").strip()


def _sha1_prefix(path: Path, n: int = HASH_PREFIX_BYTES) -> str:
    h = hashlib.sha1()
    with path.open("rb") as f:
        h.update(f.read(n))
    return h.hexdigest()


def _sha1_full(path: Path) -> str:
    """Compute the full-file SHA1 using the fastest available method.

    On Linux ``sha1sum`` is preferred — it streams the file in optimised C
    without loading the whole thing into memory. Falls back to Python's
    ``hashlib`` with an 8 MB chunk size.
    """
    if shutil.which("sha1sum"):
        r = subprocess.run(
            ["sha1sum", str(path)],
            capture_output=True, text=True, check=True,
        )
        return r.stdout.split()[0]
    h = hashlib.sha1()
    with path.open("rb") as f:
        while chunk := f.read(8 * 1024 * 1024):
            h.update(chunk)
    return h.hexdigest()


def _ffprobe(path: Path) -> dict:
    r = subprocess.run(
        [
            "ffprobe",
            "-v", "error",
            "-show_format",
            "-show_streams",
            "-of", "json",
            str(path),
        ],
        capture_output=True,
        text=True,
        check=True,
    )
    return json.loads(r.stdout)


def _extract_fields(probe: dict) -> dict:
    fmt = probe.get("format", {})
    streams = probe.get("streams", [])
    v = next((s for s in streams if s.get("codec_type") == "video"), None)
    a = next((s for s in streams if s.get("codec_type") == "audio"), None)

    fps = None
    if v and v.get("avg_frame_rate"):
        num, _, den = v["avg_frame_rate"].partition("/")
        try:
            fps = float(num) / float(den) if float(den) else None
        except (ValueError, ZeroDivisionError):
            fps = None

    return {
        "duration_s": float(fmt.get("duration", 0)) or None,
        "width": v.get("width") if v else None,
        "height": v.get("height") if v else None,
        "fps": fps,
        "video_codec": v.get("codec_name") if v else None,
        "audio_codec": a.get("codec_name") if a else None,
        "audio_channels": a.get("channels") if a else None,
        "audio_rate": int(a["sample_rate"]) if a and a.get("sample_rate") else None,
    }


def iter_videos(root: Path) -> Iterator[Path]:
    """Yield every video file under `root`, recursively, in sorted order."""
    for p in sorted(root.rglob("*")):
        if p.is_file() and p.suffix.lower() in VIDEO_EXTS:
            yield p


def collect_paths(
    target: Path,
    single_file: bool = False,
    pattern: Optional[str] = None,
) -> list[Path]:
    """Resolve a CLI target (file or directory) into a concrete file list."""
    if single_file or target.is_file():
        return [target]
    paths = list(iter_videos(target))
    if pattern:
        paths = [p for p in paths if fnmatch(p.name, pattern)]
    return paths


def ingest_path(
    target: Path,
    single_file: bool = False,
    workers: int = 8,
    pattern: Optional[str] = None,
    on_done: Optional[Callable[[Path, str], None]] = None,
    library_id: Optional[int] = None,
) -> dict[str, int]:
    """Index a directory (recursive) or a single file. Returns counts dict."""
    db.init_db()
    paths = collect_paths(target, single_file=single_file, pattern=pattern)
    return ingest_paths_parallel(paths, workers=workers, on_done=on_done, library_id=library_id)


def ingest_paths_parallel(
    paths: Iterable[Path],
    workers: int = 8,
    on_done: Optional[Callable[[Path, str], None]] = None,
    library_id: Optional[int] = None,
) -> dict[str, int]:
    """Run ingest on `paths` across `workers` threads. Reports per-file via `on_done`."""
    paths = list(paths)
    counts = {s: 0 for s in ALL_STATUSES}
    if not paths:
        return counts

    # workers == 1 avoids spinning up a pool — useful for debugging and tests.
    if workers <= 1:
        for path in paths:
            status = _ingest_safe(path, library_id=library_id)
            counts[status] += 1
            if on_done:
                on_done(path, status)
        return counts

    with ThreadPoolExecutor(max_workers=workers, thread_name_prefix="ingest") as ex:
        futures = {ex.submit(_ingest_safe, p, library_id): p for p in paths}
        for fut in as_completed(futures):
            path = futures[fut]
            status = fut.result()  # _ingest_safe never raises
            counts[status] += 1
            if on_done:
                on_done(path, status)
    return counts


def _ingest_safe(path: Path, library_id: Optional[int] = None) -> str:
    """Worker entry point — owns its own SQLite connection and never raises."""
    try:
        conn = db.connect()
        try:
            return _ingest_one(conn, path, library_id=library_id)
        finally:
            conn.close()
    except subprocess.CalledProcessError as e:
        log.warning("ffprobe failed on %s: %s", path, e)
    except Exception as e:  # noqa: BLE001  — log + swallow to keep the pool draining
        log.warning("ingest failed on %s: %s", path, e)
    return STATUS_ERRORED


def _ingest_one(conn, path: Path, library_id: Optional[int] = None) -> str:
    """Insert/update a row for `path`. Returns one of `ALL_STATUSES`."""
    stat = path.stat()
    row = conn.execute(
        "SELECT id, size_bytes, mtime FROM video WHERE path = ?", (str(path),)
    ).fetchone()
    unchanged = row and row["size_bytes"] == stat.st_size and abs(row["mtime"] - stat.st_mtime) < 1
    if unchanged:
        # If this file belongs to a library and was marked missing, restore it.
        if library_id is not None:
            conn.execute(
                "UPDATE video SET missing = 0, library_id = ?, updated_at = datetime('now') WHERE id = ?",
                (library_id, row["id"]),
            )
        return STATUS_UNCHANGED

    probe = _ffprobe(path)
    fields = _extract_fields(probe)
    sha1p = _sha1_prefix(path)
    sha1f = _sha1_full(path)
    game = _game_from_path(path)
    recorded_at = _parse_recorded_at(path, stat.st_mtime)

    if row:
        conn.execute(
            """UPDATE video SET
                size_bytes=?, mtime=?, sha1_prefix=?, sha1_full=?,
                duration_s=?, width=?, height=?, fps=?,
                video_codec=?, audio_codec=?, audio_channels=?, audio_rate=?,
                game=?, recorded_at=?, library_id=?, missing=0,
                -- Any stage that depended on the *old* file must re-run
                audio_done=0, transcribe_done=0, events_done=0, score_done=0, rank_done=0,
                updated_at=datetime('now')
               WHERE id=?""",
            (
                stat.st_size, stat.st_mtime, sha1p, sha1f,
                fields["duration_s"], fields["width"], fields["height"], fields["fps"],
                fields["video_codec"], fields["audio_codec"],
                fields["audio_channels"], fields["audio_rate"],
                game, recorded_at, library_id,
                row["id"],
            ),
        )
        return STATUS_UPDATED

    conn.execute(
        """INSERT INTO video (
            path, size_bytes, mtime, sha1_prefix, sha1_full, library_id,
            duration_s, width, height, fps,
            video_codec, audio_codec, audio_channels, audio_rate,
            game, recorded_at
           ) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)""",
        (
            str(path), stat.st_size, stat.st_mtime, sha1p, sha1f, library_id,
            fields["duration_s"], fields["width"], fields["height"], fields["fps"],
            fields["video_codec"], fields["audio_codec"],
            fields["audio_channels"], fields["audio_rate"],
            game, recorded_at,
        ),
    )
    return STATUS_INSERTED


def list_videos() -> list[dict]:
    with db.db() as conn:
        rows = conn.execute(
            "SELECT * FROM video ORDER BY path"
        ).fetchall()
    return [dict(r) for r in rows]


def get_video(video_id: int) -> Optional[dict]:
    with db.db() as conn:
        row = conn.execute(
            "SELECT * FROM video WHERE id=?", (video_id,)
        ).fetchone()
    return dict(row) if row else None


def reset_video_stages(video_id: int, stages: Iterable[str]) -> None:
    """Clear stage-done flags so the corresponding stages re-run.

    Valid stages: audio, transcribe, events, score, rank.
    """
    flag_map = {
        "audio": "audio_done",
        "transcribe": "transcribe_done",
        "events": "events_done",
        "vision": "vision_done",
        "score": "score_done",
        "rank": "rank_done",
    }
    cols = []
    for s in stages:
        if s not in flag_map:
            raise ValueError(f"unknown stage: {s}")
        cols.append(f"{flag_map[s]}=0")
    if not cols:
        return
    with db.db() as conn:
        if "vision" in stages:
            conn.execute("DELETE FROM vision_frame WHERE video_id = ?", (video_id,))
            # cascade deletes handle children; also invalidate score since vision feeds it
            if "score" not in stages:
                cols.append("score_done=0")
        sql = f"UPDATE video SET {', '.join(cols)}, updated_at=datetime('now') WHERE id=?"
        conn.execute(sql, (video_id,))


def reset_all_stages(from_stage: str) -> int:
    """Clear stage-done flags for ALL videos so the pipeline re-runs from
    `from_stage` onward. Returns the number of videos affected.

    `from_stage` is one of: audio, transcribe, events, vision, score, rank.
    Everything at or after the chosen stage gets reset.
    """
    flag_map = {
        "audio": "audio_done",
        "transcribe": "transcribe_done",
        "events": "events_done",
        "vision": "vision_done",
        "score": "score_done",
        "rank": "rank_done",
    }
    if from_stage not in flag_map:
        raise ValueError(f"unknown stage: {from_stage}")

    stage_order = ["audio", "transcribe", "events", "vision", "score", "rank"]
    start_idx = stage_order.index(from_stage)
    stages_to_reset = stage_order[start_idx:]

    cols = [f"{flag_map[s]}=0" for s in stages_to_reset]
    with db.db() as conn:
        if "vision" in stages_to_reset:
            conn.execute("DELETE FROM vision_frame")
            if "score" not in stages_to_reset:
                cols.append("score_done=0")
        sql = f"UPDATE video SET {', '.join(cols)}, updated_at=datetime('now')"
        conn.execute(sql)
        count = conn.execute("SELECT COUNT(*) c FROM video").fetchone()["c"]
    return count


def delete_video(video_id: int) -> None:
    """Remove a video row and any cached audio. Cascades to events / segments / clips."""
    from . import audio as audio_mod, config

    with db.db() as conn:
        conn.execute("DELETE FROM video WHERE id=?", (video_id,))

    audio = audio_mod.audio_path_for(video_id)
    if audio.exists():
        audio.unlink()
    clip_dir = config.CLIPS_DIR / f"video_{video_id}"
    if clip_dir.exists():
        for f in clip_dir.iterdir():
            f.unlink()
        clip_dir.rmdir()
