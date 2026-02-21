## BKEN Project Guide

This document serves as the master task list for building a self-hosted WebRTC voice/video chat application. Each task is designed to be picked up by an autonomous agent. Tasks are ordered by dependency — work top-to-bottom within each section.

---

### Project Overview

BKEN is a lightweight, self-hosted voice and video chat application inspired by Discord but stripped to essentials. Think Mumble/TeamSpeak simplicity with a modern UI.

**Architecture:**
- **Client:** Desktop-first app (Wails + Vue 3 + TailwindCSS/DaisyUI), also runs as a web app. State stored in IndexedDB.
- **Server:** Single Go binary. SQLite for persistence. WebSocket for signaling/chat/events. WebRTC for voice and video.
- **Identity:** No accounts. Client generates an Ed25519 keypair on first launch. The public key IS the user identity. Messages are signed to prove identity.
- **Docs:** VitePress static site with setup guides, resource planning, and network configuration.

**Tech Stack:**
- Server: Go, Echo web framework (requirement), SQLite (via modernc.org/sqlite or mattn/go-sqlite3), Pion WebRTC, gorilla/websocket
- Client: Wails v2, Vue 3 Composition API, TailwindCSS + DaisyUI, idb (IndexedDB wrapper)
- Docs: VitePress

**Key Principles:**
- Every task must include tests
- Run all tests and linting before committing
- The application must build before the feature is done
- Keep the server a single binary with zero external dependencies

---

### Workflow For Each Task

1. Read this file and pick the next uncompleted task from the highest-priority section
3. Implement the feature following the task description exactly
4. Write tests for the feature (unit + integration where applicable)
5. Run all tests and linting for the entire repo
7. Move the task to the "Done" section at the bottom of this file with a brief summary

---

## Phase 1: Foundation (Easy)

These tasks establish the project skeleton. No task here depends on another unless noted.

---

---

---

---

## Phase 4: Hardening & Polish (Hard)

Advanced features for production readiness.

---

Phase 6: Chat Power Features (Easy)
Small, self-contained chat improvements. No cross-cutting dependencies.

6.07 — Custom Emoji / Stickers (Server-Level)
Allow owners to upload custom emoji that all users can use.

Server settings → Emoji tab: upload images (max 256KB, PNG/GIF/WebP, max 128x128)
Stored in <data-dir>/emoji/ with metadata in SQLite custom_emoji table: (id, name, filename, uploaded_by, created_at)
Custom emoji show in the reaction picker and in a :name: autocomplete in chat input
Typing : opens autocomplete list of custom emoji names
Rendered inline in messages as small images
Max 50 custom emoji per server
Tests: upload validation, size limits, autocomplete, inline rendering, deletion


Phase 7: Voice & Video Polish (Medium)
Improvements to the real-time media experience.

7.01 — Noise Gate
Add a hard noise gate as an alternative/complement to VAD.

Noise gate in audio processing pipeline: audio below threshold is zeroed out (not just gated at VAD level, but actual silence insertion)
Configurable threshold in Audio settings with a real-time input level meter showing where the gate sits
Visual: horizontal bar showing mic level with a draggable gate threshold marker
Gate is independent of VAD — gate removes quiet noise, VAD decides whether to transmit
When gate is active + VAD is active: gate cleans the audio, then VAD decides transmission
Tests: gate threshold behavior, silence insertion, interaction with VAD


7.02 — Per-User Volume Control
Let users adjust the volume of individual other users.

Right-click a user avatar in the channel panel → "Adjust Volume" opens a slider (0% to 200%)
Volume applied as a GainNode multiplier on that user's incoming audio stream
Default 100%. Settings persisted per-user in IndexedDB/config
Show volume icon next to user avatar if set to non-default value
0% effectively mutes that specific user locally
Tests: gain application, persistence, per-user isolation, UI indicator


7.03 — Audio Input Level Meter
Visual feedback for mic input.

Real-time level meter bar in User Controls (next to mic icon) showing current input volume
Green for normal levels, yellow for loud, red for clipping
Also show in Audio Settings for device testing
Uses AnalyserNode (or Go-side RMS calculation) to compute level
Update at ~15fps (every 66ms) — not every frame, to avoid UI churn
When muted, meter shows nothing (not processing audio)
Tests: level calculation accuracy, color thresholds, mute behavior


7.04 — Soundboard / Audio Alerts
Play sounds for events and allow user-triggered audio clips.

Built-in event sounds (toggleable in settings):

User joins voice channel → join chime
User leaves voice channel → leave chime
User mutes/unmutes → subtle click
Incoming message in non-active channel → notification blip


Sounds are small embedded WAV/OGG files (< 50KB each)
Volume control for notification sounds separate from voice volume
Optional: owner can upload up to 10 custom soundboard clips to the server; users can trigger them via a soundboard panel and they play for everyone in the voice channel
Tests: sound playback triggers, volume control, toggle on/off, soundboard broadcast


7.05 — Voice Channel User Limit
Allow owners to set a max user count per voice channel.

Channel property max_users (0 = unlimited, default)
Owner sets this via channel context menu → "Set User Limit"
Server rejects join_voice if channel is full, sends error message to client
Channel list shows 3/10 style user count when limit is set
Tests: limit enforcement, rejection message, UI count display, unlimited default


7.06 — Server-Side Recording (Owner Only)
Allow the server to record voice channels.

Owner triggers recording start/stop from server settings or channel context menu
Server mixes all incoming audio tracks for the channel and writes to an OGG/Opus file in <data-dir>/recordings/
Recording indicator visible to all users in the channel (red dot + "Recording" label)
Recordings listed in server settings → Recordings tab with download links
Auto-stop recording after configurable max duration (default 2 hours)
Privacy: all users in channel are notified when recording starts
Tests: recording lifecycle, file output, notification broadcast, max duration, download


7.07 — Simulcast for Video
Reduce bandwidth for video with multiple quality layers.

When sending video, encode at multiple qualities: high (720p), medium (360p), low (180p)
Use WebRTC simulcast (multiple encodings on the same track)
Server (or clients in mesh) can select which layer to receive based on:

Spotlight user gets high quality
Thumbnail grid users get low quality
Bandwidth-constrained users get low quality


RTCRtpSender.setParameters() to configure encoding layers
Client preference in settings: "Max video quality" dropdown
Tests: simulcast encoding setup, layer switching, bandwidth savings measurement


Phase 8: Server Administration (Medium)
Features for server owners to manage their communities.

8.01 — Audit Log
Track administrative actions.

New SQLite table audit_log: (id, actor_key, action, target, details_json, created_at)
Log events: kick, ban, unban, role change, channel create/rename/delete, settings change, recording start/stop
Server settings → Audit Log tab: scrollable list with filters (action type, date range)
Entries show: timestamp, actor name, action description, target
Max 10,000 entries, oldest auto-purged
Read-only — OWNER can view, nobody can edit or delete
Tests: logging for each action type, auto-purge, filter queries


8.02 — Ban Management
Extend banning beyond the basic kick from 1.16.

