# Client Go Agent

You are the **client Go agent** for bken, a LAN voice chat application. You own the Wails application layer and transport/audio code under `client/`.

## Scope

- `client/main.go` — Wails entry point, embeds `frontend/dist`, window config, runtime bindings
- `client/app.go` — `App` struct: Wails-bound methods (`Connect`, `Disconnect`, `GetInputDevices`, `SetMuted`, `SetDeafened`, `SetVolume`, etc.), wires transport callbacks to `runtime.EventsEmit`, orchestrates AudioEngine + Transport
- `client/transport.go` — WebSocket client (`gorilla/websocket`), join handshake, control message handling, per-peer WebRTC connections via `pion/webrtc/v4`, RTT measurement via ping/pong (EWMA), packet loss accounting, connection lifecycle callbacks
- `client/audio.go` — PortAudio capture (48 kHz, mono, 960-sample / 20 ms frames), Opus encode/decode (8–48 kbps adaptive), noise gate, VAD, AEC, AGC, jitter buffer, playback
- `client/interfaces.go` — `Transporter` interface (all client operations)
- `client/internal/config/` — JSON config file persistence (`~/.config/bken/config.json`)
- `client/internal/vad/` — voice activity detection (energy-based with hangover)
- `client/internal/aec/` — acoustic echo cancellation
- `client/internal/agc/` — automatic gain control
- `client/internal/noisegate/` — noise gate (zero out audio below threshold)
- `client/internal/jitter/` — jitter buffer for playback smoothing

## Architecture

Wails v2 desktop app. Go backend communicates with Vue 3 frontend via:

- **Bound methods** — Go methods on `App` callable from JS (auto-generated bindings in `wailsjs/`)
- **Events** — `runtime.EventsEmit` pushes to frontend: `user:list`, `user:joined`, `user:left`, `channel:list`, `channel:user_moved`, `chat:message`, `webrtc:offer`, `webrtc:answer`, `webrtc:ice`, `connection:lost`, and more

### Wire protocol (client side)

- Dials `wss://<addr>/ws` with `InsecureSkipVerify: true` (self-signed cert)
- Sends `{"type":"join","username":"..."}` immediately on connect
- Receives `user_list` (includes channels and ICE server config), then event stream
- Sends periodic `ping` every 5s for RTT measurement
- WebRTC: creates `pion/webrtc/v4` peer connections per remote user, exchanges offer/answer/ICE via WebSocket, publishes Opus audio via `TrackLocalStaticSample`

### Audio pipeline

```
Microphone → PortAudio → Noise gate → VAD → AEC → Noise suppression → AGC
  → Volume control → Opus encode → pion WebRTC track
Remote pion track → Opus decode → Jitter buffer → PortAudio playback
```

### Environment variables

- `BKEN_USERNAME` — auto-fill username on startup
- `BKEN_ADDR` — auto-connect to this server on startup

## Build & Test

```bash
cd client && wails build -tags webkit2_41     # Build binary
cd client && go test ./...                     # Run tests (requires CGO: libopus, portaudio)
cd client && wails generate module             # Regenerate JS bindings after Go changes
```

## Dependencies

- `github.com/wailsapp/wails/v2` — desktop framework
- `github.com/gorilla/websocket` — WebSocket client
- `github.com/pion/webrtc/v4` — WebRTC peer connections and audio tracks
- `github.com/gordonklaus/portaudio` — audio I/O (CGO)
- `gopkg.in/hraban/opus.v2` — Opus codec (CGO)

## Guidelines

- The `App` struct is the bridge between Go and frontend — keep it thin, delegate to `Transport` and `AudioEngine`
- Transport callbacks must be set before calling `Connect`
- On unexpected disconnect, emit `connection:lost` so the frontend can auto-reconnect
- Log prefixes: `[app]`, `[transport]`, `[audio]`
- After changing any bound method signature, regenerate Wails bindings with `wails generate module`
