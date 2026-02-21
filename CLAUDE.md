# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`bken` is a LAN voice chat application. The server is a pure-Go WebSocket signaling server; the client is a Wails v2 desktop app (Go + Vue 3 frontend) that captures microphone audio, encodes it with Opus, and streams it peer-to-peer over WebRTC.

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
cd client/frontend && bun run build
```

The server accepts a `-addr` flag (default `:8443`).

## Architecture

### Wire protocol

Every connection uses WebSocket over TLS (`/ws` on port 8443):

1. **Control messages** — reliable, bidirectional, newline-delimited JSON. Client connects and immediately sends `{"type":"join","username":"..."}`. Server responds with `user_list` (includes channel list and ICE server config), then pushes events: `user_joined`, `user_left`, `chat`, `edit_message`, `delete_message`, `create_channel`, `rename_channel`, `delete_channel`, `webrtc_offer`, `webrtc_answer`, `webrtc_ice`, `kick`, `move_user`, `reaction`, `typing`, and more.
2. **Voice audio** — peer-to-peer WebRTC (`pion/webrtc/v4`). The server relays only WebRTC signaling (offer/answer/ICE candidates); Opus audio flows directly between clients over DTLS-SRTP.

### Server (`server/`)

- `server.go` — HTTPS + WebSocket upgrade (`gorilla/websocket`), HTTP routing (`/ws` for signaling, `/` for health check)
- `client.go` — per-connection goroutine: join handshake, control message read loop, WebRTC signaling relay, chat/channel/admin message handling; fires `broadcastUserJoined` / `broadcastUserLeft`
- `room.go` — `Room`: thread-safe client registry, channel state, bounded message store, reaction tracking, role-based access control (OWNER > ADMIN > MODERATOR > USER), rate limiting, circuit breaker for slow clients
- `tls.go` — generates a fresh self-signed ECDSA cert on every start; fingerprint is logged for debugging
- `api.go` — REST API (Echo): health, settings, channels CRUD, file upload/download, recordings list/download, audit log, ban management
- `recording.go` — server-side voice recording to OGG/Opus files (max 2 hours)
- `metrics.go` — periodic connection and message counter logging

No CGO. Alpine Docker build.

### Client (`client/`)

**Go layer:**
- `transport.go` — dials WebSocket (`gorilla/websocket`), handles join handshake, manages per-peer WebRTC connections via `pion/webrtc/v4`, fires `runtime.EventsEmit` callbacks so the frontend sees `user:list`, `user:joined`, `user:left`, chat events, etc.
- `audio.go` — PortAudio capture (48 kHz, mono, 960-sample / 20 ms frames) → Opus VoIP encode → WebRTC track; remote WebRTC tracks → Opus decode → jitter buffer → PortAudio playback
- `app.go` — `App`: Wails-bound methods (`Connect`, `Disconnect`, `GetInputDevices`, `SetMuted`, `SetDeafened`, etc.); wires transport callbacks to frontend events
- `interfaces.go` — `Transporter` interface covering all client operations

**Frontend** (`client/frontend/src/`): Vue 3 (Vite + Tailwind + DaisyUI). Wails runtime bindings are auto-generated under `wailsjs/` — do not edit manually; regenerate with `wails generate module` after changing Go method signatures.

### Docker

Server Dockerfile: multi-stage Alpine (prod) + `Dockerfile.dev` (golang:alpine + Air hot reload, source mounted as volume).

Client Dockerfile: Fedora 43 single-stage. Build flag `-tags webkit2_41` required (Fedora ships `webkit2gtk4.1-devel`). The following env vars are required in `docker-compose.yml` to prevent a black screen on systems without a GPU driver in-container:

```
WEBKIT_DISABLE_COMPOSITING_MODE=1
WEBKIT_DISABLE_DMABUF_RENDERER=1
LIBGL_ALWAYS_SOFTWARE=1
```
