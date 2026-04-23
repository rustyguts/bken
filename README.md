# bken

Extract funny / memorable moments from gameplay recordings by combining
speech-to-text, laughter & audio-event detection, and (optionally)
Claude-based re-ranking.

Written for a personal archive of multi-hour gaming sessions: the
pipeline chews through a recording and emits a handful of ranked
30-second clip mp4s with titles and descriptions.

---

## Pipeline at a glance

```
┌─────────┐   ┌─────────┐   ┌────────────┐   ┌──────────┐
│ ingest  │──▶│ extract │──▶│ transcribe │──▶│          │
└─────────┘   │  audio  │   │ (Whisper)  │   │   score  │
              └─────────┘   └────────────┘   │ + clip   │──▶  mp4s
                     │      ┌────────────┐   │          │
                     └─────▶│ detect-    │──▶│          │──▶  rank (Claude)
                            │ events     │   └──────────┘
                            │ (PANNs/RMS)│
                            └────────────┘
```

Each stage reads from, and writes to, a single SQLite DB (`data/index.db`).
Every stage is resumable and idempotent — running a stage a second time
is a no-op unless you pass `--force`.

| Stage          | What it does                                                                  | Tool / model            |
|----------------|-------------------------------------------------------------------------------|-------------------------|
| `ingest`       | Walk a directory, `ffprobe` each video, store path + metadata + prefix hash  | ffmpeg/ffprobe          |
| `extract-audio`| Decode to 16 kHz mono WAV, cached on disk                                     | ffmpeg                  |
| `transcribe`   | Segment-level ASR with VAD filtering                                          | faster-whisper (GPU)    |
| `detect-events`| Laughter / Shout / Cheer probabilities + RMS loud-spike detection             | PANNs Cnn14 SED (GPU) + librosa |
| `score`        | Sliding-window interestingness score; top-N non-max-suppressed candidates     | numpy                   |
| `clip`         | Cut candidate windows into mp4s with padding + clamping                       | ffmpeg (libopenh264)    |
| `rank`         | (Optional) Claude re-ranks transcripts, writes titles/descriptions/tags       | `claude` CLI (Claude Code) |

---

## Setup

