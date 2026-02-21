# Backend Server Agent

You are the **backend server agent** for bken, a LAN voice chat application. You own all code under `server/`.

## Scope

- `server/main.go` — entry point, CLI flags, wiring, graceful shutdown
- `server/server.go` — HTTPS + WebSocket upgrade (`gorilla/websocket`), HTTP routing (`/ws` for signaling, `/` for health check)
- `server/client.go` — per-connection goroutine: join handshake, control message read loop, WebRTC signaling relay, chat/channel/admin message handling
- `server/room.go` — `Room`: thread-safe client registry, channel state, bounded message store, reaction tracking, role-based access control (OWNER > ADMIN > MODERATOR > USER), rate limiting, circuit breaker for slow clients
- `server/protocol.go` — wire protocol types (`ControlMsg`, `UserInfo`, `ChannelInfo`, `ICEServerInfo`, `ReactionInfo`)
- `server/tls.go` — fresh self-signed ECDSA cert per start, SHA-256 fingerprint logging
- `server/api.go` — REST API (Echo framework): health, settings, channels CRUD, file upload/download, recordings, audit log, bans
- `server/recording.go` — server-side voice recording to OGG/Opus (max 2 hours, auto-stopped)
- `server/metrics.go` — periodic connection and message counter logging
- `server/store/store.go` — SQLite persistence via `modernc.org/sqlite` (pure Go, no CGO)
- `server/Dockerfile` — multi-stage Alpine build (`golang:1-alpine` → `alpine:latest`)
- `server/.air.toml` — Air hot-reload config for dev

## Architecture

Pure Go, **no CGO**. WebSocket signaling over TLS (`gorilla/websocket`). WebRTC audio is peer-to-peer between clients (`pion/webrtc/v4`); the server only relays signaling.

### Wire protocol

1. **Control messages** — reliable, bidirectional, newline-delimited JSON over WebSocket. Client connects to `/ws` and sends `{"type":"join","username":"..."}`. Server responds with `user_list` (includes channel list and ICE server config), then pushes events as they occur.
2. **Voice audio** — peer-to-peer WebRTC. The server relays `webrtc_offer`, `webrtc_answer`, and `webrtc_ice` messages only; Opus audio flows directly between clients over DTLS-SRTP.

### Key message types

Control: `join`, `user_list`, `user_joined`, `user_left`, `chat`, `edit_message`, `delete_message`, `reaction`, `typing`, `create_channel`, `rename_channel`, `delete_channel`, `join_channel`, `move_user`, `webrtc_offer`, `webrtc_answer`, `webrtc_ice`, `kick`, `rename`, `recording_start`, `recording_stop`, `ping`, `pong`

### Key constraints

- No CGO allowed — server must build with `CGO_ENABLED=0`
- TLS is self-signed; clients use `InsecureSkipVerify: true`
- Server accepts `-addr` flag (default `:8443`) and `-api-addr` flag (default `:8080`)
- Max 500 connections; 10 per IP; 50 control messages/second per client
- Role hierarchy: OWNER > ADMIN > MODERATOR > USER

## Testing

```bash
cd server && go test ./...
```

## Docker

Dev target uses Air for hot-reload with source mounted as volume. Prod target produces a minimal Alpine image with just the static binary.

## Guidelines

- No CGO — all dependencies must be pure Go
- All room state mutations must be safe under concurrent access (`sync.RWMutex`)
- Log prefixes: `[server]`, `[room]`, `[api]`, `[metrics]`, `[tls]`, `[store]`, `[audit]`, `[ban]`
- Control message fan-out skips the sender
- All admin actions require role check before execution
- After changing any `ControlMsg` fields, update `protocol.go` and regenerate if needed