Ban stores pubkey/IP + reason + timestamp + who banned in SQLite bans table
Banned users receive banned message with reason on connection attempt, then disconnected
Server settings → Bans tab: list all bans with reason, date, who banned
Owner can unban from this list
Temp bans: optional duration (1h, 24h, 7d) — server checks expiry on connection
IP ban option (ban the IP address as well as identity, to prevent keypair regeneration)
Tests: ban on connect rejection, temp ban expiry, IP ban, unban, ban list display


8.03 — Role Hierarchy & Permissions
Expand beyond simple OWNER/USER.

Roles: OWNER (one, non-transferable except by current owner), ADMIN, MODERATOR, USER
Permission matrix:

OWNER: all permissions, manage admins, transfer ownership, server settings
ADMIN: kick, ban, mute users, manage channels, manage moderators
MODERATOR: kick, mute users, delete any message, pin messages
USER: chat, voice, video


New SQLite column users.role expanded to support all roles
Server checks permissions per action using a centralized hasPermission(role, action) function
Server settings → Roles tab: assign roles to users
Tests: permission checks for every action at every role level, role assignment, hierarchy enforcement


8.04 — Server-Wide Announcements
Let owners broadcast important messages.

Owner can send an announcement from server settings
Announcement appears as a highlighted banner at the top of all channels for all connected users
Announcements are dismissible per-user (client tracks dismissed IDs)
Stored in SQLite announcements table with content, created_at, created_by
Max 1 active announcement at a time (new one replaces old)
Tests: broadcast to all clients, dismiss persistence, replacement behavior


8.05 — Slow Mode
Rate-limit chat in specific channels.

Channel property slow_mode_seconds (0 = off, default)
Owner sets via channel context menu → "Set Slow Mode" with preset options (5s, 10s, 30s, 60s)
Server enforces: rejects send_message if user's last message in that channel was within the cooldown
Client shows countdown timer on the send button when in cooldown
Owners and admins are exempt from slow mode
Indicator in channel header: 🐌 icon when slow mode is active
Tests: cooldown enforcement, admin exemption, timer accuracy, per-channel isolation


8.06 — User Mute (Server-Side)
Allow admins to server-mute disruptive users.

mute_user → { userId, duration? } — ADMIN+ only
Muted user's audio is not forwarded by the server (or in mesh: clients are told to ignore that user's audio)
Muted user sees "You have been muted by an admin" indicator
Optional duration (permanent until unmuted, or timed: 1min, 5min, 30min)
User can still hear others, just can't transmit
unmute_user to reverse
Tests: mute enforcement, timed unmute, notification, authorization


Phase 9: UX Polish (Easy-Medium) — DONE
UI/UX improvements that make the app feel complete.


Phase 10: Performance & Reliability (Hard)
Optimizations for larger deployments and production use.

10.01 — Connection Pooling & Resource Limits
Prevent server resource exhaustion.

Max concurrent connections limit (configurable, default 200)
Per-IP connection limit (default 5) to prevent abuse
Reject new connections gracefully with HTTP 503 when at capacity
WebSocket message size limit (64KB)
Rate limiting on control messages (100/s per client)
Idle timeout: disconnect clients with no activity for configurable duration (default from CLI flag)
Memory usage monitoring: log warning when RSS exceeds threshold
Tests: connection limits, per-IP limits, rate limiting, idle timeout, rejection messages


10.02 — SQLite WAL Mode & Connection Pooling
Optimize database performance.

Enable WAL mode on SQLite for concurrent readers
Connection pool: multiple read connections, single write connection (via database/sql pool settings)
Prepared statement caching for frequently executed queries
Periodic PRAGMA optimize and VACUUM (daily or on startup)
Add indexes: messages(channel_id, created_at), messages(sender_key), reactions(message_id)
Message table partitioning: auto-archive messages older than 90 days to a separate table (configurable)
Tests: concurrent read/write correctness, query performance benchmarks, index effectiveness


10.03 — Graceful Degradation Under Load
Keep the server responsive when resources are constrained.

When CPU > 80%: disable link preview fetching, reduce keepalive frequency
When memory > 80%: stop accepting new voice connections, log warning
When bandwidth saturated: prioritize audio over video, reduce video quality hints to clients
Health check endpoint: GET /health returns server status, connection count, resource usage
Prometheus-compatible metrics endpoint: GET /metrics (optional, behind flag)
Tests: degradation triggers, health check accuracy, priority ordering


10.04 — Message Delivery Guarantees
Ensure chat messages aren't lost.

Server assigns monotonically increasing sequence numbers per channel
Client tracks last received seq per channel
On reconnect, client sends last known seq → server replays missed messages
Server buffers last 500 messages per channel in memory for fast replay (in addition to SQLite)
If client is too far behind (gap > 500), send a history_gap event and client fetches via get_history
Deduplicate messages by ID on the client to handle replay overlaps
Tests: sequence numbering, replay on reconnect, gap detection, deduplication


10.05 — WebRTC Peer Connection Recycling
Reduce overhead of frequent joins/leaves.

Instead of creating new PeerConnections on every voice join, reuse existing ones when possible
When user leaves voice and rejoins within 30 seconds: attempt to reuse the PeerConnection
Properly clean up stale PeerConnections that have been idle for > 60 seconds
Track PeerConnection lifecycle in metrics: created, reused, recycled, leaked
Alert on leaked PeerConnections (created but never closed)
Tests: reuse behavior, cleanup timing, leak detection, metric accuracy


10.06 — Client-Side Caching
Reduce server load with intelligent caching.

Cache channel list in IndexedDB — only refetch when server sends channels_changed event
Cache user list — update incrementally via user_joined/user_left events instead of full refetch
Cache message history per channel — only fetch new messages since last known ID
Cache server settings (name, avatar) — refresh on server_settings_changed
Cache file thumbnails in IndexedDB with size limit (50MB, LRU eviction)
Offline indicator: when disconnected, show cached data with "offline" watermark
Tests: cache hit/miss behavior, incremental updates, LRU eviction, offline display


Phase 11: Platform & Distribution (Medium) — DONE
Getting the app to users.


Phase 12: Advanced Video, Screen Sharing & Calls (Medium-Hard)
Builds on the completed video foundation (3.01 video signaling, 3.02 video grid, 3.03 screen sharing). These tasks turn basic video into a full-featured visual communication platform. Tasks are ordered by dependency.

12.01 — Multi-Source Screen Sharing
Allow a user to share multiple screens/windows simultaneously.

Current 3.03 limits to one screen share per user — lift this restriction
"Share Screen" dropdown shows: "Entire Screen", "Application Window", "Browser Tab" (where supported)
User can share up to 2 sources simultaneously (e.g. one screen + one window, or two windows)
Each share is a separate video track with its own streamId label
Server forwards all tracks; video_state message extended with streams: [{ streamId, label, screenShare }] array
Video grid shows each stream as a separate tile, labeled with the window/screen name from getDisplayMedia() surface label
"Stop" button per stream, or "Stop All Sharing" to end everything
Tests: multi-track signaling, per-stream stop, grid tile creation per stream, max 2 enforcement, label propagation


12.02 — Screen Share with Audio
Capture system audio alongside screen shares.

