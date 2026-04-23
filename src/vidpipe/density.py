"""Per-second interest density + variable-length clip segmentation.

The previous scorer slid a fixed 30 s window across the timeline and kept
the top-N. That truncates clips at arbitrary boundaries — a 50-second
buildup-and-payoff gets cut at second 30, a 5-second gem comes wrapped
in 25 seconds of dead air.

This module replaces that with a two-step pipeline:

  1. `build_density` rasterizes audio events + transcript signals onto a
     1 Hz array I[t] of length ``int(duration_s) + 1``, then smooths with
     a Gaussian (sigma = `config.DENSITY_SMOOTH_SIGMA`). Same WEIGHTS as
     the old scorer, just per-second instead of per-window.
  2. `multi_scale_segments` runs **hysteresis** at multiple (high, low)
     percentile pairs. Each pair seeds where I > high and extends both
     ways while I > low, tolerating short dips up to
     `config.DENSITY_GAP_TOLERANCE` seconds. Tight thresholds yield
     short crisp clips, loose thresholds yield long sustained ones.
     Pool + NMS picks the best representation of each moment.

The output is a list of (start_s, end_s, score, features) tuples — same
shape `scoring.score_video` needs to insert into `candidate_clip`. No DB
access here; this module is pure numpy/scipy and unit-testable.
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from typing import Iterable

import numpy as np
import scipy.ndimage

# Per-signal weights used to build the density signal. Same numbers as the
# old per-window scoring weights, just applied per second so they accumulate
# along the timeline. Tune in one place.
WEIGHTS: dict[str, float] = {
    # Audio events (PANNs SED + RMS)
    "Laughter":  4.0,
    "Shout":     6.0,   # screaming highest — loudest "something happened" signal
    "Cheer":     3.0,
    "Gunfire":  -1.0,   # shooter default state; downweight
    "LoudSpike": 0.5,
    # Transcript-derived
    "talk_base": 0.08,  # any speech in this second
    "talk_dens": 0.02,  # per char/sec of speech
    "kw":        2.0,   # per keyword hit
    # Vision-derived
    "vis_ocr_generic":  0.2,
    "vis_ocr_keyword":  5.0,
    "vis_ocr_large":    2.0,
    "vis_caption_kw":   3.0,
    "vis_scene_change": 1.5,
    "vis_obj_person":   0.3,
    "vis_obj_vehicle":  0.2,
}

# Phrases that bracket reaction moments in gaming chatter. Same list as the
# old scorer; lifting it up here so weight tuning lives in one module.
KEYWORDS = [
    "oh my god", "oh my gosh", "oh fuck", "what the fuck", "wtf", "no way",
    "dude", "bro", "did you see that", "did you just", "what just happened",
    "fucking hell", "holy shit", "holy fuck", "get rekt", "let's go", "lets go",
    "are you kidding", "you idiot", "you bastard", "shut up", "dammit",
    "god damn", "goddamn", "oh no", "nooo", "why", "seriously",
    "what the hell", "incredible", "amazing",
]
KEYWORD_RE = re.compile(r"|".join(re.escape(k) for k in KEYWORDS), re.IGNORECASE)

VISION_OCR_KEYWORDS = [
    "you died", "wasted", "busted", "mission failed", "mission passed",
    "game over", "you were killed",
    "victory", "mission complete", "winner", "victory royale",
    "headshot", "double kill", "triple kill", "multi kill", "ace",
    "warning", "critical", "danger", "level up", "new high score",
]
OCR_KW_RE = re.compile(r"|".join(re.escape(k) for k in VISION_OCR_KEYWORDS), re.IGNORECASE)


@dataclass
class Region:
    """A candidate clip after segmentation. Times are seconds.

    `features` is a small breakdown for forensic display in the UI; we
    persist it as JSON in `candidate_clip.features`.
    """
    start: float
    end: float
    score: float
    features: dict


# ──────────────────────────────────────────────────────────────────────────
# Step 1 — build the density signal
# ──────────────────────────────────────────────────────────────────────────


def build_density(
    duration_s: float,
    audio_events: Iterable[dict],
    transcript_segments: Iterable[dict],
    sigma: float,
    *,
    vision_frames: Iterable[dict] | None = None,
    vision_ocr: Iterable[dict] | None = None,
    vision_detections: Iterable[dict] | None = None,
    vision_enabled: bool = True,
) -> np.ndarray:
    """Build the per-second interest signal I[t] for one video.

    `audio_events`: iterable of dicts with keys start_s, end_s, label, score.
    `transcript_segments`: iterable of dicts with keys start_s, end_s, text.
    `vision_*`: optional per-second vision metadata (OCR, detections, captions).

    Returns a 1-D float32 array of length ``int(duration_s) + 1`` where
    higher values mean "more interesting moment". Already gaussian-smoothed.
    """
    n = max(1, int(duration_s) + 1)
    I = np.zeros(n, dtype=np.float32)

    for ev in audio_events:
        w = WEIGHTS.get(ev["label"])
        if w is None:
            continue
        s = max(0, int(ev["start_s"]))
        e = min(n, int(ev["end_s"]) + 1)
        if e <= s:
            continue
        I[s:e] += w * float(ev["score"])

    for seg in transcript_segments:
        s = max(0, int(seg["start_s"]))
        e = min(n, int(seg["end_s"]) + 1)
        if e <= s:
            continue
        text = (seg.get("text") or "").strip()
        span = max(1, e - s)
        # Speech presence + density (chars per second of speech).
        I[s:e] += WEIGHTS["talk_base"] + WEIGHTS["talk_dens"] * (len(text) / span)
        if text:
            kw = len(KEYWORD_RE.findall(text))
            if kw:
                I[s:e] += WEIGHTS["kw"] * kw / span

    if vision_enabled:
        # OCR keyword hits
        if vision_ocr is not None:
            for row in vision_ocr:
                t = max(0, min(n - 1, int(row["ts_s"])))
                I[t] += WEIGHTS["vis_ocr_generic"]
                text = (row.get("text") or "").strip()
                if text and OCR_KW_RE.search(text):
                    multiplier = 1.0
                    if row.get("area_frac", 0) > 0.15:
                        multiplier += WEIGHTS["vis_ocr_large"]
                    I[t] += WEIGHTS["vis_ocr_keyword"] * multiplier

        # Caption keyword hits
        if vision_frames is not None:
            for row in vision_frames:
                t = max(0, min(n - 1, int(row["ts_s"])))
                caption = (row.get("caption") or "").strip()
                if caption:
                    kw = len(KEYWORD_RE.findall(caption))
                    if kw:
                        I[t] += WEIGHTS["vis_caption_kw"] * kw
                # Scene-change pulse
                if row.get("sampled") == "scene":
                    I[t] += WEIGHTS["vis_scene_change"]

        # Detections — capped per-second per bucket
        if vision_detections is not None:
            per_second: dict[int, dict[str, float]] = {}
            for row in vision_detections:
                t = max(0, min(n - 1, int(row["ts_s"])))
                label = row.get("label", "").lower()
                bucket = None
                if "person" in label:
                    bucket = "vis_obj_person"
                elif any(v in label for v in ("car", "bus", "truck", "motorcycle", "vehicle")):
                    bucket = "vis_obj_vehicle"
                if bucket:
                    per_second.setdefault(t, {})[bucket] = WEIGHTS[bucket]
            for t, buckets in per_second.items():
                for w in buckets.values():
                    I[t] += w

    if sigma > 0:
        I = scipy.ndimage.gaussian_filter1d(I, sigma=sigma)
    return I


# ──────────────────────────────────────────────────────────────────────────
# Step 2 — hysteresis segmentation
# ──────────────────────────────────────────────────────────────────────────


def _clamp_region(start: int, end: int, I: np.ndarray, min_len: int, max_len: int) -> tuple[int, int]:
    """Adjust [start, end) to fit [min_len, max_len], staying inside the array."""
    n = len(I)
    length = end - start
    if length < min_len:
        # Inflate symmetrically; if we hit a boundary, push the other side.
        need = min_len - length
        left = need // 2
        right = need - left
        new_s = max(0, start - left)
        new_e = min(n, end + right)
        # If we hit one boundary, take from the other side
        if new_s == 0:
            new_e = min(n, new_s + min_len)
        if new_e == n:
            new_s = max(0, new_e - min_len)
        start, end = new_s, new_e
    if (end - start) > max_len:
        peak = start + int(np.argmax(I[start:end]))
        start = max(0, peak - max_len // 2)
        end = min(n, start + max_len)
        if (end - start) < max_len:
            start = max(0, end - max_len)
    return start, end


def hysteresis_segments(
    I: np.ndarray,
    high: float,
    low: float,
    *,
    gap_tol: int,
    min_len: int,
    max_len: int,
) -> list[tuple[int, int]]:
    """Return [(start, end)] regions where I > low, seeded by I > high.

    A region is opened wherever I crosses low and contains at least one
    sample above high. Sub-threshold dips shorter than `gap_tol` seconds
    are bridged so a one-beat pause inside a laugh doesn't split the clip.
    """
    if low <= 0 or high <= 0 or len(I) == 0:
        return []
    # `>=` instead of `>` so the edge case where a percentile threshold lands
    # exactly on the data value (common when the signal has plateaus) still
    # forms a region. On real, noisy density signals the difference is moot.
    above_low = I >= low
    if gap_tol > 1:
        above_low = scipy.ndimage.binary_closing(
            above_low, structure=np.ones(int(gap_tol), dtype=bool)
        )

    regions: list[tuple[int, int]] = []
    n = len(I)
    in_r = False
    start = 0
    for t in range(n):
        if above_low[t] and not in_r:
            in_r = True
            start = t
        elif not above_low[t] and in_r:
            in_r = False
            if I[start:t].max() >= high:
                regions.append(_clamp_region(start, t, I, min_len, max_len))
    if in_r and I[start:n].max() >= high:
        regions.append(_clamp_region(start, n, I, min_len, max_len))
    return regions


# ──────────────────────────────────────────────────────────────────────────
# Step 3 — multi-scale pool + NMS
# ──────────────────────────────────────────────────────────────────────────


def _percentile_pos(I: np.ndarray, pct: float) -> float:
    """Percentile of *non-zero* density. Avoids p95 collapsing to 0 on quiet videos."""
    nz = I[I > 0]
    if len(nz) == 0:
        return float("inf")  # nothing crosses the threshold → no segments
    return float(np.percentile(nz, pct))


def _iou(a: tuple[int, int], b: tuple[int, int]) -> float:
    s = max(a[0], b[0])
    e = min(a[1], b[1])
    overlap = max(0, e - s)
    union = (a[1] - a[0]) + (b[1] - b[0]) - overlap
    return overlap / union if union > 0 else 0.0


def multi_scale_segments(
    I: np.ndarray,
    *,
    scales: list[tuple[float, float]],
    gap_tol: int,
    min_len: int,
    max_len: int,
    top_n: int,
    nms_iou: float = 0.5,
) -> list[Region]:
    """Run hysteresis at each (high_pct, low_pct) pair, pool, NMS.

    Region score is the sum of I over the region. Sum (not mean) rewards
    sustained density: a 60 s clip that stays interesting outscores a
    short spike, but `max_len` caps the window so it can't run away.
    """
    pooled: list[Region] = []
    for high_pct, low_pct in scales:
        high = _percentile_pos(I, high_pct)
        low = _percentile_pos(I, low_pct)
        if not np.isfinite(high) or not np.isfinite(low):
            continue
        for s, e in hysteresis_segments(
            I, high=high, low=low,
            gap_tol=gap_tol, min_len=min_len, max_len=max_len,
        ):
            score = float(I[s:e].sum())
            pooled.append(Region(
                start=float(s), end=float(e), score=score,
                features={
                    "scale_high_pct": high_pct,
                    "scale_low_pct": low_pct,
                    "length_s": e - s,
                    "peak": float(I[s:e].max()),
                    "mean": float(I[s:e].mean()),
                },
            ))

    # NMS by score desc; drop overlapping survivors.
    pooled.sort(key=lambda r: r.score, reverse=True)
    kept: list[Region] = []
    for r in pooled:
        keep = True
        for k in kept:
            if _iou((int(r.start), int(r.end)), (int(k.start), int(k.end))) > nms_iou:
                keep = False
                break
        if keep:
            kept.append(r)
            if len(kept) >= top_n:
                break

    kept.sort(key=lambda r: r.start)
    return kept


# ──────────────────────────────────────────────────────────────────────────
# Step 4 — boundary snapping (optional polish)
# ──────────────────────────────────────────────────────────────────────────


def snap_to_silence(
    region: Region,
    transcript_segments: list[dict],
    window_s: float,
) -> Region:
    """Shift edges by up to `window_s` toward the nearest transcript-segment gap.

    A "gap" is the timestamp between a segment's end and the next segment's
    start. Snapping to one keeps clips from starting mid-word. If there's
    no gap within `window_s` of an edge, the edge stays put.
    """
    if window_s <= 0 or not transcript_segments:
        return region
    # Build sorted lists of gap start/end timestamps.
    segs = sorted(transcript_segments, key=lambda s: s["start_s"])
    gap_times: list[float] = []
    for prev, nxt in zip(segs, segs[1:]):
        if nxt["start_s"] - prev["end_s"] >= 0.2:  # real silence
            gap_times.append((prev["end_s"] + nxt["start_s"]) / 2)
    if not gap_times:
        return region

    def nearest(t: float) -> float | None:
        best = None
        best_d = window_s + 1
        for g in gap_times:
            d = abs(g - t)
            if d <= window_s and d < best_d:
                best, best_d = g, d
        return best

    new_start = nearest(region.start)
    new_end = nearest(region.end)
    s = new_start if new_start is not None else region.start
    e = new_end if new_end is not None else region.end
    if e <= s:  # snap collapsed the region — bail and keep original
        return region
    return Region(start=s, end=e, score=region.score, features=region.features)


# ──────────────────────────────────────────────────────────────────────────
# IoU-based stable identity (for re-scoring)
# ──────────────────────────────────────────────────────────────────────────


def match_existing(
    new_regions: list[Region],
    existing: list[dict],
    iou_threshold: float = 0.5,
) -> tuple[list[tuple[Region, dict]], list[Region], list[dict]]:
    """Greedy IoU-based match between new regions and existing clip rows.

    `existing` rows must have at least: id, start_s, end_s, user_rating,
    llm_title (used for tie-break).

    Returns (matched, unmatched_new, unmatched_existing). Greedy in
    score-descending order: best new region gets first pick of any
    contested existing row; an existing row matches at most one new region.
    """
    sorted_new = sorted(new_regions, key=lambda r: r.score, reverse=True)
    available = list(existing)
    matched: list[tuple[Region, dict]] = []
    unmatched_new: list[Region] = []

    for new in sorted_new:
        best: dict | None = None
        best_iou = iou_threshold
        for row in available:
            iou = _iou(
                (int(new.start), int(new.end)),
                (int(row["start_s"]), int(row["end_s"])),
            )
            if iou < best_iou:
                continue
            if iou > best_iou:
                best, best_iou = row, iou
                continue
            # Tie on IoU — prefer rated row, then row with llm_title, then smallest id.
            if best is None:
                best = row
                continue
            cur_score = (
                (1 if row["user_rating"] is not None else 0),
                (1 if row["llm_title"] else 0),
                -row["id"],
            )
            best_score = (
                (1 if best["user_rating"] is not None else 0),
                (1 if best["llm_title"] else 0),
                -best["id"],
            )
            if cur_score > best_score:
                best = row
        if best is not None:
            matched.append((new, best))
            available.remove(best)
        else:
            unmatched_new.append(new)

    return matched, unmatched_new, available
