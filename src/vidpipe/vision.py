"""Per-second vision metadata via Florence-2 (OCR + object detection + caption).

Pipeline:
  1. Build a sample schedule: fixed 1 Hz grid unioned with scene-change
     timestamps from PySceneDetect on keyframes.
  2. Decode one frame per timestamp with PyAV (no cv2 / libgl1 dependency).
  3. Run Florence-2-large with three task prompts per frame:
     <OD> → object detections
     <OCR_WITH_REGION> → on-screen text
     <MORE_DETAILED_CAPTION> → dense caption
  4. Write vision_frame rows + child detection / OCR rows to SQLite.

Stays sequential on GPU — cohabits VRAM with transcribe / events stages,
which each run as their own explicit stage.
"""

from __future__ import annotations

import json
import logging
from pathlib import Path
from typing import Iterable

import av
import numpy as np
import torch
from PIL import Image
from rich.console import Console
from rich.progress import Progress, TimeElapsedColumn

from . import config, db

log = logging.getLogger(__name__)
console = Console()

_florence = None
_processor = None

# Florence task prompts
TASK_OD = "<OD>"
TASK_OCR = "<OCR_WITH_REGION>"
TASK_CAPTION = "<MORE_DETAILED_CAPTION>"


def _get_florence():
    """Lazy-load Florence-2-large model + processor."""
    global _florence, _processor
    if _florence is None:
        from transformers import AutoModelForCausalLM, AutoProcessor

        console.print("[cyan]Loading Florence-2-large…[/]")
        model_name = "microsoft/Florence-2-large"
        kwargs = {
            "trust_remote_code": True,
            "torch_dtype": torch.float16,
            "cache_dir": str(config.MODELS),
        }
        _florence = AutoModelForCausalLM.from_pretrained(model_name, **kwargs).to("cuda")
        _processor = AutoProcessor.from_pretrained(model_name, trust_remote_code=True, cache_dir=str(config.MODELS))
        _florence.eval()
    return _florence, _processor


def warmup() -> None:
    """Pre-download Florence-2 weights without processing video."""
    _get_florence()
    console.print("[green]✓[/] Florence-2 loaded.")