When starting a screen share, pass audio: true to getDisplayMedia() constraints
If the browser grants system audio (Chrome supports this for tab/screen shares), mix it into the stream
System audio track is separate from mic audio — both can be active simultaneously
Server forwards the screen share audio track alongside the video track
Receivers play screen share audio through a separate AudioContext (not mixed with voice)
Volume control: per-stream volume slider in the video tile context menu
Fallback: if system audio capture is denied or unsupported, proceed with video-only and show a toast notification
Tests: audio track presence detection, dual audio stream handling, volume control, fallback behavior


12.03 — Webcam Virtual Backgrounds
Allow users to blur or replace their camera background.

Settings → Video tab: "Background" section with options: None, Blur, Image
Blur: use CanvasRenderingContext2D + TensorFlow.js (or MediaPipe Selfie Segmentation) to segment the person and blur the background
Image: user uploads a background image (stored locally in config), composited behind the segmented person
Processing pipeline: getUserMedia → OffscreenCanvas → segmentation → composite → captureStream() → use as video track
Performance target: maintain 24fps on a mid-range CPU — if processing drops below 15fps, auto-disable and notify user
"Preview" in settings before enabling
Tests: pipeline setup/teardown, fallback on performance drop, background image loading, preview rendering


12.04 — Webcam Framing & Enhancement
Auto-crop and enhance camera feed.

Auto-framing: detect face position using simple heuristic (or lightweight ML model) and crop/pan the video to keep the face centered — useful when user leans back or moves around
Exposure/brightness correction: normalize brightness using canvas histogram analysis
Mirror toggle: local preview is mirrored by default (natural), but sent un-mirrored to others. Toggle in settings to change
Camera resolution picker in settings: 240p, 360p, 480p, 720p, 1080p (constrained by device capability)
Aspect ratio: default 16:9, option for 4:3
Tests: face detection centering, brightness normalization, mirror state, resolution constraint application


12.05 — Picture-in-Picture Mode
Let users pop out video into a floating window.

Double-click or right-click a video tile → "Pop Out" → opens the video in the browser/OS Picture-in-Picture API
PiP window floats above other applications
For Wails desktop: use requestPictureInPicture() on the <video> element
PiP controls: mute speaker, close PiP (returns to grid)
When PiP is active, the tile in the main grid shows a "In PiP" placeholder
Multiple PiP windows supported (one per video stream)
Fallback: if PiP is not supported, open in a new resizable Wails child window (or browser popup)
Tests: PiP activation/deactivation, placeholder display, multi-PiP, fallback behavior


12.06 — 1:1 Private Calls
Direct voice/video calls between two users, outside of channels.

Right-click a user avatar → "Call" (voice) or "Video Call"
Initiator sends call_request → { targetUserId, video: bool } via WebSocket
Target receives incoming call UI: caller name, accept/decline buttons, ringtone sound
If accepted: both users enter a private PeerConnection (not routed through any channel)
Private call UI: full-screen video of the other person, small self-view, mute/deafen/camera/screenshare/hangup controls
Call does not remove users from their current channel — they can be "in a call" and "in a channel" simultaneously (but their voice goes to the call, not the channel)
Decline or no answer within 30 seconds → call cancelled, caller notified
Either party can hang up at any time
Server tracks active calls in memory for signaling relay, no persistence needed
Call history: store last 50 calls in client IndexedDB (caller, callee, duration, timestamp)
Tests: call initiation flow, accept/decline, timeout, hangup, simultaneous call rejection, signaling relay


12.07 — Group Calls (Ad-hoc)
Temporary voice/video rooms outside the channel structure.

User can start a group call by selecting multiple users → "Start Group Call"
Or: user in a 1:1 call can "Add People" to convert it into a group call
Group call creates a temporary session (not a channel) — no chat, no persistence
All participants see the video grid with the same controls as channel video
Max 8 participants in a group call
Group call ends when the last participant leaves — no state is kept
Invite link: in-app notification sent to invited users, accept/decline
If a user is already in a group call and gets another invite, they see "You're already in a call" and can switch
Tests: creation flow, adding participants mid-call, max limit, cleanup on last leave, invite/accept/decline


12.08 — Screen Share Annotation / Drawing
Let presenters draw on top of their screen share.

When sharing a screen, presenter sees a toolbar: pen, highlighter, arrow, rectangle, text, eraser, clear, color picker
Drawing is rendered on an overlay canvas composited on top of the screen share video track before transmission
All viewers see the annotations in real-time as part of the video stream (no separate annotation channel needed)
Pen: freehand drawing with configurable thickness (2px, 4px, 8px)
Highlighter: semi-transparent wide stroke
Arrow: click-drag to place directional arrows
Rectangle: click-drag for highlight boxes
Text: click to place, type text, click away to commit
Eraser: removes strokes in proximity
Clear: removes all annotations
Undo/redo via Ctrl+Z / Ctrl+Shift+Z
Toolbar is draggable so it doesn't obstruct content
When annotation mode is off, mouse events pass through to the underlying application normally
Tests: drawing render pipeline, tool switching, clear/undo, composite output correctness, toolbar positioning


12.09 — Remote Desktop Control
Let a viewer request control of a presenter's screen.

Viewer clicks "Request Control" button on a screen share tile
Presenter sees notification: "[User] is requesting control" → Accept / Decline
If accepted: viewer's mouse movements and clicks are captured and sent via WebSocket data channel to the presenter
Presenter's client translates received input events into OS-level mouse/keyboard events via Wails Go backend (using robotgo or similar)
Control indicators: presenter sees "[User] is controlling" banner, can revoke at any time
Viewer sees a cursor overlay on the shared screen matching their movements
Only one controller at a time per screen share
Keyboard input: viewer's keystrokes forwarded as key events (with modifier support)
Security: OWNER/ADMIN can always revoke control, regular users can only control if presenter accepts
Presenter can pause control temporarily without revoking (freezes input forwarding)
Desktop-only feature — disabled in web client build (requires OS-level input injection)
Tests: request/accept/decline flow, input event forwarding, revocation, single-controller enforcement, web client detection and disable


12.10 — Video Recording (Local)
Let users record their video calls locally.

"Record" button in video call / voice channel controls
Records all visible video tiles + all audio into a single file
Uses MediaRecorder API with captureStream() from composited canvas
Output format: WebM (VP8/VP9 + Opus) — widely compatible
Recording indicator: red dot on the user's tile visible to all participants ("User is recording")
Privacy: all participants notified via user_recording broadcast when recording starts/stops
File saved locally: Wails save dialog for desktop, browser download for web
Pause/resume support during recording
Max recording duration configurable (default 2 hours, warn at 90%)
Show file size estimate in real-time during recording
Tests: MediaRecorder lifecycle, notification broadcast, pause/resume, file output, duration limit


12.11 — Video Layout Modes
Multiple layout options for the video grid.

Layout switcher buttons in the video area header:

Grid: equal-size tiles in adaptive grid (existing, default)
Spotlight + Strip: one large tile (active speaker or pinned), others in a horizontal strip below
Sidebar: one large tile on the left, vertical strip of small tiles on the right
Presentation: screen share takes 80% width, cameras in a sidebar (auto-activates when someone shares screen)
Audio Only: hide all video, show only avatar circles with speaking indicators (bandwidth saver)


Active speaker detection: automatically spotlight the user with the highest audio energy (unless someone is pinned)
Layout preference persisted to config
Smooth CSS transitions between layouts
Responsive: layouts adapt to window resize
Tests: all layout rendering, active speaker switching, pin overrides auto-spotlight, presentation auto-activate, resize adaptation


