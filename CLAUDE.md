# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`bken` is a LAN voice chat application. The server is a pure-Go WebSocket signaling server; the client is a Wails v2 desktop app (Go + Vue 3 frontend) that captures microphone audio, encodes it with Opus, and streams it peer-to-peer over WebRTC.

## Commands

```bash
# Server with hot reload (via Air)
docker compose up

# Run server tests (no CGO needed)
cd server && go test ./...

# Run a single server test
cd server && go test ./internal/core/ -run TestChannelState

# Run client Go tests (requires CGO: libopus-dev, portaudio19-dev)
cd client && go test ./...

# Run frontend tests
cd client/frontend && bun run test

# Run a single frontend test file
cd client/frontend && bunx vitest run src/__tests__/App.test.ts

# Watch mode for frontend tests
cd client/frontend && bun run test:watch

# Build client binary (Linux needs -tags webkit2_41; macOS/Windows don't)
cd client && wails build -tags webkit2_41

# Build frontend only
cd client/frontend && bun run build

# Regenerate Wails bindings after changing Go method signatures
cd client && wails generate module
```

The server accepts `-addr` (default `:8080`), `-db` (default `bken.db`), and `-blobs-dir` flags.

## Architecture

### Wire protocol

Connections use WebSocket on `/ws` (port 8080, plain HTTP):

1. Client sends `{"type":"hello","username":"..."}`.
2. Server responds with `{"type":"snapshot","self_id":"<uuid>","users":[...]}`, then broadcasts `user_joined` to others.
3. Ongoing message types — client→server: `ping`, `connect_server`, `disconnect_server`, `join_voice`, `disconnect_voice`, `send_text`. Server→client: `snapshot`, `user_joined`, `user_left`, `user_state`, `text_message`, `pong`, `error`.

The server handles presence and text chat only. No WebRTC relay — voice audio flows peer-to-peer between clients.

### Server (`server/`)

Organized into `internal/` packages:

- `main.go` — entry point; wires SQLite store, blob store, channel state, and HTTP server. Version injected via `-ldflags`.
- `internal/protocol/` — `Message` struct (JSON envelope), `User`/`VoiceState` types, protocol type constants.
- `internal/core/` — `ChannelState`: thread-safe in-memory user presence registry (`sync.RWMutex` + `atomic`). Sessions, broadcast, per-server scoped text relay.
- `internal/ws/` — `Handler`: gorilla/websocket upgrade, `hello`→`snapshot` handshake, message read loop, dispatches to `ChannelState`.
- `internal/httpapi/` — Echo HTTP server. Routes: `GET /health`, `GET /api/state`, `POST /api/blobs` (alias `/api/upload`), `GET /api/blobs/:id` (alias `/api/files/:id`). Registers the WS handler.
- `internal/blob/` — disk-backed blob store with SQLite metadata.
- `internal/store/` — SQLite store (`modernc.org/sqlite`, pure Go, no CGO). Auto-migrates on open.

No CGO. No TLS (plain HTTP). Alpine Docker build.

### Client (`client/`)

**Go layer:**
- `transport.go` — dials WebSocket, manages per-peer WebRTC connections via `pion/webrtc/v4`, fires `runtime.EventsEmit` callbacks so the frontend sees `user:list`, `user:joined`, `user:left`, chat events, etc. The client supports a richer protocol than the current server (WebRTC signaling, channels, reactions, video).
- `audio.go` — PortAudio capture (48 kHz, mono, 960-sample / 20 ms frames) → Opus encode → WebRTC track; remote tracks → Opus decode → jitter buffer → PortAudio playback.
- `app.go` — `App`: Wails-bound methods (`Connect`, `Disconnect`, `SetMuted`, `SetDeafened`, etc.); bridges transport callbacks to frontend events. Supports multiple simultaneous server connections (`sessions` map).
- `interfaces.go` — `Transporter` interface covering all transport operations.
- `internal/` — sub-packages: `config` (persisted user settings), `jitter`, `noisegate`, `vad`, `aec`, `agc`, `adapt`.

**Frontend** (`client/frontend/src/`): Vue 3, Vite 6, Tailwind CSS v4, DaisyUI v5, TypeScript, Lucide icons. Package manager is `bun`.

Wails runtime bindings are auto-generated under `wailsjs/` — do not edit manually; regenerate with `wails generate module` after changing Go method signatures.

### Frontend testing

Tests use Vitest with jsdom. The setup file (`src/__tests__/setup.ts`) provides:
- Full `window.runtime` mock (Wails runtime event bus + all window APIs)
- Full `window.go.main.App` mock (all Go bridge functions)
- Auto-reset of event bus, config, and mocks via `afterEach`
- Exported helpers: `emitWailsEvent()`, `resetWailsEvents()`, `resetConfig()`, `getGoMock()`, `flushPromises()`

### Docker

`docker-compose.yml` currently runs only the server (dev profile with Air hot reload). Client service is commented out.

Server Dockerfile: single file with `base`, `dev`, `build`, `prod` stages. Dev stage uses Air; prod stage is Alpine with non-root user and healthcheck.

The server image is published to [`ghcr.io/rustyguts/bken`](https://ghcr.io/rustyguts/bken). Pull with:

```bash
docker pull ghcr.io/rustyguts/bken:latest
```

Client Dockerfile: Debian Bookworm base (`golang:1-bookworm`). Installs GTK3, WebKit2GTK 4.1, PortAudio, Opus, PipeWire. Build flag `-tags webkit2_41` required on Linux.

### CI

GitHub Actions on push to main:
- `build.yml` — Server: standalone binary (`CGO_ENABLED=0`) + Docker image build. Client: matrix build for Linux, macOS (brew deps), Windows (MSYS2/MINGW64).
- `docker.yml` — Pushes server Docker image to `ghcr.io/rustyguts/bken` with `latest` + commit SHA tags. On PRs, pushes commit SHA tag only.
