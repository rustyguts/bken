"""Extract a 16 kHz mono WAV per video.

Whisper and virtually every audio-event model want mono 16 kHz, so we
decode once and cache on disk. Downstream stages read the cached WAV
instead of re-decoding the container every time.

The cached filename is `<video_id>.wav` — short, stable, and cheap to
look up. If you ever want to keep alternate samplings, add them as
siblings (`<id>.48k.wav`) rather than new tables.
"""

from __future__ import annotations

import logging
import subprocess
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
from typing import Callable, Optional

from rich.console import Console

from . import config, db

log = logging.getLogger(__name__)
console = Console()


def audio_path_for(video_id: int) -> Path:
    return config.AUDIO_DIR / f"{video_id}.wav"


def extract_audio(video_path: Path, out_path: Path, overwrite: bool = False) -> None:
    if out_path.exists() and not overwrite:
        return
    out_path.parent.mkdir(parents=True, exist_ok=True)
    # -vn: drop video. -ac 1: mono. -ar 16000: 16 kHz. pcm_s16le: standard WAV.
    cmd = [
        "ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
        "-i", str(video_path),
        "-vn",
        "-ac", str(config.AUDIO_CHANNELS),
        "-ar", str(config.AUDIO_SAMPLE_RATE),
        "-c:a", "pcm_s16le",
        str(out_path),
    ]
    subprocess.run(cmd, check=True)


def _extract_one(video_id: int, video_path: str, force: bool) -> bool:
    """Worker: ffmpeg + DB flag flip. Owns its own SQLite connection."""
    out = audio_path_for(video_id)
    extract_audio(Path(video_path), out, overwrite=force)
    conn = db.connect()
    try:
        conn.execute(
            "UPDATE video SET audio_done=1, updated_at=datetime('now') WHERE id=?",
            (video_id,),
        )
    finally:
        conn.close()
    return True


def extract_pending(
    force: bool = False,
    workers: int = 4,
    on_done: Optional[Callable[[int, bool], None]] = None,
) -> int:
    """Extract audio for every video whose audio_done flag is 0.

    `on_done(video_id, ok)` fires once per video as work completes — wire
    a Rich progress bar to it from the CLI.
    """
    config.ensure_dirs()
    with db.db() as conn:
        rows = conn.execute(
            "SELECT id, path FROM video WHERE audio_done = 0 OR ? = 1",
            (1 if force else 0,),
        ).fetchall()
    if not rows:
        return 0

    n = 0
    if workers <= 1:
        for row in rows:
            ok = _safe_extract(row["id"], row["path"], force)
            n += 1 if ok else 0
            if on_done:
                on_done(row["id"], ok)
        return n

    with ThreadPoolExecutor(max_workers=workers, thread_name_prefix="audio") as ex:
        futures = {
            ex.submit(_safe_extract, row["id"], row["path"], force): row
            for row in rows
        }
        for fut in as_completed(futures):
            row = futures[fut]
            ok = fut.result()
            n += 1 if ok else 0
            if on_done:
                on_done(row["id"], ok)
    return n


def _safe_extract(video_id: int, path: str, force: bool) -> bool:
    try:
        return _extract_one(video_id, path, force)
    except subprocess.CalledProcessError as e:
        log.error("ffmpeg failed for video_id=%s path=%s: %s", video_id, path, e)
    except Exception as e:  # noqa: BLE001
        log.error("audio extract failed for video_id=%s: %s", video_id, e)
    return False
