"""Paths and constants.

One place to change where derived artifacts live. Everything is keyed to
`ROOT`, so moving the project (or symlinking `data/` to a larger disk) is
a one-line change.
"""

from __future__ import annotations

import os
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DATA = Path(os.environ.get("VIDPIPE_DATA", ROOT / "data"))
MODELS = Path(os.environ.get("VIDPIPE_MODELS", ROOT / "models"))

DB_PATH = DATA / "index.db"
AUDIO_DIR = DATA / "audio"
CLIPS_DIR = DATA / "clips"
ARTIFACTS_DIR = DATA / "artifacts"

# Audio processing
AUDIO_SAMPLE_RATE = 16000  # Hz — what Whisper and most audio models want
AUDIO_CHANNELS = 1

# ASR
WHISPER_MODEL = os.environ.get("VIDPIPE_WHISPER_MODEL", "large-v3")
WHISPER_COMPUTE_TYPE = os.environ.get("VIDPIPE_WHISPER_COMPUTE", "float16")

# Scoring (density-based)
#
# We rasterize all signals (audio events, transcript) onto a 1 Hz interest
# density I[t], smooth it, then carve out variable-length clips with
# hysteresis thresholding. Three threshold passes give clips of different
# lengths (short crisp moments → long sustained bits) over the same signal.
#
# Tuning notes:
#  - DENSITY_SMOOTH_SIGMA: bigger = wider clips (signal blurs into neighbors)
#  - DENSITY_GAP_TOLERANCE: a sub-threshold dip shorter than this many seconds
#    won't terminate a clip — keeps a one-beat pause inside a laugh together
#  - DENSITY_SCALES: list of (high_pct, low_pct) percentile pairs. Tight pair
#    (high pcts) → short clips; loose pair → long clips. Pool + NMS picks
#    the best representation of each moment.
DENSITY_SMOOTH_SIGMA = 3.0
DENSITY_GAP_TOLERANCE = 4
DENSITY_SCALES: list[tuple[float, float]] = [
    (95.0, 85.0),  # tight: 5–15 s — single reactions
    (90.0, 75.0),  # medium: 15–35 s — joke + payoff
    (85.0, 65.0),  # loose: 35–90 s — sustained bits
]
TOP_N_CANDIDATES = 30  # max clips per video after pool+NMS

# Clip extraction
CLIP_PAD_BEFORE = 3.0
# Generous trail-off so a punchline doesn't get cut at the laugh — gives the
# beat after the moment to land. The hysteresis segmentation already finds the
# natural end of the action; this pad sits *after* that, capturing reactions.
CLIP_PAD_AFTER = 8.0
CLIP_MIN_SECONDS = 8.0
CLIP_MAX_SECONDS = 90.0  # was 60; raised so the loose hysteresis pass isn't capped before NMS

# Boundary snapping: after segmentation we shift each edge by up to this many
# seconds toward the nearest transcript-segment gap, so clips don't start
# mid-word. Set to 0 to disable.
CLIP_SNAP_WINDOW = 2.0

# Encode quality. Source archive is AV1 1080p, so we default to libsvtav1 — it
# preserves the source far better than libopenh264 at any sane bitrate, and
# AV1-in-MP4 plays in every current browser. Override via env if you need a
# different codec or want to dial CPU usage down.
#   svtav1 preset: 0 (slowest/best) … 13 (fastest). 5 ≈ "saturate a desktop
#   CPU at sub-realtime for 1080p". Bump to 7-8 for faster, drop to 3-4 if
#   you want CPU truly to hurt.
#   crf: 0 (lossless) … 63. 26-30 is visually transparent on gameplay; 28
#   keeps clips small while preserving HUD text.
CLIP_VIDEO_CODEC = os.environ.get("VIDPIPE_CLIP_VCODEC", "libsvtav1")
CLIP_VIDEO_CRF = int(os.environ.get("VIDPIPE_CLIP_CRF", "28"))
CLIP_VIDEO_PRESET = os.environ.get("VIDPIPE_CLIP_PRESET", "5")
CLIP_AUDIO_BITRATE = os.environ.get("VIDPIPE_CLIP_ABITRATE", "192k")
# Fallback bitrate for codecs that don't expose CRF (libopenh264). 1080p target.
CLIP_VIDEO_BITRATE = os.environ.get("VIDPIPE_CLIP_VBITRATE", "10M")

# Vision (Florence-2) sampling
VISION_FPS = float(os.environ.get("VIDPIPE_VISION_FPS", "1.0"))
VISION_SCENE_DETECT = os.environ.get("VIDPIPE_VISION_SCENE", "1") == "1"
VISION_SCENE_THRESHOLD = float(os.environ.get("VIDPIPE_VISION_SCENE_THRESH", "27.0"))
VISION_MAX_FRAMES = int(os.environ.get("VIDPIPE_VISION_MAX_FRAMES", "5000"))
VISION_BATCH_SIZE = int(os.environ.get("VIDPIPE_VISION_BATCH", "4"))
VISION_SCORE_ENABLED = os.environ.get("VIDPIPE_VISION_SCORE", "1") == "1"


def ensure_dirs() -> None:
    for p in (DATA, MODELS, AUDIO_DIR, CLIPS_DIR, ARTIFACTS_DIR):
        p.mkdir(parents=True, exist_ok=True)


def resolve_data(stored: str | Path) -> Path:
    """Resolve a clip/thumb path stored in the DB to an absolute path that
    works in the current environment.

    Historically we stored absolute paths (e.g. `/home/rusty/.../data/...`).
    Those break when `DATA` changes — notably, when the same DB is mounted
    inside a Docker container where the data dir is `/app/data`. This helper
    accepts:

      * absolute paths under the current `DATA` (returned as-is)
      * absolute paths under a *different* data root (rewritten by finding
        the `/data/` boundary and re-rooting)
      * relative paths (joined onto `DATA`)

    If none of those work we return the input as a `Path` and let the
    caller 404.
    """
    p = Path(stored)
    if not p.is_absolute():
        return DATA / p
    if p.exists():
        return p
    # Re-root: strip everything up to and including the last `data` segment
    # and rebuild under the current DATA.
    parts = p.parts
    for i in range(len(parts) - 1, -1, -1):
        if parts[i] == "data":
            return DATA.joinpath(*parts[i + 1 :])
    return p
