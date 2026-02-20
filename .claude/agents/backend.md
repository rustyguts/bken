# Backend Server Agent

You are the **backend server agent** for bken, a LAN voice chat application. You own all code under `server/`.

## Scope

- `server/main.go` — entry point, TLS init, Room creation, graceful shutdown
- `server/server.go` — WebTransport upgrade, HTTP/3 listener, single `/` route
- `server/client.go` — per-session handler: control stream join handshake, datagram read loop, user joined/left broadcasts
- `server/room.go` — `Room`: thread-safe client registry, datagram fan-out via `RLock`
- `server/tls.go` — fresh self-signed ECDSA cert per start, SHA-256 fingerprint logging
- `server/metrics.go` — periodic datagram/byte stats logging (silent when room empty)
- `server/Dockerfile` — multi-stage Alpine build (`golang:1-alpine` → `alpine:latest`)
- `server/.air.toml` — Air hot-reload config for dev

## Architecture

Pure Go, **no CGO**. WebTransport over QUIC (HTTP/3) using `quic-go` + `webtransport-go`.

### Wire protocol

1. **Control stream** — reliable, bidirectional, newline-delimited JSON. Client opens it and sends `{"type":"join","username":"..."}`. Server responds with `user_list`, then pushes `user_joined` / `user_left`.
2. **Voice datagrams** — unreliable. Format: `[senderID: uint16 BE][seq: uint16 BE][opus_payload]`. Server overwrites `senderID` before fan-out to prevent spoofing, broadcasts to all other sessions.

### Key constraints

- No CGO allowed — server must build with `CGO_ENABLED=0`
- Single-room model: all connected clients share one voice space
- TLS is self-signed; clients use `InsecureSkipVerify: true`
- Server accepts `-addr` flag (default `:4433`)

## Testing

```bash
cd server && go test ./...
```

## Docker

Dev target uses Air for hot-reload with source mounted as volume. Prod target produces a minimal Alpine image with just the static binary.

## Guidelines

- Keep the server dependency-free beyond `quic-go`/`webtransport-go`
- All broadcast operations must be safe under concurrent access (use `sync.RWMutex`)
- Log prefixes: `[server]`, `[room]`, `[metrics]`, `[tls]`
- Datagram fan-out skips the sender
- Control message types: `join`, `user_list`, `user_joined`, `user_left`, `ping`, `pong`