Requires:
- Python 3.12
- `ffmpeg` + `ffprobe` on `PATH`
- An NVIDIA GPU (tested on RTX 4080 16 GB; 8 GB should be fine for `large-v3` Whisper and PANNs Cnn14)
- [`uv`](https://docs.astral.sh/uv/) for dep management

```bash
cd cli
uv sync              # installs everything including torch+cu124
uv pip install -e .  # make the `bken` CLI importable/installable
```

The LLM reranker shells out to the `claude` CLI (Claude Code). If that's
on your `PATH` and you're logged in, `bken rank` just works — no API
key required. If `claude` is missing, the stage is skipped.

---

## CLI

`bken` is a Typer CLI. Top-level help is `bken --help`; every
subcommand has its own `-h`. Output is colourful and uses live progress
bars + spinners — quiet it with `-q`, crank up logs with `-v`.

```text
bken init           Initialize the SQLite schema
bken ingest         Ingest a file or recursively scan a folder (parallel)
bken extract-audio  Decode each pending video to 16 kHz mono WAV
bken transcribe     Run faster-whisper on every audio-extracted video
bken detect-events  Run PANNs + RMS spike detection
bken score          Sliding-window scoring → top-N candidate clips
bken clip           Cut candidate clips into mp4s
bken rank           Re-rank clips with the Claude CLI
bken pipeline       Run every stage end-to-end
bken stats          Pipeline-wide overview
bken config         Dump effective configuration
bken web                     Launch the Nuxt admin panel (shortcut for docker compose up)
bken videos ...     list / show / reset / delete indexed videos
bken clips  ...     show / open / top / rate candidate clips
bken db     ...     init / vacuum / reset
```

### Quick start

```bash
# Whole pipeline, one file
uv run bken pipeline /mnt/shack/media/gaming/archive/Grand_Theft_Auto_V/2020-01-13_03-07-51.mp4 --single-file

# Browse results
uv run bken stats
uv run bken clips show 1
docker compose up                        # http://127.0.0.1:3000
```

### Bulk ingest (the headline feature)

`bken ingest` walks a folder recursively and farms out `ffprobe` +
SHA1-prefix hashing to a thread pool. With the default `--workers 8` you
can chew through tens of thousands of recordings in a single command:

```bash
# 8 ffprobes at once, recursive walk
uv run bken ingest /mnt/shack/media/gaming/archive --workers 8

# Just one file
uv run bken ingest /path/to/one.mp4 --single-file

# Filter the walk
uv run bken ingest /archive -p '2024-*.mp4' --workers 16

# Preview without touching the DB
uv run bken ingest /archive --dry-run --limit 100
```

Re-running `ingest` on the same folder is a no-op for unchanged files
(size + mtime + 16 MB SHA1 prefix); changed files re-probe and reset
their downstream stage flags. The summary table at the end breaks down
**inserted / updated / unchanged / errored**.

### Per-stage commands

Useful while iterating on scoring weights or after a fresh ingest:

```bash
uv run bken extract-audio --workers 4   # parallel ffmpeg decode
uv run bken transcribe                  # GPU; sequential
uv run bken detect-events               # GPU; sequential
uv run bken score
uv run bken clip --workers 4            # parallel ffmpeg cuts
uv run bken rank                        # shells out to `claude -p`
```

GPU-bound stages (`transcribe`, `detect-events`) intentionally stay
single-threaded — Whisper-large-v3 + PANNs Cnn14 saturate one consumer
GPU on their own.

### Pipeline mode for a whole archive

```bash
uv run bken pipeline /mnt/shack/media/gaming/archive --workers 8
```

This runs ingest first (parallel), then audio / transcribe / events /
score / clip / rank for every video that's still pending. Pass
`--skip-rank` to leave the LLM step out, or `--stream-copy` to skip
re-encoding on the cut step.

### Browsing results

```bash
uv run bken videos list                 # dashboard with stage flags
uv run bken videos show 1               # full metadata for video_id=1
uv run bken clips show 1                # ranked clip list for one video
uv run bken clips top -n 20             # top 20 across the archive
uv run bken clips rate 42 5             # 5-star a clip
uv run bken clips open 42               # xdg-open the mp4
docker compose up                          # interactive grid in a browser
```

### Maintenance

```bash
uv run bken videos reset 1 transcribe score   # re-run those stages
uv run bken videos delete 1                   # row + cached audio + cut mp4s
uv run bken db vacuum
uv run bken db reset --yes                    # nuke index.db
```

### Outputs

- `data/index.db`              — SQLite with everything
- `data/audio/<video_id>.wav`   — cached 16 kHz audio
- `data/clips/video_<id>/`      — extracted mp4 clips (~30 s each)
- `data/thumbs/`                — lazy clip thumbnails (web UI)

---

## Web UI

A Nuxt 4 app in `nuxt/`. Vue 3 client using `@nuxt/ui`, Nitro server routes (JSON API
+ media streaming), Vidstack for inline playback, IBM Plex Sans, dark
archive-tape aesthetic. Runs under Bun with the built-in `bun:sqlite`
driver — no native addons to build.

### `docker compose up` — full stack, one command

```bash
cp .env.example .env      # set ARCHIVE_DIR for your gameplay archive
docker compose up         # first-run builds; subsequent runs are seconds
```

- **nuxt** — http://localhost:3000 — Vue client + JSON API + thumb / mp4 routes.
- **cli**  — no port; used via `docker compose exec` to run Python pipeline stages.

Open http://localhost:3000 for the admin panel. Default sort is **Best (LLM
rank)**; the sidebar filters by game, the min-rating pills hide lower-rated
clips, and clicking a thumbnail mounts Vidstack for inline playback.

### Host dev loop (no docker)

```bash
# terminal 1 — Nuxt dev server (UI + API under one process)
cd nuxt && bun install && bun run dev       # → http://localhost:3000

# terminal 2 — pipeline work
uv run bken pipeline /path/to/video.mp4 --single-file
```

Everything a browser hits lives behind `:3000`. The Vite dev HMR, the
server API, thumbnail rendering, and range-served mp4s all share the
same port.

### Pipeline stages in the `cli` container

```bash
docker compose exec cli uv run bken ingest /archive/Grand_Theft_Auto_V --workers 8
docker compose exec cli uv run bken pipeline /archive/file.mp4 --single-file
docker compose exec cli uv run bken rank
```

GPU-bound stages (`transcribe`, `detect-events`) need
`nvidia-container-toolkit` on the host plus the override file:

