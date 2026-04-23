# bken — Project Overview

## What This Application Is

`bken` is a **personal gaming-clip extraction pipeline**. It takes multi-hour gameplay recordings (often thousands of files in an archive) and automatically surfaces the funniest, most memorable, or most interesting moments as short, titled, shareable MP4 clips.

It is designed for a single user with a large personal archive — someone who records every gaming session and later wants to find the "remember that time…" moments without watching hundreds of hours of footage.

---

## Core Objective

> **Turn raw gameplay recordings into a curated, searchable, shareable clip library with minimal human effort.**

The user points the tool at a folder of videos. The pipeline runs a series of AI/ML stages and produces, for each video, a ranked list of candidate moments. The user then reviews these moments in a web-based video editor, trims their boundaries, renames them, and generates clips on demand. Clips can be shared via Open Graph-enabled links that embed inline on Discord, Twitter, etc.

---

## Architecture (High-Level)

The system is split into two runtimes that share nothing except a **SQLite database** and a **directory layout**:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  CLI Pipeline (Python 3.12 + uv + PyTorch)                                  │
│  ─────────────────────────────────────────                                  │
│  Ingest → Extract Audio → Transcribe → Detect Events → Detect Vision →      │
│  Score → Rank (LLM) → Clip (ffmpeg)                                         │
│                                                                             │
│  Runs inside Docker or on host. GPU-bound stages are sequential; CPU/IO     │
│  stages parallelize with --workers.                                         │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼ reads/writes
                        ┌─────────────────────┐
                        │  SQLite (WAL mode)  │  ← data/index.db
                        │  + artifacts on disk │  ← audio/, clips/, thumbs/
                        └─────────────────────┘
                                    │
                                    ▼ reads + media routes
┌─────────────────────────────────────────────────────────────────────────────┐
│  Web UI (Nuxt 3 + Bun + Nitro)                                              │
│  ─────────────────────────────                                              │
│  Dashboard (video grid) → Video Editor (player + moments list) →            │
│  Share page (/share/:id with OG/Twitter Card meta tags)                     │
│                                                                             │
│  Serves UI, JSON API, MP4 streaming (range requests), lazy thumbnails.      │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Key Design Principle: The DB is the Contract

The Python CLI and the Nuxt server never call each other. They communicate only through:
- **SQLite schema** (tables: `video`, `transcript_segment`, `audio_event`, `vision_frame`, `vision_detection`, `vision_ocr`, `candidate_clip`, `job`, `library`)
- **Path conventions** (`data/audio/<id>.wav`, `data/clips/video_<id>/`, `data/thumbs/`)
- **ffmpeg thumbnail filter chains**

This means either half can be rewritten or replaced without touching the other.

---

## Pipeline Stages (Detailed)

Each stage is **idempotent and resumable**. The `video` table has boolean `_done` flags (`audio_done`, `transcribe_done`, etc.). Re-running a stage skips already-completed videos unless `--force` is passed.

### 1. `ingest` — File Discovery & Metadata
- Recursively walks a directory tree (or targets a single file).
- Runs `ffprobe` to extract duration, resolution, FPS, codecs.
- Computes SHA1 prefix hash (first 16 MB) for change detection.
- Derives `game` from parent directory name; derives `recorded_at` from filename or mtime.
- Supports library directories that are periodically re-scanned (via `library` table + cron scheduler in Nuxt).
- Change detection: size + mtime + SHA1 prefix. Unchanged files are no-ops; updated files reset downstream stage flags.

### 2. `extract-audio` — Audio Decoding
- Decodes each video to 16 kHz mono WAV with ffmpeg.
- Cached on disk at `data/audio/<video_id>.wav`.
- Parallelized (`--workers`) because it is CPU/IO bound.

### 3. `transcribe` — Speech-to-Text
- Runs `faster-whisper` (Whisper `large-v3` by default) on the cached WAV.
- GPU-bound; intentionally sequential because one GPU is saturated.
- Stores segment-level transcripts with start/end timestamps, log-probs, and VAD-filtered no-speech scores.
- Enables keyword matching ("oh my god", "holy shit", "let's go", etc.) in later scoring.