def _sample_schedule(video_path: Path, duration_s: float) -> list[float]:
    """Build sorted, deduped sample timestamps.

    Union of:
      - fixed grid at VISION_FPS Hz over [0, duration_s]
      - scene-change timestamps from PySceneDetect ContentDetector on keyframes
    Deduplicate within 0.3 s, cap to VISION_MAX_FRAMES.
    """
    fps = config.VISION_FPS
    grid = [float(t) for t in np.arange(0.0, duration_s + 1.0 / fps, 1.0 / fps)]

    scene_changes: list[float] = []
    if config.VISION_SCENE_DETECT:
        try:
            scene_changes = _scene_changes(video_path, duration_s)
        except Exception as e:
            log.warning("scene detection failed for %s: %s", video_path, e)

    combined = sorted(set(grid + scene_changes))
    # dedupe within 0.3 s
    deduped: list[float] = []
    for t in combined:
        if not deduped or abs(t - deduped[-1]) >= 0.3:
            deduped.append(t)

    if len(deduped) > config.VISION_MAX_FRAMES:
        # Keep fixed-grid evenly spaced subset plus all scene changes
        grid_set = set(grid)
        keep_scenes = [t for t in deduped if t in grid_set or t in scene_changes]
        non_scene = [t for t in deduped if t not in scene_changes]
        n_keep = config.VISION_MAX_FRAMES - len(keep_scenes)
        if n_keep > 0 and non_scene:
            step = max(1, len(non_scene) // n_keep)
            sampled_non_scene = non_scene[::step][:n_keep]
            deduped = sorted(set(keep_scenes + sampled_non_scene))
        else:
            deduped = deduped[: config.VISION_MAX_FRAMES]
    return deduped


def _scene_changes(video_path: Path, _duration_s: float) -> list[float]:
    """Run PySceneDetect ContentDetector and return scene-cut timestamps."""
    from scenedetect import ContentDetector, SceneManager
    from scenedetect.backends import VideoStreamCv2

    detector = ContentDetector(threshold=config.VISION_SCENE_THRESHOLD)
    sm = SceneManager()
    sm.add_detector(detector)
    stream = VideoStreamCv2(str(video_path))
    sm.detect_scenes(stream)
    return [scene[0].get_seconds() for scene in sm.get_scene_list()]


def _decode_frames(video_path: Path, timestamps: list[float]) -> Iterable[tuple[float, np.ndarray]]:
    """Yield (timestamp, rgb_uint8_array) for each requested timestamp.

    Uses PyAV seek-and-decode; no cv2 / libgl1 dependency.
    """
    container = av.open(str(video_path))
    stream = container.streams.video[0]
    # Stream time_base can vary; use pts for accurate seeking.
    time_base = float(stream.time_base or (1 / 30))

    for ts in timestamps:
        target_pts = int(ts / time_base)
        container.seek(target_pts, backward=True, stream=stream)
        frame = None
        for packet in container.demux(stream):
            for frm in packet.decode():
                if frm.pts is not None and float(frm.pts) >= target_pts:
                    frame = frm
                    break
            if frame is not None:
                break
        if frame is None:
            log.warning("could not decode frame at ts=%.2f for %s", ts, video_path)
            continue
        img = frame.to_ndarray(format="rgb24")
        yield (ts, img)

    container.close()


def _parse_florence_od(text: str) -> list[dict]:
    """Parse <OD> result into {label, bbox, conf} list."""
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return []
    # Florence-2 OD format: {"<OD>": {"labels": [...], "bboxes": [[x1,y1,x2,y2], ...]}}
    od = data.get("<OD>") or data.get("od") or {}
    labels = od.get("labels", [])
    bboxes = od.get("bboxes", [])
    results = []
    for lbl, bbox in zip(labels, bboxes):
        x1, y1, x2, y2 = bbox
        area_frac = max(0.0, (x2 - x1) * (y2 - y1))
        results.append({"label": lbl, "bbox": [x1, y1, x2, y2], "conf": 1.0, "area_frac": area_frac})
    return results


def _parse_florence_ocr(text: str) -> list[dict]:
    """Parse <OCR_WITH_REGION> result into {text, polygon, conf} list.

    Convert polygon to axis-aligned bbox and compute area_frac.
    """
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return []
    ocr = data.get("<OCR_WITH_REGION>") or data.get("ocr_with_region") or {}
    labels = ocr.get("labels", [])
    bboxes = ocr.get("quad_boxes", [])
    results = []
    for lbl, poly in zip(labels, bboxes):
        # poly is [x1,y1,x2,y2,x3,y3,x4,y4] — convert to aabb
        xs = poly[0::2]
        ys = poly[1::2]
        x1, x2 = min(xs), max(xs)
        y1, y2 = min(ys), max(ys)
        area_frac = max(0.0, (x2 - x1) * (y2 - y1))
        results.append({"text": lbl, "bbox": [x1, y1, x2, y2], "conf": 1.0, "area_frac": area_frac})
    return results


def _parse_florence_caption(text: str) -> str:
    """Extract caption string from <MORE_DETAILED_CAPTION> result."""
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return text.strip()
    cap = data.get("<MORE_DETAILED_CAPTION>") or data.get("more_detailed_caption") or {}
    if isinstance(cap, str):
        return cap.strip()
    if isinstance(cap, list) and cap:
        return str(cap[0]).strip()
    return ""


def _run_florence_batch(images: list[Image.Image], prompts: list[str]) -> list[str]:
    """Run Florence-2 on a batch of images + prompts. Returns list of result strings."""
    model, processor = _get_florence()
    inputs = processor(text=prompts, images=images, return_tensors="pt").to("cuda")
    with torch.no_grad():
        generated_ids = model.generate(
            input_ids=inputs["input_ids"],
            pixel_values=inputs["pixel_values"],
            max_new_tokens=1024,
            num_beams=3,
        )
    results = processor.batch_decode(generated_ids, skip_special_tokens=False)
    return results


def detect_vision_for_video(video_id: int, video_path: Path) -> dict[str, int]:
    """Run Florence-2 on sampled frames for one video. Returns counts dict."""
    # Get duration from ffprobe-friendly source
    import subprocess

    probe = subprocess.run(
        ["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "json", str(video_path)],
        capture_output=True,
        text=True,
        check=True,
    )
    duration = float(json.loads(probe.stdout)["format"]["duration"])

    schedule = _sample_schedule(video_path, duration)
    console.print(f"[cyan]Vision sampling[/] video_id={video_id}  {len(schedule)} frames")

    frame_data: list[tuple[float, np.ndarray]] = []
    for ts, img in _decode_frames(video_path, schedule):
        frame_data.append((ts, img))

    # Run Florence-2 in batches of 3 prompts × batch_size frames
    batch_size = config.VISION_BATCH_SIZE
    all_results: list[dict] = []

    with Progress(
        *Progress.get_default_columns(), TimeElapsedColumn(), console=console
    ) as prog:
        task = prog.add_task(f"Florence-2 video_id={video_id}", total=len(frame_data))
        for i in range(0, len(frame_data), batch_size):
            batch = frame_data[i : i + batch_size]
            images = [Image.fromarray(img) for _ts, img in batch]
            prompts = []
            for _ in batch:
                prompts.extend([TASK_OD, TASK_OCR, TASK_CAPTION])
            results = _run_florence_batch(images * 3, prompts)
            # results interleave: [od0, ocr0, cap0, od1, ocr1, cap1, ...]
            for j, (ts, img) in enumerate(batch):
                od_text = results[j * 3 + 0]
                ocr_text = results[j * 3 + 1]
                cap_text = results[j * 3 + 2]
                h, w = img.shape[:2]
                od_items = _parse_florence_od(od_text)
                for it in od_items:
                    it["area_frac"] = it["area_frac"] / max(1, w * h)
                ocr_items = _parse_florence_ocr(ocr_text)
                for it in ocr_items:
                    it["area_frac"] = it["area_frac"] / max(1, w * h)
                caption = _parse_florence_caption(cap_text)
                all_results.append({
                    "ts": ts,
                    "sampled": "scene" if ts not in set(np.arange(0.0, duration + 1.0 / config.VISION_FPS, 1.0 / config.VISION_FPS)) else "fixed",
                    "caption": caption,
                    "od": od_items,
                    "ocr": ocr_items,
                })
            prog.update(task, advance=len(batch))

    # Write to DB in one transaction
    with db.db() as conn:
        conn.execute("DELETE FROM vision_frame WHERE video_id = ?", (video_id,))
        for r in all_results:
            cur = conn.execute(
                """INSERT INTO vision_frame
                   (video_id, ts_s, sampled, caption, n_objs, n_ocr, raw_json)
                   VALUES (?,?,?,?,?,?,?)""",
                (
                    video_id,
                    float(r["ts"]),
                    r["sampled"],
                    r["caption"] or None,
                    len(r["od"]),
                    len(r["ocr"]),
                    json.dumps({"od": r["od"], "ocr": r["ocr"]}),
                ),
            )
            frame_id = cur.lastrowid
            for it in r["od"]:
                conn.execute(
                    """INSERT INTO vision_detection
                       (frame_id, video_id, ts_s, label, conf, bbox_x1, bbox_y1, bbox_x2, bbox_y2, area_frac)
                       VALUES (?,?,?,?,?,?,?,?,?,?)""",
                    (
                        frame_id, video_id, float(r["ts"]),
                        it["label"], it["conf"],
                        it["bbox"][0], it["bbox"][1], it["bbox"][2], it["bbox"][3],
                        it["area_frac"],
                    ),
                )
            for it in r["ocr"]:
                conn.execute(
                    """INSERT INTO vision_ocr
                       (frame_id, video_id, ts_s, text, text_upper, conf, bbox_x1, bbox_y1, bbox_x2, bbox_y2, area_frac)
                       VALUES (?,?,?,?,?,?,?,?,?,?,?)""",
                    (
                        frame_id, video_id, float(r["ts"]),
                        it["text"], it["text"].upper(), it["conf"],
                        it["bbox"][0], it["bbox"][1], it["bbox"][2], it["bbox"][3],
                        it["area_frac"],
                    ),
                )
        conn.execute(
            "UPDATE video SET vision_done=1, updated_at=datetime('now') WHERE id=?",
            (video_id,),
        )

    return {
        "frames": len(all_results),
        "objects": sum(len(r["od"]) for r in all_results),
        "ocr": sum(len(r["ocr"]) for r in all_results),
    }


def detect_pending(force: bool = False) -> dict[int, dict[str, int]]:
    with db.db() as conn:
        rows = conn.execute(
            """SELECT id, path FROM video
               WHERE (vision_done = 0 OR ? = 1)""",
            (1 if force else 0,),
        ).fetchall()
    results = {}
    for row in rows:
        video_path = Path(row["path"])
        if not video_path.exists():
            log.warning("missing video file for video_id=%s: %s", row["id"], row["path"])
            continue
        results[row["id"]] = detect_vision_for_video(row["id"], video_path)
    return results


def reset_vision(conn, video_id: int) -> None:
    """Delete vision rows for a video and invalidate downstream stages."""
    conn.execute("DELETE FROM vision_frame WHERE video_id = ?", (video_id,))
    conn.execute(
        "UPDATE video SET vision_done=0, score_done=0 WHERE id=?",
        (video_id,),
    )
