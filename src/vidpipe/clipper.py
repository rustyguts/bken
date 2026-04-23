"""Cut candidate clips out of source videos using ffmpeg.

We pad each candidate by config.CLIP_PAD_BEFORE / CLIP_PAD_AFTER seconds
and clamp to [CLIP_MIN_SECONDS, CLIP_MAX_SECONDS] so the resulting mp4
doesn't start mid-word. Scene-cut / silence snapping is a future
improvement; for now padding + clamp gives decent edges.
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


def _pad_clip(start: float, end: float, duration: float) -> tuple[float, float]:
    s = max(0.0, start - config.CLIP_PAD_BEFORE)
    e = min(duration, end + config.CLIP_PAD_AFTER)
    length = e - s
    if length < config.CLIP_MIN_SECONDS:
        need = config.CLIP_MIN_SECONDS - length
        s = max(0.0, s - need / 2)
        e = min(duration, e + need / 2)
    if (e - s) > config.CLIP_MAX_SECONDS:
        mid = (s + e) / 2
        s = mid - config.CLIP_MAX_SECONDS / 2
        e = mid + config.CLIP_MAX_SECONDS / 2
    return s, e


def _video_encode_args(codec: str) -> list[str]:
    args: list[str] = ["-c:v", codec]
    if codec == "libsvtav1":
        args += [
            "-preset", str(config.CLIP_VIDEO_PRESET),
            "-crf", str(config.CLIP_VIDEO_CRF),
            "-pix_fmt", "yuv420p",
            "-g", "120",
        ]
    elif codec == "libaom-av1":
        args += [
            "-crf", str(config.CLIP_VIDEO_CRF),
            "-cpu-used", str(config.CLIP_VIDEO_PRESET),
            "-row-mt", "1",
            "-pix_fmt", "yuv420p",
            "-b:v", "0",
        ]
    elif codec == "libvpx-vp9":
        args += [
            "-crf", str(config.CLIP_VIDEO_CRF),
            "-b:v", "0",
            "-deadline", "good",
            "-cpu-used", str(config.CLIP_VIDEO_PRESET),
            "-row-mt", "1",
            "-pix_fmt", "yuv420p",
        ]
    elif codec == "libopenh264":
        args += [
            "-b:v", config.CLIP_VIDEO_BITRATE,
            "-profile:v", "high",
            "-pix_fmt", "yuv420p",
        ]
    elif codec == "h264_nvenc":
        args += [
            "-preset", "p7",
            "-rc", "vbr",
            "-cq", str(config.CLIP_VIDEO_CRF),
            "-b:v", "0",
            "-profile:v", "high",
            "-pix_fmt", "yuv420p",
        ]
    elif codec == "av1_nvenc":
        args += [
            "-preset", "p7",
            "-rc", "vbr",
            "-cq", str(config.CLIP_VIDEO_CRF),
            "-b:v", "0",
            "-pix_fmt", "yuv420p",
        ]
    else:
        args += ["-b:v", config.CLIP_VIDEO_BITRATE, "-pix_fmt", "yuv420p"]
    return args


def _ffmpeg_cut(
    src: Path, dst: Path, start: float, end: float, stream_copy: bool = False
) -> None:
    dst.parent.mkdir(parents=True, exist_ok=True)
    dur = end - start
    tmp = dst.with_name(dst.stem + ".partial" + dst.suffix)
    cmd = [
        "ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
        "-ss", f"{start:.3f}",
        "-i", str(src),
        "-t", f"{dur:.3f}",
    ]
    if stream_copy:
        cmd += ["-c", "copy"]
    else:
        cmd += _video_encode_args(config.CLIP_VIDEO_CODEC)
        cmd += [
            "-c:a", "aac", "-b:a", config.CLIP_AUDIO_BITRATE,
            "-movflags", "+faststart",
        ]
    cmd += [str(tmp)]
    try:
        subprocess.run(cmd, check=True)
        tmp.replace(dst)
    except BaseException:
        tmp.unlink(missing_ok=True)
        raise


def _cut_one(
    clip_id: int,
    src: Path,
    out: Path,
    start: float,
    end: float,
    stream_copy: bool,
    force: bool = False,
) -> bool:
    if force or not out.exists():
        _ffmpeg_cut(src, out, start, end, stream_copy=stream_copy)
    try:
        rel = out.relative_to(config.DATA)
        stored = str(rel)
    except ValueError:
        stored = str(out)
    conn = db.connect()
    try:
        conn.execute(
            "UPDATE candidate_clip SET clip_path=? WHERE id=?",
            (stored, clip_id),
        )
    finally:
        conn.close()
    return True


def cut_single(
    clip_id: int,
    stream_copy: bool = False,
    force: bool = False,
) -> bool:
    """Cut a single candidate clip to disk and update its clip_path."""
    with db.db() as conn:
        row = conn.execute(
            """SELECT c.id, c.video_id, c.start_s, c.end_s, c.source,
                      v.path AS video_path, v.duration_s
                 FROM candidate_clip c
                 JOIN video v ON v.id = c.video_id
                WHERE c.id=?""",
            (clip_id,),
        ).fetchone()
    if not row:
        log.error("clip_id=%s not found", clip_id)
        return False
    src = Path(row["video_path"])
    duration = row["duration_s"] or 0
    video_id = row["video_id"]
    s, e = _pad_clip(row["start_s"], row["end_s"], duration)
    out_dir = config.CLIPS_DIR / f"video_{video_id}"
    out_dir.mkdir(parents=True, exist_ok=True)
    out = out_dir / f"clip_{clip_id:04d}_{int(s):05d}-{int(e):05d}.mp4"
    return _safe_cut(clip_id, src, out, s, e, stream_copy, force)


def _safe_cut(clip_id, src, out, start, end, stream_copy, force=False) -> bool:
    try:
        return _cut_one(clip_id, src, out, start, end, stream_copy, force=force)
    except subprocess.CalledProcessError as ex:
        log.error("ffmpeg cut failed clip_id=%s: %s", clip_id, ex)
    except Exception as ex:
        log.error("clip cut failed clip_id=%s: %s", clip_id, ex)
    return False


def extract_pending(
    stream_copy: bool = False,
    workers: int = 4,
    on_video_done: Optional[Callable[[int, int], None]] = None,
    on_clip_done: Optional[Callable[[int, int, bool], None]] = None,
    force: bool = False,
) -> dict[int, int]:
    """Cut clips for every video that's been scored. Returns {video_id: clips_cut}."""
    with db.db() as conn:
        rows = conn.execute(
            "SELECT id FROM video WHERE score_done = 1"
        ).fetchall()
    out: dict[int, int] = {}
    for r in rows:
        vid = r["id"]

        def per_clip(clip_id: int, ok: bool, _vid=vid) -> None:
            if on_clip_done:
                on_clip_done(_vid, clip_id, ok)

        n = extract_clips_for_video(
            vid, stream_copy=stream_copy, workers=workers,
            on_done=per_clip, force=force,
        )
        out[vid] = n
        if on_video_done:
            on_video_done(vid, n)
    return out