### 4. `detect-events` — Audio Event Detection
- Runs **PANNs Cnn14** (pre-trained sound event detection) on the cached WAV.
- Detects: `Laughter`, `Shout`, `Cheer`, `Gunfire`.
- Also runs RMS loud-spike detection for generic loud audio events.
- GPU-bound; sequential.
- Stores per-event rows with start/end/label/score.

### 5. `detect-vision` — Visual Analysis (Florence-2)
- Samples frames from the video (fixed 1 Hz grid + scene-change timestamps from PySceneDetect).
- Runs **Florence-2-large** on each frame for three tasks:
  - `<OD>` — object detection (people, vehicles, etc.)
  - `<OCR_WITH_REGION>` — on-screen text (death screens, victory banners, HUD text)
  - `<MORE_DETAILED_CAPTION>` — dense caption describing the scene
- GPU-bound; sequential.
- Writes `vision_frame`, `vision_detection`, and `vision_ocr` rows.
- Scene changes are weighted in scoring to catch visual transitions.

### 6. `score` — Moment Identification (The Heart of the System)

This is the most important and most unusual stage. It does **NOT** use fixed-length windows.

Instead it uses a **density-based, multi-scale hysteresis segmentation** algorithm:

1. **Build Density Signal** `I[t]`:
   - Rasterize all signals onto a per-second array:
     - Audio events (laughter, shouting, cheering, gunfire, loud spikes)
     - Transcript signals (speech presence, keyword hits, speech density)
     - Vision signals (OCR keywords like "WASTED" / "YOU DIED", scene changes, caption keywords, detected people/vehicles)
   - Apply weighted sums (e.g., `Shout = 6.0`, `Laughter = 4.0`, keyword hit = 2.0).
   - Gaussian-smooth the signal (`sigma = 3.0s`).

2. **Multi-Scale Hysteresis Segmentation**:
   - Run three threshold passes on the smoothed density:
     - **Tight** (95th → 85th percentile): 5–15 s clips — single reactions
     - **Medium** (90th → 75th percentile): 15–35 s clips — joke + payoff
     - **Loose** (85th → 65th percentile): 35–90 s clips — sustained bits
   - Hysteresis: seed where density > high threshold, extend while density > low threshold, bridging short gaps up to 4 seconds so a pause inside a laugh doesn't split the clip.
   - Clamp lengths to `[8 s, 90 s]`.

3. **Pool + Non-Maximum Suppression (NMS)**:
   - Collect all regions from all three scales.
   - Sort by total density sum (rewards sustained interestingness, not just short spikes).
   - Greedy NMS with IoU threshold 0.5: keep the best-scoring region of each cluster, discard overlapping duplicates.
   - Return up to `TOP_N_CANDIDATES = 30` clips per video.

4. **Boundary Snapping**:
   - Optionally shift start/end edges toward the nearest transcript gap (silence between speech segments) so clips don't start mid-word.

5. **Stable Identity Across Re-Scoring**:
   - When re-running `score`, new regions are matched to existing `candidate_clip` rows by IoU ≥ 0.5.
   - Matched clips keep their `user_rating`, `llm_title`, `llm_desc`, `llm_contents`, `llm_tags`, and `clip_path`.
   - Unmatched existing clips are deactivated (`active = 0`), not deleted.
   - Manual moments (`source = 'manual'`) are excluded from matching and never deactivated.
   - This makes tuning weights safe: user data and LLM metadata survive.

### 7. `rank` — LLM Re-Ranking (Claude)
- Shells out to the local `claude` CLI (Claude Code), NOT the Anthropic HTTP API.
- No API key needed; uses the user's existing Claude subscription auth.
- Sends all active candidates for one video in a single prompt.
- Claude returns a JSON array with:
  - `rank` (1..N permutation)
  - `keep` (boolean)
  - `title` (≤ 8 words)
  - `reason` (one sentence)
  - `contents` (2–3 sentence narrative)
  - `tags` (array of strings)
- The prompt includes transcript text, on-screen OCR, peak vision caption, and heuristic score for context.
- Claude judges whether the combined audio + visual context suggests a genuinely funny/memorable/cool moment.

