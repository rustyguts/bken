# Architecture

BKEN is a LAN-first voice chat application. The server handles WebSocket signaling and chat; clients communicate directly over WebRTC for audio.

## Overview

```
 Client A                    Server                     Client B
 --------                    ------                     --------
    |--- WebSocket join ------->|                          |
    |<-- user_list, channels ---|                          |
    |                           |<--- WebSocket join ------|
    |<-- user_joined -----------|----> user_joined ------->|
    |                           |                          |
    |--- webrtc_offer --------->|----> webrtc_offer ------>|
    |<-- webrtc_answer ---------|<---- webrtc_answer ------|
    |--- webrtc_ice ----------->|----> webrtc_ice -------->|
    |<-- webrtc_ice ------------|<---- webrtc_ice ---------|
    |                           |                          |
    |<========= Opus audio (peer-to-peer WebRTC) ========>|
```

## Server (`server/`)

The server is a single Go binary with no external dependencies beyond embedded SQLite. It serves two roles:

1. **WebSocket signaling** (port 8080): upgrades HTTP connections to WebSocket, manages the join handshake, relays control messages (chat, WebRTC signaling, channel management), and maintains the room state.

2. **REST API** (port 8080): provides HTTP endpoints for health checks, server settings, channel CRUD, file uploads/downloads, and invite pages.

### Key Files

| File | Purpose |
|------|---------|
| `main.go` | Entry point, CLI flags, wiring |
| `server.go` | HTTPS + WebSocket upgrade (`gorilla/websocket`), HTTP routing |
| `client.go` | Per-connection goroutine: join handshake, control message loop, WebRTC signaling relay, chat/admin handling |
| `room.go` | Thread-safe client registry, channel state, message store, reaction tracking, role-based access control, rate limiting |
| `protocol.go` | Wire protocol types (`ControlMsg`, `UserInfo`, `ChannelInfo`, `ICEServerInfo`, `ReactionInfo`) |
| `api.go` | REST API (Echo framework): health, settings, channels, file upload/download, recordings, audit log, bans |
| `recording.go` | Server-side voice recording to OGG/Opus (max 2 hours, auto-stopped) |
| `tls.go` | Self-signed ECDSA certificate generation |
| `linkpreview.go` | OpenGraph metadata extraction for link previews in chat |
| `metrics.go` | Periodic connection and message counter logging |
| `store/store.go` | SQLite persistence: migrations, settings, channels, files, bans, audit log |

### Wire Protocol

Every connection uses a single WebSocket session:

- **Control messages**: reliable, bidirectional, newline-delimited JSON. Message types include `join`, `user_list`, `user_joined`, `user_left`, `chat`, `edit_message`, `delete_message`, `reaction`, `typing`, `create_channel`, `rename_channel`, `delete_channel`, `join_channel`, `move_user`, `webrtc_offer`, `webrtc_answer`, `webrtc_ice`, `kick`, `rename`, `recording_start`, `recording_stop`, `ping`, `pong`, and more.
- **Voice**: peer-to-peer WebRTC (Opus audio, DTLS-SRTP). The server only relays signaling; audio flows directly between clients.

### Persistence

SQLite via `modernc.org/sqlite` (pure Go, no CGO). Stores:
- Server settings (key-value)
- Channels (id, name, position)
- File metadata (name, content type, disk path, size)

Chat messages are ephemeral (in-memory only).

## Client (`client/`)

The client is a Wails v2 desktop application with a Go backend and Vue 3 frontend.

### Go Layer

| File | Purpose |
|------|---------|
| `app.go` | Wails-bound methods: `Connect`, `Disconnect`, `GetInputDevices`, `SetMuted`, `SetDeafened`, `SetPTTMode`, etc. |
| `transport.go` | WebSocket connection, control message handling, WebRTC peer connections via `pion/webrtc/v4`, metrics collection |
| `audio.go` | PortAudio capture (48 kHz mono, 960-sample frames), Opus encode/decode (32 kbps adaptive), playback |
| `noise.go` | Spectral gating noise suppression |
| `internal/vad/` | Voice activity detection (energy-based with hangover) |
| `internal/aec/` | Acoustic echo cancellation |
| `internal/agc/` | Automatic gain control |
| `internal/adapt/` | Adaptive bitrate and jitter buffer depth |
| `internal/jitter/` | Jitter buffer for audio playback |
| `internal/config/` | JSON config file persistence |

### Frontend (`client/frontend/src/`)

Vue 3 with Composition API, Vite, TailwindCSS, and DaisyUI.

| Component | Purpose |
|-----------|---------|
| `App.vue` | Root: connection state, event wiring, PTT listeners, auto-join logic |
| `Sidebar.vue` | Server list with avatars, add server dialog, `bken://` deep links |
| `Room.vue` | Main layout: title bar + channels + chat |
| `TitleBar.vue` | Server name (inline rename for owner), invite link copy |
| `ServerChannels.vue` | Channel list (DaisyUI menu), user list per channel, context menus, drag-to-reorder |
| `ChannelChatroom.vue` | Chat messages, reactions, pins, file uploads, link previews, `@mention` autocomplete |
| `UserControls.vue` | Mute, deafen, video, screen share, settings, leave voice |
| `UserCard.vue` | Per-user avatar with mute toggle and kick button |
| `UserProfilePopup.vue` | User profile card with online/speaking status |
| `VideoGrid.vue` | Video tile layout for camera/screen share streams |
| `MetricsBar.vue` | Connection quality indicator (RTT, loss, jitter) |
| `SettingsPage.vue` | Tabbed settings shell |
| `AboutSettings.vue` | Version info and links |
| `AppearanceSettings.vue` | Theme picker and message density controls |
| `KeybindsSettings.vue` | PTT key binding configuration |
| `KeyboardShortcuts.vue` | Keyboard shortcut reference overlay |
| `ReconnectBanner.vue` | Disconnect warning with auto-reconnect countdown |

### Audio Pipeline

```
Microphone
    |
PortAudio capture (48kHz, mono, 960 samples / 20ms)
    |
Noise gate (zero out audio below threshold)
    |
VAD gate (energy threshold + hangover)
    |
AEC (delay estimation + subtraction)
    |
Noise suppression (spectral gating)
    |
AGC (target level normalization)
    |
Volume control
    |
Opus encode (8-48 kbps adaptive)
    |
pion WebRTC track (DTLS-SRTP)
    |
  [peer-to-peer]
    |
Remote pion WebRTC track
    |
Opus decode
    |
Jitter buffer (adaptive depth)
    |
PortAudio playback
```

## Docker

- **Server Dockerfile**: multi-stage Alpine build (production) + `Dockerfile.dev` (golang:alpine + Air hot reload)
- **Client Dockerfile**: Fedora 43 single-stage, requires `-tags webkit2_41` build flag
- **docker-compose.yml**: development configuration with volume mounts and port mapping
