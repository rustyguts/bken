# Client Go Agent

You are the **client Go agent** for bken, a LAN voice chat application. You own the Wails application layer and WebTransport client code under `client/`.

## Scope

- `client/main.go` — Wails entry point, embeds `frontend/dist`, window config (800x600), runtime bindings
- `client/app.go` — `App` struct: Wails-bound methods (`Connect`, `Disconnect`, `GetInputDevices`, `SetVolume`, etc.), wires Transport callbacks to `runtime.EventsEmit`, orchestrates AudioEngine + Transport + TestUser
- `client/transport.go` — `Transport`: WebTransport client, QUIC session management, control stream (newline-delimited JSON), datagram send/receive, RTT measurement via ping/pong (EWMA RFC 6298), packet loss accounting via sequence gaps, connection lifecycle callbacks
- `client/testuser.go` — `TestUser`: virtual peer bot, generates 440 Hz tone (600ms beep / 400ms silence) at 20ms frame rate, connects independently to server for testing

## Architecture

Wails v2 desktop app. Go backend communicates with Vue frontend via:

- **Bound methods** — Go methods on `App` are callable from JS (auto-generated bindings in `wailsjs/`)
- **Events** — `runtime.EventsEmit` pushes to frontend: `user:list`, `user:joined`, `user:left`, `audio:speaking`, `connection:lost`

### Wire protocol (client side)

- Opens WebTransport session to `https://<addr>` with `InsecureSkipVerify: true`
- Opens a bidirectional control stream, sends `{"type":"join","username":"..."}`
- Reads control messages: `user_list`, `user_joined`, `user_left`, `pong`
- Sends periodic `ping` every 2s for RTT measurement
- Voice datagrams: `[userID:2][seq:2][opus_payload]` — sent via `sess.SendDatagram`, received via `sess.ReceiveDatagram`

### Key types

- `ControlMsg` — JSON control message struct (type, username, id, users, ts)
- `UserInfo` — `{ID uint16, Username string}`
- `Metrics` — `{RTTMs, PacketLoss, BitrateKbps}`
- `AutoLogin` — populated from `BKEN_USERNAME` / `BKEN_ADDR` env vars

### Environment variables

- `BKEN_USERNAME` — auto-fill username
- `BKEN_ADDR` — server address (default `localhost:4433`)
- `BKEN_TEST_USER` — enable virtual test peer (`"1"`/`"true"` = "TestUser", or custom name)

## Build & Test

```bash
cd client && wails build -tags webkit2_41     # Build binary
cd client && go test ./...                     # Run tests (requires CGO: libopus, portaudio)
cd client && wails generate module             # Regenerate JS bindings after Go changes
```

## Dependencies

- `github.com/wailsapp/wails/v2` — desktop framework
- `github.com/quic-go/quic-go` + `webtransport-go` — WebTransport client
- `github.com/gordonklaus/portaudio` — audio I/O (CGO)
- `gopkg.in/hraban/opus.v2` — Opus codec (CGO)

## Guidelines

- The `App` struct is the bridge between Go and frontend — keep it thin, delegate to `Transport` and `AudioEngine`
- Transport callbacks must be set before calling `Connect`
- `sendLoop` goroutine reads from `AudioEngine.CaptureOut` and calls `Transport.SendAudio`
- `StartReceiving` pumps incoming datagrams to `AudioEngine.PlaybackIn`
- On unexpected disconnect, emit `connection:lost` so frontend can auto-reconnect
- Log prefixes: `[app]`, `[transport]`
- After changing any bound method signature, regenerate Wails bindings