### 8. `clip` — MP4 Extraction
- Runs `ffmpeg` to cut each candidate window into an MP4.
- Pads boundaries: `+3s` before, `+8s` after (generous trail-off so punchlines land).
- Clamps to video bounds; respects min/max length constraints.
- Default codec: `libsvtav1` with CRF 28 (small, high-quality, browser-compatible).
- Falls back to `libopenh264` or bitrate mode if codec unavailable.
- Supports `--stream-copy` for fast keyframe-boundary cuts (lossless, looser edges).
- Parallelized (`--workers`) because ffmpeg encoding is CPU/IO bound.
- Clips are generated **on demand** — the pipeline identifies moments but does not cut them by default. Users cut via the UI editor or `bken create-clip <id>`.

---

## Data Model (What the Application Thinks About)

### `video` — The Source Recording
- `path`, `size_bytes`, `mtime`, `sha1_prefix`, `sha1_full`
- `duration_s`, `width`, `height`, `fps`, `video_codec`, `audio_codec`
- `game` (parent dir), `recorded_at` (parsed from filename)
- `library_id`, `missing` (file moved/deleted)
- Stage flags: `audio_done`, `transcribe_done`, `events_done`, `vision_done`, `score_done`, `rank_done`

### `transcript_segment` — ASR Output
- `video_id`, `start_s`, `end_s`, `text`, `avg_logprob`, `no_speech`

### `audio_event` — PANNs + RMS
- `video_id`, `start_s`, `end_s`, `label` (Laughter/Shout/Cheer/Gunfire/LoudSpike), `score`

### `vision_frame` — Sampled Frames
- `video_id`, `ts_s`, `sampled` (fixed/scene), `caption`, `n_objs`, `n_ocr`, `raw_json`

### `vision_detection` — Object Detections
- `frame_id`, `video_id`, `ts_s`, `label`, `conf`, `bbox_*`, `area_frac`

### `vision_ocr` — On-Screen Text
- `frame_id`, `video_id`, `ts_s`, `text`, `text_upper`, `conf`, `bbox_*`, `area_frac`

### `candidate_clip` — Identified Moment
- `video_id`, `start_s`, `end_s`, `score`, `features` (JSON breakdown)
- `transcript` (concatenated text for the window)
- `llm_rank`, `llm_title`, `llm_desc`, `llm_contents`, `llm_tags`
- `clip_path`, `thumb_path`
- `user_rating` (1–5 stars, NULL = unrated)
- `active` (0 = deactivated by re-scoring, 1 = current)
- `title` (user-editable display name; falls back to `llm_title`)
- `source` (`auto` = pipeline-generated, `manual` = user-created in UI)
- `clip_stale` (1 = boundaries changed since last cut)

### `job` — Queue for Async Work
- `video_id`, `library_id`, `type` (ingest, extract_audio, transcribe, detect_events, detect_vision, score, rank, create_clip, reprocess, library_sync)
- `status` (pending | running | done | failed), `target_id`, `error_message`
- The Nuxt UI polls `/api/jobs` to show real-time pipeline status.

### `library` — Watched Directories
- `name`, `path`, `scan_interval_minutes`, `last_scan_at`, `active`
- Nuxt scheduler auto-spawns `bken library sync` via cron.

---

## Web UI (Nuxt 3)

### Dashboard (`/`)
- Grid of all ingested videos with thumbnails (lazy ffmpeg-rendered).
- Filters: library, missing-file toggle.
- Per-video badges: game, duration, moment count, pipeline stage completion (audio, transcribe, events, score).
- Clicking a card opens the video editor.

### Video Editor (`/video/:id`)
- **Inline HTML5 video player** streaming the full source video (`/stream/video/:id.mp4`).
- **Timeline strip**: visual bars showing all moments; click to seek; playhead auto-scrolls the list.
- **Moments sidebar**: list of all `active` candidate clips with sort controls (start time, score, LLM rank, user rating, duration, title, etc.).
- Two view modes:
  - **Small**: compact cards with time range, title, badges (manual, clipped, stale, score).
  - **Detailed**: expanded cards showing LLM title, description, contents, tags, transcript, score details, editing controls.
- **Editing controls** (for selected moment):
  - Nudge start/end by ±0.1s or ±1s
  - Set start/end to current playhead position
  - Edit title inline
  - Create / update clip (calls `ffmpeg` via server route)
  - Download clip
  - Share link (copies `/share/:id` URL with Open Graph meta tags)
  - Delete moment