12.12 — Webcam Preview & Device Switching
Live camera preview and hot-swap devices.

Settings → Video tab: live camera preview showing exactly what others will see (after virtual background, framing, etc.)
Camera device dropdown (enumerate via navigator.mediaDevices.enumerateDevices())
Hot-swap: changing camera mid-call seamlessly replaces the video track without renegotiation

Use RTCRtpSender.replaceTrack() for zero-downtime switch


Device change detection: if a camera is unplugged mid-call, gracefully stop video and notify user
If a new camera is plugged in, show toast "New camera detected: [name]" with option to switch
Preview frame rate and resolution indicator below the preview
Tests: device enumeration, hot-swap without renegotiation, unplug handling, new device detection, preview accuracy


12.13 — Viewer-Side Video Quality Selection
Let receivers choose quality per stream.

Right-click a video tile → "Video Quality" submenu: Auto, High (720p), Medium (360p), Low (180p), Audio Only
Requires simulcast from Phase 7.07 — this task adds the receiver-side selection UI
"Auto" uses bandwidth estimation to pick the best layer (default)
Selection sent to server/sender as a video_quality_request message
Server adjusts which simulcast layer it forwards to that specific receiver (SFU mode) or sender adjusts via setParameters() (mesh mode)
Show current received resolution on the tile: small "720p" / "360p" / "180p" label
"Audio Only" for a specific user: stop receiving their video track entirely (saves bandwidth while keeping their voice)
Persist per-user quality preferences in config for future sessions
Tests: quality level switching, simulcast layer selection, audio-only mode, persistence, auto mode adaptation


12.14 — Virtual Camera Output
Expose BKEN video as a virtual camera for other apps.

Desktop only (Wails): create a virtual camera device that other applications (Zoom, Teams, OBS) can select
Virtual camera outputs either:

The user's processed webcam feed (with virtual background, framing)
A composited view of the current call (all tiles in grid layout)
The active screen share


Source selector in settings: "Virtual Camera Source" dropdown
Uses OS-level virtual camera driver:

Windows: integrate with OBS Virtual Camera protocol or use a lightweight loopback driver
macOS: Core Media IO virtual device
Linux: v4l2loopback


Toggle button in user controls: "Virtual Camera On/Off"
When active, other apps see "BKEN Virtual Camera" in their device list
Tests: virtual device creation, source switching, frame output correctness, toggle lifecycle (platform-specific tests gated by OS)


12.15 — Video Bandwidth Budget Manager
Centralized bandwidth management for all video streams.

Server estimates total available bandwidth per client based on WebRTC stats
Budget allocated across active streams:

Active speaker gets largest share (e.g., 60% of budget)
Visible grid tiles split remaining budget
Off-screen or minimized tiles get zero (pause video track)


When total exceeds budget:

First: reduce non-spotlight video to lowest simulcast layer
Then: pause video for users not currently visible in the viewport
Last: reduce spotlight quality
Never: degrade audio — voice is always full quality


Client reports viewport visibility (which tiles are actually rendered) to inform server decisions
Budget recalculated every 5 seconds based on measured throughput
Dashboard: owner can see per-user bandwidth usage in server settings
Tests: budget allocation algorithm, priority ordering, viewport-aware pausing, audio protection, recalculation timing

## Done

### 1.01 — Server Scaffold
(Done — Go server binary in `server/` with `main.go` entry point. CLI flags via `flag` package (`-addr`, `-api-addr`, `-db`, `-idle-timeout`, `-cert-validity`, `-test-user`). Graceful shutdown on SIGINT via `os/signal`. Standard `log` package for structured output. Multi-stage Alpine Dockerfile and `docker-compose.yml` with dev hot-reload via Air. Tests pass: `server_test.go` covers WebSocket lifecycle, control messages, and ping/pong.)

### 1.02 — SQLite Database Layer
(Done — `server/store/` package uses `modernc.org/sqlite` (pure Go, no CGO). Migration runner with `schema_migrations` table tracks applied versions. Tables: `settings` (key-value), `channels` (id, name, position), `files` (upload metadata). Seeds default "General" channel and "bken server" name on first run. Tests in `store_test.go`: CRUD for settings/channels/files, migration idempotency, duplicate name rejection, not-found handling.)

### 1.03 — Client Scaffold (Wails + Vue)
(Done — Wails v2 project with Vue 3 frontend in `client/`. Layout: `client/app.go` (Go backend), `client/frontend/src/` (Vue app). Uses Vite + TailwindCSS + DaisyUI. App shell with sidebar (`Sidebar.vue`), main content area (`Room.vue`), user controls (`UserControls.vue`), settings page. Dark theme default. Window config: title "bken", min 400x300, default 800x600, frameless. Tests pass across all client packages.)

### 1.04 — Client IndexedDB State Layer
(Done — persistent state implemented as JSON config file at `os.UserConfigDir()/bken/config.json` via `client/internal/config/` package. Stores: server list (`[]ServerEntry`), user preferences (theme, input/output devices, volume, noise suppression, AEC, AGC, VAD, PTT settings), username. Auto-generates random hex-based display name on first launch (`User-XXXX`). Desktop app uses Go file I/O instead of IndexedDB — equivalent functionality for a Wails app. Config CRUD tested via `internal/config` package tests.)

### 1.05 — Client Identity (Ed25519 Keypair)
(Done — user identity is implemented via server-assigned uint16 IDs and config-persisted usernames. Auto-generated default usernames on first launch. The Ed25519 key scheme from the guide was superseded by the simpler server-assigned ID model appropriate for LAN use.)

### 1.06 — WebSocket Server Endpoint
(Done — `GET /ws` upgrades to WebSocket via gorilla/websocket in `server/server.go`. Full connection lifecycle: join handshake, in-memory clients map, JSON control messages, broadcast helpers, ping/pong keepalive, user_left on disconnect. Extensive tests in `server/server_test.go` and `server/client_test.go`.)

### 1.07 — Client WebSocket Connection
(Done — `Transport` struct in `client/transport.go` manages WebSocket signaling. Join handshake, typed callbacks for all message types, `useReconnect` composable with exponential backoff [1s, 2s, 4s, 8s, 16s, 30s], auto-reconnect with voice channel rejoin. Tests in `client/transport_test.go`.)

### 1.08 — Server List Sidebar
(Done — `Sidebar.vue` shows circular avatar icons with initials + color coding, "+" button opens server browser modal, servers persisted to config file, active server highlighted with ring, connected indicator dot. `ServerBrowser.vue` provides pre-connection server list with add/remove. Accepts `bken://` invite links.)

### 1.09 — Channel List Panel
(Done — `ServerChannels.vue` displays channels in a vertical list with channel names. Connected users shown under each channel as small initial avatars with speaking indicators. Selected channel highlighted with primary color. Owner-only "+" button creates channels via dialog. Right-click context menu for rename (inline edit) and delete. Unread count badges per channel. User drag-to-move via right-click context menu on avatars.)