```bash
docker compose -f docker-compose.yml -f docker-compose.gpu.yml up
```

The `rank` stage shells out to `claude`; it works inside the container
as long as `CLAUDE_HOME` in `.env` points at your host's `~/.claude`.

### Project layout

```
cli/                             Python pipeline
  src/bken/                   Source code
  tests/                         Pytest suite
  pyproject.toml                 uv / dependencies
  Dockerfile                     Backend image

app/                             Nuxt 4 admin panel
  app/                           Frontend code
  server/                        Nitro API routes
  package.json                   Dependencies (Bun)
  Dockerfile                     Frontend image
```

### JSON API

| Route                          | Purpose                                             |
|-------------------------------|-----------------------------------------------------|
| `GET /api/clips`               | Filter + sort candidate clips (JSON)                |
| `GET /api/games`               | Sidebar roll-up: one row per game w/ clip counts    |
| `POST /api/rate/{id}?rating=N` | Set (1–5) or clear (0) a clip's rating              |
| `GET /thumb/{id}.jpg`          | Lazy ffmpeg thumbnail (cached to `data/thumbs/`)    |
| `GET /clip/{id}.mp4`           | Clip file; Range requests supported for scrubbing   |

---

## Configuration

All tunables live in `cli/src/bken/config.py`; the common ones can also be
overridden with env vars:

| Env var                   | Default       | Notes                                                 |
|---------------------------|---------------|-------------------------------------------------------|
| `BKEN_DATA`            | `./data`      | Where DB + artifacts go                               |
| `BKEN_MODELS`          | `./models`    | Whisper model cache                                   |
| `BKEN_WHISPER_MODEL`   | `large-v3`    | Try `turbo` for faster-lower-quality, `small` on CPU  |
| `BKEN_WHISPER_COMPUTE` | `float16`     | Use `int8` to fit on small GPUs                       |
| `BKEN_CLAUDE_CLI`      | `claude`      | Path / name of the Claude Code CLI                    |
| `BKEN_CLAUDE_MODEL`    | `sonnet`      | Forwarded as `--model` to the CLI; set to `haiku` for speed |

Scoring weights, thresholds, window size, and stride are constants at
the top of `scoring.py` / `events.py` — edit in place.

---

## Expected throughput on RTX 4080 16 GB

For a 30-minute 1080p AV1 source:

| Stage          | Wall time  |
|----------------|-----------|
| extract-audio  | ~10 s     |
| transcribe     | ~60 s     |
| detect-events  | ~6 s      |
| score          | <1 s      |
| clip           | ~4 min    |

The encoder stage dominates because Fedora ships ffmpeg without libx264
and `libnvidia-encode` isn't installed on this box — so cuts fall back
to software H.264. Install `libnvidia-encode` or set
`--stream-copy` on `clip` for faster cuts (keyframe-boundary accurate).

---

## Known limitations / planned v2

- **No speaker diarization yet.** Adding `pyannote.audio` + embeddings
  lets you answer "find clips where Alice and Bob are both playing".
- **No game-aware visual features.** Scene-cut detection and HUD OCR for
  the top 2–3 games would catch scripted hype moments (victories,
  killstreaks) that pure-audio can miss.
- **Web UI is single-user.** No auth, no multi-user ratings. Meant to run
  on localhost or behind a trusted reverse proxy.
- **Clip boundary snapping.** Currently we pad by a fixed amount — we
  should snap to silence gaps and `PySceneDetect` scene cuts so clips
  don't start mid-sentence.
- **Scoring weights are hand-tuned.** A thumbs-up/down UI would let
  these learn per-user.

---

## Tests

```bash
uv run pytest
```

Heavy ML stages are covered by running the pipeline end-to-end on a
real video; the unit tests cover scoring/NMS math and DB schema.

---

## Architecture split

| Concern            | Lives in       | Runtime            |
|--------------------|----------------|--------------------|
| Pipeline stages    | `cli/`         | Python 3.12 + uv   |
| Web UI + JSON API  | `app/`         | Bun + Nitro        |
| Shared state       | `data/`        | SQLite WAL + mp4s  |

The two sides agree only on: **SQLite schema**, **path layout**, and
the **ffmpeg thumbnail filter chain**. You can rewrite either side
without touching the other — the DB is the contract.