- **Manual moment creation**: Mark In / Mark Out at playhead, type a title, create. Manual moments survive re-scoring.
- **Reprocess button**: Reset score/rank flags for this video and re-run the pipeline (manual moments preserved).

### Share Page (`/share/:id`)
- Server-side rendered HTML page with Open Graph and Twitter Card meta tags.
- Includes `og:video`, `twitter:player`, poster image, title, description, tags.
- Enables inline video playback previews when pasted into Discord, Twitter, Slack, iMessage, etc.
- Includes a simple branded video player and download/copy-link buttons.

### Jobs Page (`/jobs`)
- Polls `/api/jobs` to show pending/running/failed pipeline jobs.

### API Surface
- `GET /api/videos` — list videos with filters
- `GET /api/videos/:id` — video metadata + stage flags
- `GET /api/videos/:id/moments` — active moments for a video
- `PATCH /api/moments/:id` — update title, start_s, end_s
- `DELETE /api/moments/:id` — delete moment
- `POST /api/moments` — create manual moment
- `POST /api/moments/:id/clip` — cut clip via ffmpeg
- `POST /api/videos/:id/reprocess` — queue reprocessing
- `GET /api/clips` — filter + sort all clips (dashboard view)
- `GET /api/games` — sidebar rollup by game
- `GET /api/jobs` — job queue status
- `GET /api/libraries` / `POST /api/libraries` / `DELETE /api/libraries/:id` / `POST /api/libraries/:id/sync`
- `GET /stream/video/:id.mp4` — range-request streaming of source video
- `GET /clip/:id.mp4` — serve cut clip (with `?download=1` for attachment)
- `GET /thumb/:id.jpg` — lazy ffmpeg thumbnail (cached to `data/thumbs/`)
- `GET /share/:id` — server-side share page with OG meta tags

---

## Key Behaviors & Invariants

### Resumability
- Every stage checks `_done` flags. Re-running is a no-op unless `--force`.
- Ingest detects changes via `size + mtime + sha1_prefix`. Changed files reset downstream flags automatically.
- Audio extraction caches WAVs. Clip cutting caches MP4s.

### Re-Scoring Safety
- `score` uses IoU matching to preserve user ratings and LLM metadata across re-runs.
- Manual moments (`source = 'manual'`) are excluded from the diff and never deactivated.
- `--reset` flag exists for destructive full rebuilds.

### Library Sync
- Directories can be registered as "libraries" with configurable scan intervals.
- The Nuxt server runs a cron scheduler that spawns `bken library sync`.
- Sync detects new files, moved files (via SHA1), and missing files.

### Vision-Aware Scoring
- The scorer is genuinely multi-modal: audio events, transcript keywords, on-screen OCR (death/victory text), scene changes, object detections, and captions all contribute to the density signal.
- This catches moments that audio alone misses (e.g., a silent crash where the screen shows an explosion, or a victory screen with no shouting).

### Variable-Length Clips
- Unlike systems that output rigid 30-second windows, bken produces clips whose length is determined by the natural arc of the moment.
- A quick reaction might be 8 seconds. A sustained argument might be 60 seconds. The hysteresis segmentation finds these naturally.

### Single-User, Local-First
- No auth, no multi-user ratings, no cloud dependency.
- Designed to run on localhost or behind a trusted reverse proxy.
- The LLM ranker uses the local `claude` CLI rather than an API key.
- SQLite + local file system means zero external database or object store.

---

## Technology Stack

| Layer | Technology |
|-------|------------|
| CLI runtime | Python 3.12 |
| Package manager | `uv` (Astral) |
| CLI framework | Typer + Rich |
| ML / GPU | PyTorch 2.4+ (CUDA 12.4), faster-whisper, PANNs (panns-inference), transformers (Florence-2) |
| Audio processing | ffmpeg, librosa, soundfile |
| Vision | PyAV, PySceneDetect, OpenCV (headless), Pillow, Florence-2 |
| Web framework | Nuxt 3 (Bun runtime) |
| UI components | `@nuxt/ui`, Tailwind CSS, Vidstack (player), IBM Plex Sans |
| DB | SQLite (WAL mode, shared between Python and Bun) |
| Containers | Docker + Docker Compose |

---

## File Layout (Actual)

