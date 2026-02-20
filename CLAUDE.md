# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`bken` is a LAN voice chat application. The server is a pure-Go WebTransport (QUIC/HTTP3) relay; the client is a Wails v2 desktop app (Go + Vue/Svelte frontend) that captures microphone audio, encodes it with Opus, and streams it over WebTransport.

## Commands

All commands assume Docker Compose unless noted.

```bash
# Full stack (server hot-reloads via Air; clients are pre-built images)
docker compose up

# Server only (hot reload)
docker compose up server

# Run server tests
cd server && go test ./...

# Run client tests (requires CGO toolchain: libopus-devel, portaudio-devel)
cd client && go test ./...

# Build client binary on host
cd client && wails build -tags webkit2_41

# Frontend only
cd client/frontend && npm run build
```

The server accepts a `-addr` flag (default `:4433`).

## Architecture

### Wire protocol

Every connection uses a single WebTransport session over QUIC:

1. **Control stream** — reliable, bidirectional, newline-delimited JSON. Client opens it and immediately sends `{"type":"join","username":"..."}`. Server responds with `user_list`, then pushes `user_joined` / `user_left` as peers connect.
2. **Voice datagrams** — unreliable UDP-like. Each datagram is `[senderID: uint16 BE][seq: uint16 BE][opus_payload]`. The server overwrites `senderID` before fan-out to prevent spoofing, then broadcasts to all other sessions.

### Server (`server/`)

- `server.go` — WebTransport upgrade, single HTTP route `/`
- `client.go` — per-session goroutine: accepts control stream → join handshake → `readDatagrams` goroutine → control read loop until disconnect; fires `broadcastUserJoined` / `broadcastUserLeft`
- `room.go` — `Room`: thread-safe client registry; `Broadcast` fans datagrams out under `RLock`
- `tls.go` — generates a fresh self-signed ECDSA cert on every start; fingerprint is logged for debugging. Client uses `InsecureSkipVerify: true`.
- `metrics.go` — logs datagrams/bytes every 5 s (resets counters each tick); silent when room is empty

No CGO. Alpine Docker build.

### Client (`client/`)

**Go layer:**
- `transport.go` — `Transport`: dials WebTransport, opens control stream, sends join, reads control messages → fires `OnUserList` / `OnUserJoined` / `OnUserLeft` callbacks; `SendAudio` builds datagrams; `StartReceiving` pumps incoming datagrams to a playback channel
- `audio.go` — `AudioEngine`: PortAudio capture (48 kHz, mono, 960-sample / 20 ms frames) → Opus VoIP encode (32 kbps) → `CaptureOut` chan; `PlaybackIn` chan → Opus decode → PortAudio playback; `testMode` routes capture directly to playback for loopback testing
- `app.go` — `App`: Wails-bound methods (`Connect`, `Disconnect`, `GetInputDevices`, etc.); wires `Transport` callbacks to `runtime.EventsEmit` so the frontend sees `user:list`, `user:joined`, `user:left`

**Frontend** (`client/frontend/src/`): Vue (Vite + Tailwind + DaisyUI). Wails runtime bindings are auto-generated under `wailsjs/` — do not edit manually; regenerate with `wails generate module` after changing Go method signatures.

### Docker

Server Dockerfile: multi-stage Alpine (prod) + `Dockerfile.dev` (golang:alpine + Air hot reload, source mounted as volume).

Client Dockerfile: Fedora 43 single-stage. Build flag `-tags webkit2_41` required (Fedora ships `webkit2gtk4.1-devel`). The following env vars are required in `docker-compose.yml` to prevent a black screen on systems without a GPU driver in-container:

```
WEBKIT_DISABLE_COMPOSITING_MODE=1
WEBKIT_DISABLE_DMABUF_RENDERER=1
LIBGL_ALWAYS_SOFTWARE=1
```