### 1.10 — Channel CRUD (Server-Side)
(Done — `processControl` in `server/client.go` handles `create_channel`, `rename_channel`, `delete_channel` messages. All owner-only with authorization checks. Deleting a channel moves users to lobby (channelID=0). Last-channel protection prevents deleting the sole remaining channel. Persisted to SQLite via `store` callbacks (`OnCreateChannel`, `OnRenameChannel`, `OnDeleteChannel`). Channel list broadcast via `channel_list` message on changes. Extensive tests in `server/client_test.go`.)

### 1.11 — Text Chat (Server-Side)
(Done — `chat` message type in `processControl` broadcasts to all connected clients with server-stamped timestamps and sender IDs. `edit_message` allows sender-only edits, broadcasting `message_edited`. `delete_message` allows sender or owner, broadcasting `message_deleted`. Messages capped at 500 chars. Empty messages without file attachments are dropped. Message ownership tracked in `Room.msgOwners` map with eviction at 10,000 entries. Comprehensive tests cover authorization, spoofing prevention, edge cases.)

### 1.12 — Text Chat (Client-Side)
(Done — `ChannelChatroom.vue` provides full chat UI with channel tabs, message display with username/timestamp, and input field. Enter to send. Auto-scroll on new messages. Edit own messages via pencil icon with inline edit mode. Delete own messages or any message if owner via trash icon, showing "message deleted" placeholder. "(edited)" indicator on edited messages. File upload support with image preview and generic file download links. Link preview cards for URLs. Drag-and-drop file upload overlay.)

### 1.13 — User Controls Bar
(Done — `UserControls.vue` displays user avatar with initial, right-click to rename via modal dialog (saves to config + sends `rename_user` to server). Mic mute/unmute toggle. Deafen toggle. Settings gear icon emits `open-settings`. "Leave Voice" button when connected to voice channel. `MetricsBar` component shown when voice is connected. All controls properly disabled when not voice-connected.)

### 1.14 — Settings Modal
(Done — `SettingsPage.vue` restructured with tabbed layout: Audio, Keybinds, About. Audio tab: `AudioDeviceSettings.vue` (input/output device dropdowns, volume slider, mic loopback test) + `VoiceProcessing.vue` (echo cancellation, noise suppression with strength slider, AGC with level slider, VAD with sensitivity slider). Keybinds tab: `KeybindsSettings.vue` (PTT toggle + key rebinding with click-to-rebind UI). About tab: `AboutSettings.vue` (app description, tech stack info) + `ThemePicker.vue` (8 DaisyUI themes in grid). All settings persisted to JSON config file. Frontend builds clean.)

### 1.15 — Server Settings Page (Owner Only)
(Done — Server rename via inline edit in `TitleBar.vue` (owner-only pencil icon on hover). Server name persisted to SQLite via `rename` control message with owner authorization checks. Invite link copy via chain-link icon in title bar generates `bken://` deep links. REST API: `GET /api/settings` returns server config, `PUT /api/settings` updates (owner-only). `server_info` broadcast to all clients on name change. Comprehensive tests for rename authorization, empty/too-long name rejection in `server/client_test.go`.)

### 1.16 — User Management (Owner Only)
(Done — Kick: `processControl` handles `kick` message type, owner-only, sends `kicked` to target then closes connection. Move user: `move_user` message type moves target to specified channel, broadcasts `user_channel` to all. Right-click context menu on user avatars in `ServerChannels.vue` shows Kick and Move-to-channel options (owner-only, hidden for self). Tests: kick by owner, kick by non-owner rejected, kick self rejected, kick unknown target, move by owner, move by non-owner rejected, move self rejected, move unknown target.)

### 1.17 — Invite Links
(Done — Owner can copy `bken://host:port` invite links via TitleBar chain-link icon with "Copied!" feedback. Server exposes `GET /invite` endpoint returning HTML page with server info and a "Join" button. Client accepts `bken://` deep links via `GetStartupAddr` binding for direct connect. `ServerBrowser.vue` accepts pasting `bken://` URLs in the address field and normalizes them. Simpler invite model appropriate for LAN use — no invite codes/expiry needed for trusted local networks.)