```
cli/
  src/bken/           # Python CLI package
    cli.py               # Typer entrypoint (all subcommands + pipeline orchestration)
    config.py            # Paths, env vars, tunables (sample rate, thresholds, codec prefs)
    db.py                # SQLite schema, connection helpers, forward migrations
    ingest.py            # ffprobe + SHA1-prefix hash, threaded walk, library sync
    audio.py             # ffmpeg decode to 16 kHz mono WAV
    transcribe.py        # faster-whisper ASR with VAD
    events.py            # PANNs CNN14 SED + RMS loud-spike detection
    scoring.py           # Orchestrates density-based scoring (calls density.py)
    density.py           # Hysteresis-based multi-scale segmentation + IoU matching
    clipper.py           # ffmpeg cut with padding + boundary snapping
    ranker.py            # Shells out to `claude` CLI for LLM re-ranking
    thumbs.py            # ffmpeg thumbnail generation (on-demand for web)
    vision.py            # Florence-2 object detection, OCR, captioning
    library.py           # Library CRUD + sync logic
  tests/                 # pytest suite
  pyproject.toml         # uv project config

app/                     # Nuxt 3 web UI
  app/
    pages/               # index.vue (dashboard), video/[id].vue (editor), jobs.vue, libraries.vue
    composables/         # useDashboard.ts, useVideoEditor.ts
    components/          # ConfirmModal.vue
    layouts/             # default.vue
    plugins/             # vidstack.client.ts
  server/
    api/                 # Nitro JSON API routes
    routes/              # Media routes (clip, thumb, stream, share page)
    plugins/             # scheduler.ts (node-cron library sync)
    utils/               # db.ts, paths.ts, thumbs.ts
  package.json           # bun dependencies

data/                    # Runtime artifacts (gitignored)
  index.db               # SQLite database
  audio/                 # Cached 16 kHz WAVs
  clips/                 # Extracted MP4 clips
  thumbs/                # Lazy-rendered JPG thumbnails
  artifacts/             # Misc pipeline outputs

models/                  # Downloaded model weights (Whisper, PANNs, Florence-2)
```

---

## Deployment

### Docker Compose (Recommended)
```bash
cp .env.example .env   # set ARCHIVE_DIR and CLAUDE_HOME
docker compose up       # Nuxt on :3000, CLI container stays running
```
- `app` service: Nuxt dev server on port 3000.
- `cli` service: Python container with GPU support available via override file.
- Both mount `./data` for shared DB + artifacts.
- GPU stages need `nvidia-container-toolkit` + `docker-compose.gpu.yml`.

### Host Dev (No Docker)
```bash
# Terminal 1: Nuxt
cd app && bun install && bun run dev   # → http://localhost:3000

# Terminal 2: Pipeline
uv run bken pipeline /path/to/video.mp4 --single-file
```

---

## Summary for an LLM

If you are working on this codebase, remember:

1. **The goal is clip extraction from gameplay recordings**, not general video editing or streaming.
2. **The pipeline is the product.** The UI is a review layer on top of it. Most intelligence lives in the Python CLI (`density.py`, `ranker.py`, `vision.py`, `events.py`).
3. **The DB schema is the contract.** Python writes; Nuxt reads. Keep schema changes synchronized in both `cli/src/bken/db.py` and `app/server/utils/db.ts`.
4. **Re-scoring must be safe.** User ratings, LLM titles, and manual moments must survive re-running `score`. Use IoU matching and the `active` / `source` columns correctly.
5. **Clips are variable-length.** The hysteresis segmentation is the core differentiator. Do not replace it with fixed windows without understanding why it exists.
6. **Vision matters.** OCR on death screens, scene changes, and object detections are first-class signals alongside audio and transcript.
7. **It is single-user and local-first.** Do not add auth, cloud APIs, or multi-user complexity unless explicitly asked.
8. **GPU stages are intentionally sequential.** `transcribe`, `detect-events`, and `detect-vision` each saturate one GPU. Parallelize `ingest`, `extract-audio`, and `clip` instead.
9. **The `rank` stage uses the `claude` CLI, not an HTTP API.** It shells out to a local binary and parses JSON from stdout.
10. **Share links need OG meta tags.** The `/share/:id` route is server-side rendered HTML, not a Vue page, because crawlers need to read `<meta>` tags.
