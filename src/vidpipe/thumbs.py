"""Thumbnail generation for clip cards.

Rendered lazily on first request and cached to disk at
`data/thumbs/clip_<id>.jpg`. We seek into the (already-cut) clip rather
than the source video because the clip is small, local, and seeking is
cheap; seeking a ~10 GB AV1 source file over NFS would be slow.
"""

from __future__ import annotations

import logging
import subprocess
from pathlib import Path

from . import config, db

log = logging.getLogger(__name__)

THUMBS_DIR = config.DATA / "thumbs"
THUMB_WIDTH = 640  # downscale from 1920 — ~180 kB jpegs is fine for the grid


def thumb_path_for(clip_id: int) -> Path:
    return THUMBS_DIR / f"clip_{clip_id:06d}.jpg"


def _render(clip_file: Path, out: Path, seek_s: float) -> None:
    out.parent.mkdir(parents=True, exist_ok=True)
    # Source files in the archive are mis-tagged with NTSC-era colour
    # metadata (color_space=smpte170m, color_trc=bt470m) on content that's
    # actually 1080p bt709 — most recorder/encoder tools do this. HTML <video>
    # largely ignores those tags for HD and renders correctly; mjpeg takes
    # them at face value and produces washed-out stills.
    #
    # Fix: override the input tags with `setparams` (the content is bt709),
    # then let the scale filter do an explicit matrix conversion to bt601
    # because JPEG's YCbCr is BT.601 full-range by spec.
    video_filter = (
        "setparams=color_primaries=bt709:color_trc=bt709:colorspace=bt709:range=tv,"
        f"scale={THUMB_WIDTH}:-2:flags=lanczos:"
        "in_color_matrix=bt709:out_color_matrix=bt601,"
        "format=yuvj420p"
    )
    cmd = [
        "ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
        "-ss", f"{seek_s:.3f}",
        "-i", str(clip_file),
        "-frames:v", "1",
        "-vf", video_filter,
        "-q:v", "3",
        str(out),
    ]
    subprocess.run(cmd, check=True)


def ensure_thumb(clip_id: int) -> Path | None:
    """Return the on-disk path for a clip's thumbnail, rendering if needed.
    Returns None if the clip has no mp4 on disk yet.
    """
    out = thumb_path_for(clip_id)
    if out.exists() and out.stat().st_size > 0:
        return out

    with db.db() as conn:
        row = conn.execute(
            "SELECT clip_path, start_s, end_s FROM candidate_clip WHERE id=?",
            (clip_id,),
        ).fetchone()
    if not row or not row["clip_path"]:
        return None

    clip_file = config.resolve_data(row["clip_path"])
    if not clip_file.exists():
        log.warning("clip file missing: %s", clip_file)
        return None

    # Grab a frame near the middle of the clip; falls back to 0.5s if the clip
    # is very short.
    dur = max(0.5, (row["end_s"] - row["start_s"]))
    seek_s = min(dur / 2, dur - 0.1)

    try:
        _render(clip_file, out, seek_s)
    except subprocess.CalledProcessError as e:
        log.error("thumb render failed for clip_id=%s: %s", clip_id, e)
        return None

    try:
        stored = str(out.relative_to(config.DATA))
    except ValueError:
        stored = str(out)
    with db.db() as conn:
        conn.execute(
            "UPDATE candidate_clip SET thumb_path=? WHERE id=?", (stored, clip_id)
        )
    return out