def extract_clips_for_video(
    video_id: int,
    stream_copy: bool = False,
    workers: int = 4,
    on_done: Optional[Callable[[int, bool], None]] = None,
    force: bool = False,
) -> int:
    with db.db() as conn:
        vrow = conn.execute("SELECT path, duration_s FROM video WHERE id=?", (video_id,)).fetchone()
        if not vrow:
            return 0
        clip_rows = conn.execute(
            "SELECT id, start_s, end_s FROM candidate_clip "
            "WHERE video_id=? AND active=1 ORDER BY start_s",
            (video_id,),
        ).fetchall()

    src = Path(vrow["path"])
    duration = vrow["duration_s"] or 0
    out_dir = config.CLIPS_DIR / f"video_{video_id}"
    out_dir.mkdir(parents=True, exist_ok=True)

    jobs = []
    for row in clip_rows:
        s, e = _pad_clip(row["start_s"], row["end_s"], duration)
        out = out_dir / f"clip_{row['id']:04d}_{int(s):05d}-{int(e):05d}.mp4"
        jobs.append((row["id"], src, out, s, e))

    n = 0
    if workers <= 1:
        for clip_id, src_p, out_p, s, e in jobs:
            ok = _safe_cut(clip_id, src_p, out_p, s, e, stream_copy, force)
            n += 1 if ok else 0
            if on_done:
                on_done(clip_id, ok)
        return n

    with ThreadPoolExecutor(max_workers=workers, thread_name_prefix=f"clip-v{video_id}") as ex:
        futures = {
            ex.submit(_safe_cut, clip_id, src_p, out_p, s, e, stream_copy, force): clip_id
            for (clip_id, src_p, out_p, s, e) in jobs
        }
        for fut in as_completed(futures):
            clip_id = futures[fut]
            ok = fut.result()
            n += 1 if ok else 0
            if on_done:
                on_done(clip_id, ok)
    return n