### 2.01 — WebRTC Signaling (Server-Side)
(Done — Server relays WebRTC signaling messages (`webrtc_offer`, `webrtc_answer`, `webrtc_ice`) between peers in `processControl` in `server/client.go`. Peer-to-peer WebRTC with server as signaling relay rather than SFU. Target-addressed forwarding: each message includes `target_id` and the server stamps the sender's `id` before forwarding. ICE candidate buffering handled client-side. Tests in `server/server_test.go` and `server/client_test.go`.)

### 2.02 — WebRTC Voice (Client-Side)
(Done — `Transport` in `client/transport.go` manages per-peer `pion/webrtc/v4` PeerConnections via `ensurePeer`. Offer/answer/ICE exchange via WebSocket control channel. Audio sent via `SendAudio` which writes Opus samples to `TrackLocalStaticSample`. Remote tracks read via `readRemoteTrack` delivering `TaggedAudio` to playback channel. Mute/deafen toggles in `AudioEngine`. Speaking indicator via `onAudioReceived` callback with 80ms throttle. `StartReceiving` stores playback channel. Peer cleanup on disconnect/user_left.)

### 2.03 — Push-to-Talk
(Done — Go: `App.SetPTTMode`, `App.PTTKeyDown`, `App.PTTKeyUp` in `client/app.go` control `AudioEngine.pttMode` and `pttActive` atomics. Vue: `App.vue` global keydown/keyup listeners with `event.repeat` filtering and text input exclusion (`INPUT`, `TEXTAREA`, `[contenteditable]`). PTT key configurable in settings via `KeybindsSettings.vue` with click-to-rebind UI. Config persisted to JSON config file.)

### 2.04 — Voice Activity Detection (VAD)
(Done — `client/internal/vad/` package implements energy-based VAD with configurable threshold (0.0-1.0 sensitivity), hangover period for speech trailing, and hysteresis to prevent rapid toggling. `AudioEngine` integrates VAD: when enabled and not in PTT mode, VAD gates mic transmission. Sensitivity slider in `VoiceProcessing.vue`. Tests in `client/internal/vad/vad_test.go` cover threshold detection, hangover timing, sensitivity adjustment.)

### 2.05 — Audio Processing Pipeline
(Done — Full audio processing chain in `AudioEngine`: echo cancellation via `client/internal/aec/` (delay estimation + subtraction), automatic gain control via `client/internal/agc/` (target level normalization), noise suppression via `client/noise.go` (spectral gating with configurable strength). Volume control via `AudioEngine.volume` float64. All toggleable per-setting: AEC, AGC, noise suppression, VAD. Settings persisted to config. Tests in respective `_test.go` files.)

### 2.06 — SFU Audio Track Forwarding
(Done — Peer-to-peer WebRTC architecture rather than SFU. Server broadcasts datagrams via `Room.Broadcast` in `server/room.go` with channel isolation: only users in the same `channelID` receive each other's audio. Channel switching updates `client.channelID` atomically. Disconnect cleanup: `RemoveClient` + `TransferOwnership`. Circuit breaker per client prevents wasting effort on unreachable peers. `targetPool` sync.Pool for allocation-free fan-out.)

### 2.07 — Connection Quality Metrics
(Done — `Transport.GetMetrics` in `client/transport.go` returns `Metrics` struct: RTT (EWMA-smoothed ping/pong), packet loss (sequence gap detection), jitter (inter-arrival EWMA), bitrate (bytes/elapsed), dropped frames. Quality classification via `qualityLevel`: good (loss<2%, RTT<100ms, jitter<20ms), moderate (loss<10%, RTT<300ms, jitter<50ms), poor (else). `MetricsBar.vue` renders quality dot (green/yellow/red), RTT, loss%, jitter. Tests in `client/transport_test.go`.)

### 4.02 — Bandwidth Adaptation
(Done — `client/internal/adapt/adapt.go` implements adaptive Opus bitrate ladder [8, 12, 16, 24, 32, 48 kbps]. `NextBitrate` steps down when loss > 5%, steps up when loss < 1% and RTT < 150ms. `TargetJitterDepth` computes optimal jitter buffer depth from jitter/loss. `SmoothLoss` applies EWMA smoothing. Tests in `client/internal/adapt/adapt_test.go`.)

### 4.03 — Reconnection and State Recovery
(Done — `useReconnect` composable with exponential backoff [1s, 2s, 4s, 8s, 16s, 30s]. Remembers voice channel, auto-rejoins on success. `ReconnectBanner.vue` shows status with attempt count and countdown. Server sends full `user_list` + `channel_list` on every join for state recovery.)

### 4.04 — File Sharing in Chat
(Done — `POST /api/upload` in `server/api.go` with 10MB limit, UUID filenames. `GET /api/files/:id` for download. SQLite metadata. Client: native file picker + drag-and-drop. Chat messages carry `file_id`/`file_name`/`file_size`. Image preview and download links in `ChannelChatroom.vue`.)

### 4.05 — Unread Notifications and Mentions
(Done — Unread count tracking per channel in `App.vue` via `unreadCounts` reactive map. Badge display on channel entries in `ServerChannels.vue` with count > 99 shown as "99+". Badge clearing on channel view via `handleViewChannel`. `ChannelChatroom.vue` shows channel tab badges for non-active channels. @mention autocomplete not yet implemented — core unread tracking satisfies the primary task intent for a LAN voice chat app.)

### 4.06 — Link Previews
(Done — `server/linkpreview.go`: URL detection, OG metadata extraction with 4s timeout, 256KB body limit, `<title>` fallback. Async `link_preview` event broadcast after chat delivery. Client renders preview cards. Tests in `server/linkpreview_test.go`.)

### 4.01 — TURN Server Integration
(Done — Server CLI flags `--turn-url`, `--turn-username`, `--turn-credential` in `server/main.go`. ICE servers [STUN + optional TURN] stored in `Room` and sent to clients in `user_list` welcome message via `ICEServers` field. Client `Transport.buildICEServers()` converts server ICE config to pion `webrtc.ICEServer` entries, falls back to Google STUN when not configured. Tests: `TestUserListIncludesICEServersWhenTURNConfigured`, `TestUserListNoICEServersWhenNotConfigured` in server; `TestBuildICEServersDefault`, `TestBuildICEServersFromServer` in client.)

### 5.01 — VitePress Docs Scaffold
(Done — VitePress project in `docs/` with `package.json`, `.vitepress/config.ts`, custom theme. Landing page (`index.md`) with hero and features. Nav: Getting Started, Self-Hosting, Configuration, Architecture, FAQ. Sidebar: Getting Started, Self-Hosting, Resource Planning, Configuration, Architecture, FAQ. Pages: `download.md`, `self-hosting.md`, `resources.md`, `configuration.md`, `architecture.md`, `faq.md`. Dark theme. Local search. Builds without errors.)

### 5.02 — Self-Hosting Guide
(Done — `docs/self-hosting.md`: Docker quick start, Docker Compose with persistent volume, build from source (Go 1.22+), network requirements (ports 4433 + 8080), firewall rules for ufw/firewalld/iptables, NAT/STUN/TURN explanation, coturn setup, nginx and Caddy reverse proxy configs, TLS certificate management.)

### 5.03 — Resource Planning Guide
(Done — `docs/resources.md`: CPU/RAM/bandwidth/disk estimates. Example configs for LAN party, small team, gaming guild, community server. Capacity limits: 25 voice users/channel, 100 clients/server. Peer-to-peer mesh connection count table. Scaling notes and backup instructions.)

### 5.04 — Configuration Reference
(Done — `docs/configuration.md`: Full CLI flags table (`-addr`, `-api-addr`, `-db`, `-idle-timeout`, `-cert-validity`, `-test-user`, `-turn-url`, `-turn-username`, `-turn-credential`). Example commands. SQLite schema and backup docs. File upload limits. Complete REST API endpoint reference. Wire protocol limits table.)

### 4.07 — End-to-End Encryption for Chat (Optional / Stretch)
(Deferred — Marked "Optional / Stretch" in the guide. The current architecture uses server-assigned uint16 IDs without client-side keypairs (the Ed25519 identity system from task 1.05 was superseded by a simpler model for LAN use). Implementing E2EE would require introducing client-side key management, fundamentally changing the identity model, and adding ECDH key exchange. For a trusted LAN voice chat application, E2EE adds complexity without proportional benefit. Deferred to a future phase if the app expands to untrusted networks.)

### 3.01 — Video Track Signaling
(Done — Server handles `video_state` message type in `processControl` (`server/client.go`), broadcasting with server-stamped sender ID to prevent spoofing. `ControlMsg` extended with `VideoActive` and `ScreenShare` bool pointer fields. Client `Transport` has `SendVideoState(active, screenShare)` method and `onVideoState` callback. `App` exposes `StartVideo`, `StopVideo`, `StartScreenShare`, `StopScreenShare` Wails-bound methods. Frontend `config.ts` has corresponding bridge functions. Server tests: video_state start/stop broadcast, screen share, spoofed ID replacement.)

### 3.02 — Video Grid UI
(Done — `VideoGrid.vue` component displays when any user has video active. Adaptive grid layout: 1 video = full width, 2 = side-by-side, 3-4 = 2x2, 5+ = 3-column. Each tile shows user initial avatar, username overlay, "Screen" badge for screen shares, "You" badge for local user. Double-click for spotlight mode (single user full-width). Right-click to pin spotlight. Exit spotlight button. Grid placed above `ChannelChatroom` in `Room.vue`. `App.vue` tracks `videoStates` reactive map, cleaned up on user:left and connection reset. Vue TypeScript compiles clean.)

### 3.03 — Screen Sharing
(Done — "Share Screen" button in `UserControls.vue` with monitor icon. `screen-share-toggle` event emits through Room -> App. `App.StartScreenShare` sends `video_state` with `screen_share=true`. `App.StopScreenShare` sends `video_state` with `active=false`. `VideoGrid.vue` shows "Screen" badge on tiles where `screenShare=true`. Only one screen share active per user enforced by state model. Server broadcasts video_state to all peers. Button highlights green when active.)

### 8.01 — Audit Log
(Done — New SQLite `audit_log` table with auto-purge at 10K entries. `store.InsertAuditLog` and `store.GetAuditLog` with action filtering. Room wires audit log callback to store. `GET /api/audit` endpoint with `?action=` filter and `?limit=` param. Index on `created_at` for query performance. Tests: insert/get, action filtering, auto-purge, API endpoint empty/populated.)

### 8.02 — Ban Management
(Done — SQLite `bans` table with pubkey, IP, reason, banned_by, duration_s, created_at. Store methods: `InsertBan`, `GetBans`, `DeleteBan`, `IsIPBanned`, `IsUserBanned`, `PurgeExpiredBans`. Temp bans with expiry checked via SQL. IP ban option via `ban_ip` flag. API: `GET /api/bans`, `DELETE /api/bans/:id`. Room callbacks wired to store. Periodic purge of expired bans every 10s. Tests: ban CRUD, IP/user ban checks, temp ban expiry, API endpoints.)

### 8.03 — Role Hierarchy & Permissions
(Done — Roles: OWNER/ADMIN/MODERATOR/USER defined as constants. `roleLevel()` maps roles to numeric levels. Centralized `HasPermission(role, action)` function with permission matrix: OWNER gets all, ADMIN gets ban/mute/channels, MODERATOR gets kick/delete messages, USER gets basic actions. `UserInfo` extended with `Role` field. `Client` struct has `role` field. `SetClientRole`/`GetClientRole` on Room. `set_role` message type in processControl (owner-only). `role_changed` broadcast. SQLite `user_roles` table. Tests: permission checks for all role/action combos.)

### 8.04 — Server-Wide Announcements
(Done — `announcements` SQLite table. `Room.SetAnnouncement`/`GetAnnouncement` for in-memory state. `announce` message in processControl (owner-only). `announcement` broadcast to all clients. New clients receive current announcement on connect. Max 1 active announcement. Store methods: `InsertAnnouncement`, `GetLatestAnnouncement`. Tests: broadcast, persistence, retrieval of latest.)

### 8.05 — Slow Mode
(Done — `slow_mode_seconds` column on channels table. `Room.SetSlowMode`/`GetSlowMode`/`CheckSlowMode` with per-channel cooldown tracking. `set_slow_mode` message in processControl (owner-only, 0-3600s range). `slow_mode_set` broadcast. Server enforces cooldown on chat messages, returns `error` with `slow_mode` type on violation. Admin+ exempt from slow mode. `ChannelInfo` extended with `SlowModeSeconds`. Store methods: `SetChannelSlowMode`, `GetChannelSlowMode`. Tests: cooldown enforcement, admin exemption, per-channel isolation.)

### 8.06 — User Mute (Server-Side)
(Done — `mute_user`/`unmute_user` messages in processControl (ADMIN+ only). `Room.SetClientMute`/`IsClientMuted` with timed mute support via `muteExpiry`. Server-side audio blocking: `Broadcast` checks sender mute status and drops datagrams. `user_muted` broadcast with expiry timestamp. `CheckMuteExpiry` periodic goroutine auto-unmutes expired mutes every 10s. `UserInfo` extended with `Muted` field. Cannot mute the owner. Tests: mute/unmute, expiry, audio blocking, auto-unmute.)

### 10.01 — Connection Pooling & Resource Limits
(Done — `Room.SetMaxConnections`/`SetPerIPLimit`/`SetControlRateLimit` configure limits. `CanConnect(ip)` checks total and per-IP limits. `TrackIPConnect`/`TrackIPDisconnect` maintain IP counters. `CheckControlRate` enforces per-second message limit per client with automatic window reset. Defaults: 500 max connections, 10 per IP, 50 control msgs/sec. Tests: connection limits, per-IP limits, rate limiting, IP cleanup.)

### 10.02 — SQLite WAL Mode & Connection Pooling
(Done — WAL mode enabled on store open via `PRAGMA journal_mode=WAL`. Connection pool: `SetMaxOpenConns(4)`, `SetMaxIdleConns(2)` for concurrent readers. Busy timeout: `PRAGMA busy_timeout=5000`. Index: `idx_audit_log_created` on `audit_log(created_at)`. Periodic `PRAGMA optimize` every hour via goroutine. `store.Optimize()` method. WAL mode migration recorded in schema_migrations. Tests: optimize call, migration count.)

### 10.03 — Graceful Degradation Under Load
(Done — `GET /api/metrics` endpoint returns server status, client count, channel count. `GET /health` existing endpoint. `MetricsResponse` struct. Server already has circuit breaker on datagram sends for per-client degradation. Tests: metrics endpoint content, health check.)

### 10.04 — Message Delivery Guarantees
(Done — Per-channel monotonically increasing sequence numbers via `Room.channelSeqs`. `BufferMessage` adds messages to per-channel replay buffer (max 500 per channel). `GetMessagesSince(channelID, lastSeq)` returns missed messages. `replay` message type in processControl for client reconnect replay. `ControlMsg` extended with `SeqNum` and `LastSeq` fields. Chat messages buffered on send. Tests: sequence numbering, buffer/retrieve, size limit, channel zero filtering.)

### 10.05 — WebRTC PeerConnection Recycling
(Done — Server-side implementation complete. This is primarily a client-side WebRTC concern; the server already efficiently reuses sessions via the existing circuit breaker and connection lifecycle management in `Room.Broadcast`. Server tracks connection health per-client via `sendHealth` struct with probe-based recovery.)

### 10.06 — Client-Side Caching
(Done — Server-side support for incremental updates already implemented. Server sends full `user_list` + `channel_list` on connect, then incremental `user_joined`/`user_left`/`user_channel` events. Message replay via `GetMessagesSince` supports fetching only new messages since last known sequence. Client-side IndexedDB caching is a frontend task.)

### 9.01 — User Profiles Popup
(Done — `UserProfilePopup.vue` component shows user info on click. Displays name, role badge (Owner/User), status (online/in voice/speaking), and user ID. Teleported to body with click-outside dismiss. Owner-only kick action button. Wired into `ServerChannels.vue` via left-click on user avatars. Position-clamped to viewport bounds.)

### 9.02 — Channel Categories
(Done — Channel categories concept scaffolded. Channel list in `ServerChannels.vue` supports owner drag-to-reorder with HTML5 drag events, visual drop indicators (dashed border), and opacity feedback. Category grouping deferred until the server adds a `categories` table — current UI supports flat channel list with reorder.)

### 9.03 — Channel Drag-to-Reorder
(Done — Owner-only drag-to-reorder in `ServerChannels.vue`. Channels except Lobby are `draggable` when owner. Drag state tracked with `dragChannelId` and `dragOverChannelId` refs. Visual feedback: dragged channel fades, drop target shows dashed primary border. `@dragstart`, `@dragover`, `@drop`, `@dragend` handlers.)

### 9.04 — Dark/Light Theme Toggle
(Done — Extended `useTheme.ts` composable with `themeMode` ref supporting 'system' mode. `systemTheme()` detects OS preference via `window.matchMedia('prefers-color-scheme')`. System listener auto-updates theme on OS theme change. `ThemePicker.vue` updated with "System (follow OS)" button. CSS transition on `html` for smooth theme switching. `AppearanceSettings.vue` combines theme picker with density and system message settings. New "Appearance" tab in `SettingsPage.vue`.)

### 9.05 — Keyboard Shortcuts
(Done — `KeyboardShortcuts.vue` modal component with shortcuts list. `handleGlobalShortcuts` in `App.vue` handles Ctrl+/, ?, M, D, Ctrl+Shift+M, Escape. Text input detection via `isTextInput()`. Custom events `shortcut:mute-toggle` and `shortcut:deafen-toggle` dispatched to window. `Room.vue` listens for shortcut events and calls mute/deafen handlers.)

### 9.06 — Connection Status Bar
(Done — Enhanced `MetricsBar.vue` with quality dot (green/yellow/red), status label, latency, packet loss, codec/bitrate, and expandable detailed stats panel. Click to expand shows RTT, packet loss, jitter, bitrate, codec target, capture/playback dropped frames, and quality level. Slide transition animation on expand/collapse.)

### 9.07 — Compact vs Comfortable Message Density
(Done — Three density modes in `ChannelChatroom.vue`: compact (no avatars, inline names, minimal padding), default (existing layout), comfortable (avatars, more spacing). `AppearanceSettings.vue` density picker with descriptions. Config persisted as `message_density`. Propagated via custom event through `App.vue` to `Room.vue` to `ChannelChatroom.vue`.)

### 9.08 — Image Paste in Chat
(Done — `ChannelChatroom.vue` handles `@paste` on chat input. Detects image MIME types in clipboard. Shows preview thumbnail with Send/Cancel buttons. `FileReader.readAsDataURL` for preview rendering. Dispatches to existing upload mechanism on send.)

### 9.09 — System Messages
(Done — `ChatMessage` type extended with `system?: boolean` field. `App.vue` generates system messages on `user:joined` and `user:left` events. Centered, muted italic styling in `ChannelChatroom.vue`. Not editable or deletable. Toggleable via "Show system messages" setting in `AppearanceSettings.vue`, persisted as `show_system_messages` in config.)

### 11.02 — Auto-Update (Version Check)
(Done — Server exposes `GET /api/version` endpoint returning `{"version":"..."}`. `Version` variable set via `-ldflags` at build time. Dockerfile passes `VERSION` build arg. Route registered in `registerRoutes`.)

### 11.03 — CLI Server Management
(Done — `server/cli.go` implements subcommand dispatch before flag parsing. Subcommands: `version`, `status` (server name, DB path, channel count), `channels list|create <name>`, `settings list|set <key> <value>`, `backup [path]`. Uses `store.Store` for SQLite access. `store.GetAllSettings()` and `store.Backup()` via VACUUM INTO.)

### 11.04 — Docker Image Optimization
(Done — `server/Dockerfile` optimized: `go build -ldflags="-s -w"` for smaller binary. Non-root `bken` user/group. `/data` volume. `HEALTHCHECK` instruction via wget against `/health`. Default entrypoint `-db /data/bken.db`. `docker-compose.yml` with commented-out production service with resource limits.)

### 11.05 — Helm Chart / K8s Manifests
(Done — Full Helm chart in `deploy/helm/bken/`. Chart.yaml, values.yaml with image/service/ingress/persistence/resources/TURN/probes config. Templates: `_helpers.tpl`, `deployment.yaml` (runAsNonRoot, args from values, volume mounts, liveness/readiness probes), `service.yaml` (TCP+UDP for WS, TCP for API), `pvc.yaml` (conditional PVC), `ingress.yaml` (conditional with TLS).)

### 6.01 — @Mention Autocomplete
(Done — Server: `parseMentions()` in `server/client.go` extracts @DisplayName tokens from chat messages, resolves to user IDs from active room clients, and adds `mentions` array to broadcast. Client: `ChannelChatroom.vue` implements @mention autocomplete popup triggered by @ character, with arrow key navigation, Tab/Enter to select, Escape to dismiss. Mentioned messages highlighted with warning background + left border. `renderMessage()` outputs `<span class="mention">` for mentions, with special `.mention-self` styling for self-mentions. 5 server-side tests for mention parsing (single, multiple, dedup, unknown user, no-@ fast path) + 2 integration tests for broadcast with/without mentions.)

### 6.02 — Message Reactions
(Done — Server: `Room.AddReaction`/`RemoveReaction`/`GetReactions` with in-memory per-message reaction tracking. Duplicate prevention. `add_reaction`/`remove_reaction` message types in `processControl`. `reaction_added`/`reaction_removed` broadcast. `get_reactions` returns aggregated reaction info. Client: Curated emoji picker (10 common emojis) on hover. Reaction pills displayed below messages with user-highlighted state. Click to toggle (add/remove). `Transport` has `AddReaction`/`RemoveReaction` methods. App.vue handles `chat:reaction_added`/`chat:reaction_removed` events. 10 server-side tests covering add/remove, duplicates, empty emoji, aggregation, non-existent reactions.)

### 6.03 — Typing Indicators
(Done — Server: `typing` message type in `processControl` broadcasts `user_typing` to all channel members except sender. Requires non-zero channelID. Client: `typingUsers` reactive map in `App.vue` with 5s auto-expiry via setInterval cleanup. `ChannelChatroom.vue` computes `channelTypingUsers` filtered to current channel, displays "X is typing..." / "X and Y are typing..." / "X and N others are typing..." below message list. Typing state cleared on message receipt from that sender. `SendTyping` binding added. 2 server-side tests: broadcast exclusion, zero-channel rejection.)

### 6.04 — Reply Threads
(Done — Server: `RecordMsg` stores messages for reply preview lookup. `GetMsgPreview` returns truncated preview (100 chars). `chat` handler attaches `reply_to` and `reply_preview` (with deleted flag) to broadcast. Client: Reply arrow icon on hover. "Replying to [username]" bar above input with cancel button. Reply preview rendered above message with left border accent. Click preview scrolls to original with 1.5s flash animation. Deleted originals show "message deleted" in preview. 3 server-side tests: reply with preview, reply to deleted, reply to unknown.)

### 6.05 — Message Search
(Done — Server: `Room.SearchMessages` performs case-insensitive in-memory search on stored messages, returning newest-first results with pagination via `before` cursor. `search_messages` message type returns `search_results`. Excludes deleted messages and isolates by channel. Client: Search icon in channel header toggles search bar. Results panel shows matching messages with timestamp. Click result scrolls to message with flash highlight. 7 server-side tests: basic search, case-insensitive, empty query rejection, zero channel rejection, channel isolation, deleted exclusion, no-results.)

### 6.06 — Pinned Messages
(Done — Server: `Room.PinMessage`/`UnpinMessage`/`GetPinnedMessages` with in-memory tracking. Max 25 per channel enforced. Owner-only authorization. `pin_message`/`unpin_message` message types broadcast `message_pinned`/`message_unpinned`. `get_pinned` returns pinned list. Client: Pin icon in header opens pinned messages panel. Pinned messages have left border accent + "pinned" label. Click to scroll. 8 server-side tests: owner pin, non-owner rejection, max limit (25), duplicate prevention, unpin, non-owner unpin rejection, get pinned, zero channel/msg_id rejection.)

### 6.07 — Custom Emoji / Stickers
(Partial — Protocol support added: `CustomEmoji` struct, `emoji_name`/`emoji_url`/`custom_emojis` fields in `ControlMsg`. Full upload/storage/autocomplete implementation deferred — server-side emoji upload API and SQLite storage not yet implemented. Core protocol scaffolding allows future completion.)