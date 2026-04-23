# AGENTS.md — bken

## Project Overview

`bken` is a Python CLI pipeline that extracts funny / memorable moments from gameplay recordings. It combines speech-to-text (Whisper), audio event detection (PANNs for laughter/shouting/cheering), and optional Claude-based re-ranking into a SQLite-backed, resumable workflow.

The **Nuxt 3 web UI** is the primary interface: a per-video moment editor where users review AI-suggested moments, trim their boundaries, rename them, and generate clips on-demand. A job queue panel shows real-time pipeline status.

## Architecture

```
┌─────────┐   ┌─────────┐   ┌────────────┐   ┌──────────┐
│ ingest  │──▶│ extract │──▶│ transcribe │──▶│          │
└─────────┘   │  audio  │   │ (Whisper)  │   │   score  │──▶  rank (Claude)
              └─────────┘   └────────────┘   │          │
                     │      ┌────────────┐   │          │
                     └─────▶│ detect-    │──▶│          │
                            │ events     │   └──────────┘
                            │ (PANNs/RMS)│
                            └────────────┘
                                    │
                                    ▼
                              ┌──────────┐
                              │  Nuxt UI │──▶  review moments
                              │ (editor) │──▶  trim / rename
                              └──────────┘──▶  create clip (ffmpeg)
```

- **CLI** (`src/bken/`): Typer CLI with Rich progress bars. Each stage reads/writes a single SQLite DB (`data/index.db`). Stages are idempotent and resumable.
- **Web UI** (`nuxt/`): Nuxt 3 + Bun. Server routes in `nuxt/server/` read from the same SQLite DB and shell out to `ffmpeg` for thumbnails and clip cutting. The CLI and Nuxt share `./data` via Docker bind mounts.
- **Legacy frontend** (`frontend/`): Vue 3 + Vite SPA. Still present but no longer actively used; the Nuxt app is the current UI.

## Tech Stack

| Layer | Technology |
|-------|------------|
| CLI runtime | Python 3.12 |
| Package manager | `uv` (Astral) |
| ML / GPU | PyTorch 2.4+ (CUDA 12.4), faster-whisper, PANNs (panns-inference) |
| Web framework | Nuxt 3 (Bun runtime) |
| DB | SQLite (WAL mode, single file) |
| Containers | Docker + Docker Compose |

## File Layout

```
src/bken/           # Python CLI package
  cli.py               # Typer entrypoint (all subcommands)
  config.py            # Paths, env vars, tunables (sample rate, thresholds, codec prefs)
  db.py                # SQLite schema, connection helpers, forward migrations
  ingest.py            # ffprobe + SHA1-prefix hash, threaded walk
  audio.py             # ffmpeg decode to 16 kHz mono WAV
  transcribe.py        # faster-whisper ASR with VAD
  events.py            # PANNs CNN14 SED + RMS loud-spike detection
  scoring.py           # Sliding-window interestingness (legacy; still used for fallback)
  density.py           # Hysteresis-based multi-scale segmentation (current scorer)
  clipper.py           # ffmpeg cut with padding + boundary snapping
  ranker.py            # Shells out to `claude` CLI for LLM re-ranking
  thumbs.py            # ffmpeg thumbnail generation (on-demand for web)
  web.py               # FastAPI app (legacy; Nuxt server routes are current)

nuxt/                  # Nuxt 3 web UI (current)
  server/              # Nitro server routes (API + media + share page)
  components/          # Vue components
  pages/               # File-based routing (dashboard + video editor)
  composables/         # Shared Vue composables (dashboard + editor)

frontend/              # Legacy Vue 3 SPA (kept for reference)

tests/                 # pytest suite
  test_smoke.py        # Pure-Python unit tests (no GPU / no heavy ML)

data/                  # Runtime artifacts (gitignored)
  index.db             # SQLite database
  audio/               # Cached 16 kHz WAVs
  clips/               # Extracted MP4 clips
  thumbs/              # Lazy-rendered JPG thumbnails
  artifacts/           # Misc pipeline outputs

models/                # Downloaded model weights (Whisper, PANNs)
```

## Development Setup

