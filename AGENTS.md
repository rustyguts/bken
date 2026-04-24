# AGENTS.md — bken

## Project Overview

`bken` extracts funny / memorable moments from gameplay recordings. It combines speech-to-text (Whisper via whisper.cpp), audio-event detection (PANNs CNN14 via ONNX for laughter / shouting / cheering / gunfire), optional vision (Florence-2 via ONNX for OCR + object detection), and local-LLM re-ranking (Ollama + Qwen2.5 1.5B) into a SQLite-backed, resumable pipeline.

The entire system is a **single Go binary**. `bken serve` runs the web UI (Datastar + Tailwind + DaisyUI), the JSON API, media streaming, and the asynq worker loop in one process. `bken worker` runs the worker only; individual pipeline stages are also callable as CLI subcommands for one-off use.

## Architecture

```
┌─────────┐   ┌─────────┐   ┌────────────┐   ┌──────────┐
│ ingest  │──▶│ extract │──▶│ transcribe │──▶│          │
└─────────┘   │  audio  │   │ (whisper)  │   │   score  │──▶  rank (Ollama)
              └─────────┘   └────────────┘   │ (density)│
                     │      ┌────────────┐   │          │
                     └─────▶│ detect-    │──▶│          │
                            │ events     │   └──────────┘
                            │ (PANNs)    │
                            └────────────┘
                                    │
                                    ▼
                              ┌──────────┐
                              │ Datastar │──▶  review moments
                              │   UI     │──▶  trim / rename
                              └──────────┘──▶  create clip (ffmpeg)
```

