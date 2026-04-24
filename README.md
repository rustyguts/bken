# bken

Extract funny / memorable moments from gameplay recordings. Speech-to-text
(whisper.cpp), audio-event detection (PANNs), optional vision (Florence-2),
local LLM re-ranking (Ollama + Qwen2.5 1.5B). Single Go binary, SQLite-backed, resumable.

---

## Pipeline

```
ingest ─▶ extract-audio ─▶ transcribe ─▶ score ─▶ rank ─▶ create-clip
                     └──▶ detect-events ─┘
                     └──▶ detect-vision ─┘
```

Each stage reads and writes a single SQLite DB (`data/index.db`). Every
stage is idempotent — re-running a stage is a no-op unless `--force` is
passed. Work is enqueued on [asynq](https://github.com/hibiken/asynq)
(Redis-backed) so the UI and CLI share one worker pool.

| Stage | Tool |
|-------|------|
| `ingest` | ffprobe + SHA1 prefix |
| `extract-audio` | ffmpeg → 16 kHz mono WAV |
| `transcribe` | whisper.cpp (build tag `whisper`) |
| `detect-events` | PANNs CNN14 via ONNX Runtime (build tag `onnx`) |
| `detect-vision` | Florence-2 via ONNX Runtime (build tag `onnx`) |
| `score` | density-based hysteresis + NMS |
| `rank` | Ollama + Qwen2.5 1.5B Instruct |
| `create-clip` | ffmpeg cut with padding + boundary snap |

---

## Setup

Requires:
- Go 1.25+
- DragonflyDB or Redis (or the compose service)
- `ffmpeg` + `ffprobe` on `$PATH`
- Optional: Bun (to rebuild Tailwind CSS)
- Optional: CUDA + libwhisper.so / libonnxruntime.so for GPU ML stages
- Optional: GPU for accelerated Ollama + ML stages

### Docker (recommended)

```bash
cp .env.example .env        # edit ARCHIVE_DIR
docker compose up --build   # bken + dragonfly on :3000
```

Open http://localhost:3000.

### Native

```bash
# build
bun install && bun run build:css        # only if templates changed
go build ./cmd/bken

# run the broker (DragonflyDB, redis-compatible)
dragonfly --logtostderr &

# run the server (UI + API + worker)
./bken serve
```

---

## CLI

```bash
bken init-db
bken ingest /archive --workers 8
bken extract-audio --workers 4
bken transcribe                         # -tags whisper
bken detect-events                      # -tags onnx
bken score
bken rank
bken create-clip <candidate_clip_id>

bken library add "GTA V" /archive/gta5 --scan-interval 60
bken library list
bken library sync <id>

bken serve                              # web UI + worker
bken worker --concurrency 4             # worker only
```

---

## Model files

Put into `./models`:

- `ggml-large-v3.bin` — Whisper weights. Use whisper.cpp's `download-ggml-model.sh`.
- `panns_cnn14.onnx` + `class_labels_indices.csv` — exported via `python scripts/export_onnx.py --models ./models`.
- `florence2_base.onnx` — exported by the same script (best-effort). If absent, `detect-vision` inserts frame rows with no captions so downstream stages still run.

---

## Layout

```
cmd/bken/              # main
internal/              # pipeline + web + queue packages
scripts/export_onnx.py # one-time PyTorch → ONNX export
Dockerfile             # multi-stage: bun CSS build → go build → alpine runtime
docker-compose.yml     # bken + dragonfly
docker-compose.gpu.yml # NVIDIA override
AGENTS.md              # full architectural notes
cli/                   # legacy Python CLI
internal/web/ui/       # Datastar + Tailwind + DaisyUI (built via Bun)
```

See `AGENTS.md` for architectural notes, conventions, and environment
variables.