### Prerequisites
- Python 3.12
- `uv` (https://docs.astral.sh/uv/)
- `ffmpeg` + `ffprobe` on `PATH`
- NVIDIA GPU recommended for `transcribe` and `detect-events`
- Bun (for Nuxt frontend)

### Python CLI
```bash
uv sync                          # installs deps + torch+cu124
uv pip install -e .              # editable install
uv run bken --help            # verify
```

### Nuxt UI
```bash
cd nuxt
bun install
bun run dev                      # → http://localhost:3000
```

### Tests
```bash
uv run pytest
```

Heavy ML stages are not unit-tested; run the full pipeline on a real video for end-to-end coverage.

## Running the Pipeline

```bash
# Full pipeline, one file (stops before clip cutting)
uv run bken pipeline /path/to/video.mp4 --single-file

# Bulk ingest + process
uv run bken ingest /archive --workers 8
uv run bken extract-audio --workers 4
uv run bken transcribe
uv run bken detect-events
uv run bken score
uv run bken rank              # optional; needs `claude` CLI on PATH

# Cut a single clip after UI review
uv run bken create-clip <clip_id>
```

## Docker Compose

```bash
cp .env.example .env             # edit ARCHIVE_DIR and CLAUDE_HOME
docker compose up                # Nuxt on :3000, CLI container stays running
```

Run pipeline stages inside the container:
```bash
docker compose exec cli uv run bken ingest /archive --workers 8
```

For GPU stages, use the override file:
```bash
docker compose -f docker-compose.yml -f docker-compose.gpu.yml up
```

## Code Conventions

- **Python**: Use `from __future__ import annotations` at the top of every file. Type hints encouraged.
- **DB access**: Use the `@contextmanager` `db()` from `db.py` or `connect()` directly. WAL mode is enabled; `timeout=30` handles brief writer contention.
- **Paths**: Use `pathlib.Path`. All runtime artifacts resolve through `config.py` (env-overridable via `BKEN_DATA`, `BKEN_MODELS`).
- **Stage flags**: Each `video` row has boolean `_done` columns (`audio_done`, `transcribe_done`, etc.). Downstream stages query for rows where the previous stage is done and their own flag is 0.
- **Resumability**: Re-running a stage is a no-op unless `--force` is passed. Ingest detects changes via `size + mtime + sha1_prefix`.
- **Logging / UX**: The CLI uses `rich` progress bars and spinners. Keep output friendly; add `-q` / `-v` toggles where appropriate.
- **Migrations**: Forward-only, idempotent `ALTER TABLE` blocks in `db.py::_MIGRATIONS`. New columns must have sensible defaults so old rows stay valid.
- **Clip stability**: The scoring pipeline uses IoU-based matching (`density.py::match_existing`) so re-running `score` preserves `user_rating`, `llm_title`, etc. for overlapping windows.

## Key Configuration

Environment variables (all optional, see `config.py` for defaults):

| Variable | Default | Purpose |
|----------|---------|---------|
| `BKEN_DATA` | `./data` | DB + artifacts root |
| `BKEN_MODELS` | `./models` | Model cache |
| `BKEN_WHISPER_MODEL` | `large-v3` | Whisper model size |
| `BKEN_WHISPER_COMPUTE` | `float16` | `int8` for small GPUs |
| `BKEN_CLIP_VCODEC` | `libsvtav1` | Video codec for cuts |
| `BKEN_CLIP_CRF` | `28` | Quality (0=lossless, 63=worst) |
| `BKEN_CLAUDE_CLI` | `claude` | Path to Claude Code CLI |
| `BKEN_CLAUDE_MODEL` | `sonnet` | Model forwarded to `claude -p` |
| `BKEN_BASE_URL` | inferred from request | Public origin for share links (e.g. `https://clips.example.com`) |

Scoring thresholds and window parameters are module-level constants at the top of `density.py`, `events.py`, and `config.py`.

## Important Notes for Agents

- **Do not commit `data/` or `models/`**. Both are gitignored and can be very large.
- **The Nuxt app is the current web UI.** The FastAPI app in `web.py` and the `frontend/` SPA are legacy. New UI work belongs in `nuxt/`.
- **GPU stages are intentionally single-threaded** (`transcribe`, `detect-events`). They saturate one GPU on their own. Parallelize `ingest`, `extract-audio`, and `clip` instead via `--workers`.
- **Clips are generated on-demand.** The pipeline identifies moments but does not cut mp4s by default. Users create clips via the UI editor or `bken create-clip <id>`.
- **Manual moments survive reprocessing.** `candidate_clip.source='manual'` rows are excluded from the scoring diff, so user-created moments are never deactivated by re-scoring.
- **Job queue** is tracked in the `job` table. The Nuxt UI polls `/api/jobs` to show real-time pipeline status.
- **Per-video reprocess** resets stage flags for a single video and re-runs from that stage onward. Available in the UI or via `bken reprocess-video <id> <stage>`.
- **The `rank` stage shells out to the `claude` CLI**, not an API client. It requires the user to have Claude Code installed and authenticated.
- **Clip paths in the DB may be absolute** from a different environment. Always use `config.resolve_data()` to turn a stored path into a valid local `Path`.
- **WAL mode is on.** If you copy the DB while the app is running, also copy `-wal` and `-shm` files or run `PRAGMA wal_checkpoint(FULL)` first.
- **Share links** are served server-side at `/share/:id` with Open Graph and Twitter Card meta tags for inline video playback on Discord, Twitter, etc. Set `BKEN_BASE_URL` when running behind a reverse proxy so absolute URLs are correct.