- **Binary** (`cmd/bken`): Cobra CLI. Every stage is a subcommand AND an asynq task type, so the UI can enqueue the same work the CLI runs.
- **Queue** (`internal/queue`): [asynq](https://github.com/hibiken/asynq) on Redis. Every task insert writes a `job` row first so the UI can show status; the asynq task id is stored as `job.asynq_id`.
- **DB** (`internal/db`): SQLite WAL, schema identical to the previous Python pipeline — same column names, same forward-only migrations, same invariants. Uses `modernc.org/sqlite` (pure Go, no CGo).
- **Web** (`internal/web`): [Echo v4](https://echo.labstack.com/). `/api/*` JSON handlers + `/clip/*`, `/thumb/*`, `/stream/video/*`, `/share/*` media. HTTP Range via `http.ServeContent`.
- **UI** (`internal/web/ui`): `html/template` pages with Datastar SSE for reactivity. Tailwind CSS + DaisyUI built via bun. Static assets and templates are `//go:embed`'d into the binary.

## Tech Stack

| Layer | Technology |
|-------|------------|
| Language | Go 1.25 |
| CLI | `spf13/cobra` |
| Web | `labstack/echo/v4` |
| Queue | `hibiken/asynq` + DragonflyDB (Redis-compatible) |
| DB | SQLite via `modernc.org/sqlite` (pure Go, WAL mode) |
| UI | Datastar + Tailwind 4 + DaisyUI 5 + IBM Plex Sans |
| ASR | whisper.cpp via `github.com/ggerganov/whisper.cpp/bindings/go` (build tag `whisper`) |
| ML inference | ONNX Runtime via `github.com/yalue/onnxruntime_go` (build tag `onnx`) |
| Audio math | `gonum.org/v1/gonum/dsp/fourier`, `go-audio/wav` |
| Video / audio I/O | `ffmpeg` / `ffprobe` via `os/exec` |
| Containers | Docker + Docker Compose + Redis |

## File Layout

```
cmd/bken/              # main entrypoint
internal/
  cli/                 # Cobra subcommands wire
  config/              # paths + tunables (env-overridable)
  db/                  # schema + migrations (identical to legacy Python schema)
  ffmpeg/              # ffprobe + ffmpeg exec wrappers
  ingest/              # dir walk, sha1, ffprobe, video upsert
  audio/               # 16 kHz mono WAV extraction
  asr/                 # whisper.cpp ASR (build tag `whisper`)
  events/              # PANNs CNN14 ONNX + RMS loud-spike (build tag `onnx`)
  vision/              # Florence-2 ONNX + OCR/detection (build tag `onnx`)
  scoring/             # density + IoU + NMS + sliding-window fallback
  clipper/             # ffmpeg cut with padding + boundary snap
  thumbs/              # on-demand JPG thumbnails
  rank/                # local LLM via Ollama (OpenAI-compatible HTTP)
  library/             # registered archive dirs + scheduled scans
  queue/               # asynq wiring, task types, payloads, job-row sync
  web/                 # Echo server, API handlers, media handlers, share page
  web/ui/              # embedded templates + static CSS + Datastar handlers
scripts/
  export_onnx.py       # one-time PyTorch → ONNX export (PANNs, Florence-2)
data/                  # runtime artifacts (gitignored)
  index.db             # SQLite database
  audio/               # cached 16 kHz WAVs
  clips/               # cut MP4 clips
  thumbs/              # lazy-rendered JPG thumbnails
  redis/               # asynq AOF
models/                # downloaded weights (ggml-large-v3.bin, panns_cnn14.onnx, …)
cli/                   # legacy Python package (kept during transition)
app/                   # legacy Nuxt 4 UI (kept during transition)
```

## Development Setup

### Prerequisites
- Go 1.25+
- DragonflyDB or Redis 7 (local, or the compose `dragonfly` service)
- `ffmpeg` + `ffprobe` on `$PATH`
- Bun (only to rebuild the Tailwind CSS)
- Optional: NVIDIA GPU + CUDA for ONNX Runtime GPU and whisper.cpp GPU builds
- Optional: Ollama running locally (or via compose) for the `rank` stage. Auto-pulls `qwen2.5:1.5b-instruct` on first use.

### Build

```bash
# default build (no ML — ASR/events/vision stages return a clear error)
go build ./cmd/bken

# build with whisper.cpp ASR (requires libwhisper.so + whisper.h)
go build -tags whisper ./cmd/bken

# build with ONNX (requires libonnxruntime.so at runtime)
go build -tags onnx ./cmd/bken

# both
go build -tags whisper,onnx ./cmd/bken
```

### Tailwind CSS

```bash
bun install
bun run build:css       # produces internal/web/ui/static/dist/app.css
```

The build is only needed when templates or `input.css` change; the compiled file is embedded into the binary at `go build` time.

### Run

```bash
# full stack (web + worker + scheduler) on :3000
bken serve

# worker only (no HTTP listener)
bken worker --concurrency 4

# individual stages
bken init-db
bken ingest /path/to/archive --workers 8
bken extract-audio --workers 4
bken transcribe              # needs -tags whisper + model file
bken detect-events           # needs -tags onnx + panns_cnn14.onnx
bken score
bken rank                    # needs Ollama reachable at BKEN_LLM_BASE_URL
bken create-clip <candidate_clip_id>
bken library add "name" /path --scan-interval 60
```

### ONNX model export

The PyTorch models (PANNs CNN14, Florence-2) are exported to ONNX once via:

```bash
uv run python scripts/export_onnx.py --models ./models
```

This writes `panns_cnn14.onnx`, `class_labels_indices.csv`, and (best-effort) `florence2_base.onnx` into `./models`. Re-run only if you change model versions.

### Whisper model

Download a `ggml-*.bin` model from the whisper.cpp repo into `./models`, e.g.

```bash
bash path/to/whisper.cpp/models/download-ggml-model.sh large-v3 ./models
```

## Docker Compose

```bash
cp .env.example .env              # edit ARCHIVE_DIR
docker compose up --build         # bken + dragonfly on :3000
```

Scale the worker pool:

```bash
docker compose --profile scale up --scale worker=3
```

GPU stages (requires `nvidia-container-toolkit`):

```bash
docker compose -f docker-compose.yml -f docker-compose.gpu.yml up
```

The default image does **not** bake in whisper.cpp or ONNX Runtime — those are opt-in via build tags. To ship a GPU image, extend the Dockerfile with a `Dockerfile.gpu` that installs the libs and adds `-tags whisper,onnx` to `go build`.

## Code Conventions

- **Database** is the source of truth. Every stage is idempotent and updates `*_done` flags; re-running a stage is a no-op unless `--force` is passed.
- **Paths** are resolved via `config.ResolveData` so a DB copied across machines still finds its clips.
- **Context-aware**: every public function takes `context.Context` first; long-running work respects cancellation.
- **No unnecessary comments** — named identifiers carry intent; comments only where "why" is non-obvious.
- **Jobs**: never bypass the queue from the web layer. `POST /api/moments/:id/clip` enqueues `TypeCreateClip`; the worker runs `clipper.Create`.
- **Build tags**: ML code gated on `whisper` / `onnx`. Stub files on the default build return informative errors pointing at the tag flags.
- **Scheduler** (`internal/web/scheduler.go`): wakes every 60 s, enqueues `TypeLibrarySync` for any library whose `next_scan_at` has passed.

## Key Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `BKEN_DATA` | `./data` | DB + artifacts root |
| `BKEN_MODELS` | `./models` | Model cache |
| `BKEN_REDIS` | `127.0.0.1:6379` | asynq broker (Redis or DragonflyDB) |
| `BKEN_HTTP` | `:3000` | Echo listen address |
| `BKEN_BASE_URL` | inferred | Origin for share links |
| `BKEN_WHISPER_MODEL` | `large-v3` | `ggml-*.bin` prefix |
| `BKEN_CLIP_VCODEC` | `libsvtav1` | Video codec for cuts |
| `BKEN_CLIP_CRF` | `28` | Quality |
| `BKEN_LLM_BASE_URL` | `http://ollama:11434` | Ollama HTTP base URL |
| `BKEN_LLM_MODEL` | `qwen2.5:1.5b-instruct` | Model tag Ollama serves |

Scoring + clip tunables live in `internal/config/config.go`.

## Important Notes for Agents

- **Data and models are gitignored**. Don't commit `data/` or `models/`.
- **UI is embedded**: editing `internal/web/ui/templates/*.html` requires `go build` to pick up changes (no hot reload). Tailwind changes require `bun run build:css` first.
- **Clip paths in old DBs are absolute from a different environment**. Always use `config.ResolveData`.
- **WAL mode is on.** When copying the DB, also copy `-wal` and `-shm`.
- **Share links** live at `/share/:id` with OG + Twitter Card meta. Set `BKEN_BASE_URL` behind a reverse proxy.
- **Manual moments survive reprocessing**. `candidate_clip.source='manual'` rows are excluded from the scoring diff — re-running `score` won't deactivate them.

## Maintaining the Changelog

Every time you make a significant change to the codebase (adding features, fixing bugs, or refactoring), you **must** update `changelog.md` under the `## [Unreleased]` section.

- **Added**: New features or capabilities.
- **Changed**: Changes in existing functionality.
- **Deprecated**: Soon-to-be-removed features.
- **Removed**: Removed features.
- **Fixed**: Bug fixes.
- **Security**: Vulnerability fixes.

Keep descriptions concise and technical, focusing on the "what" and "why".
- **Legacy code still lives** in `cli/` (Python) and `app/` (Nuxt). They share the same `./data` directory and will continue to work while the Go rewrite stabilizes. Delete those trees once the Go binary has fully replaced them.
