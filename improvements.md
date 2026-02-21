# Improvements Backlog

Concrete refactoring, simplification, and optimization tasks. Roughly prioritised high → low within each section.

---

## Backend (`server/`)

### Architecture & Structure

- [ ] **Decompose `processControl()`** — the function in `client.go` is ~620 lines handling 40+ message types as a single switch statement. Extract handler groups into dedicated functions: `handleChatMessages()`, `handleChannelOps()`, `handleAdminOps()`, `handleWebRTCSignaling()`, `handleRecordingOps()`.
- [ ] **Typed message dispatch** — instead of a raw `ControlMsg` with 100+ optional fields, define typed sub-structs per message category (e.g. `ChatPayload`, `ChannelPayload`, `WebRTCPayload`). Unmarshal into the correct type after reading the `type` field.
- [ ] **Extract `room.go` eviction helpers** — the bounded-map eviction pattern (trim slice, delete from map) appears 4+ times across `RecordMsg`, `RecordMsgOwner`, `InsertAuditLog`, `InsertBan`. Extract as a generic `evictOldest(keys *[]K, m map[K]V, limit int)` helper.
- [ ] **Replace parallel map+slice pattern** — `msgOwners`/`msgOwnerKeys` and `msgStore`/`msgStoreKeys` use parallel data structures for insertion-order tracking. Replace with a proper ordered map or a ring buffer to eliminate sync bugs.
- [ ] **Expose hardcoded limits as CLI flags** — `maxConnections` (500), `perIPLimit` (10), and `controlRateLimit` (50) are wired in `main.go` but not exposed as `-max-connections`, `-per-ip-limit`, `-rate-limit` flags. Same for `maxMsgBuffer` (500) and `maxPinnedPerChannel` (25).
- [ ] **Expose recording directory as a flag** — `recordingsDir = "recordings"` is hardcoded in `recording.go`. Add a `-recordings-dir` flag to `main.go` alongside `-db`.

### Performance

- [ ] **Fix SQL `NOT IN` subquery** — the audit log and ban eviction queries use `DELETE WHERE id NOT IN (SELECT id ... LIMIT N)` which is O(n) full-table scan. Replace with `DELETE WHERE id <= (SELECT id FROM ... ORDER BY id DESC LIMIT 1 OFFSET N)` using rowid.
- [ ] **Channel lookup: slice → map** — `SetChannelMaxUsers()` and other functions in `room.go` do a linear scan over `channels []ChannelInfo` to find a channel by ID. Maintain a `channelsByID map[int64]*ChannelInfo` for O(1) lookups.
- [ ] **Mention parsing O(n²)** — `parseMentions()` in `client.go` iterates all connected clients and does a `strings.Contains` per client. Pre-build a username→ID map on join/leave events instead.

### Error Handling

- [ ] **`tls.go`: replace `log.Fatalf` with returned errors** — `ecdsa.GenerateKey()` and `x509.CreateCertificate()` failures call `log.Fatalf`, bypassing any shutdown logic. Return errors to `main.go` for clean handling.
- [ ] **`processControl()`: validate message before acting** — several handlers (e.g. `edit_message`, `create_channel`, `rename_channel`) access fields from `ControlMsg` without checking if the required fields are non-zero/non-empty first.
- [ ] **`recording.go`: propagate file creation errors** — if `os.Create()` fails, the function returns silently. Surface the error to the caller so the `recording_start` acknowledgement can include a failure status.
- [ ] **`api.go`: distinguish "not found" from DB error** — `GetSetting()` returns `(value, bool, error)` but several API handlers collapse the `bool` and `error` paths into the same response branch.
- [ ] **`ClaimOwnership()` race condition** — two clients connecting simultaneously can both pass the `owner == 0` check before either writes. Add an atomic CAS or hold the write lock through the check-and-set.

### Code Quality

- [ ] **Named constants for magic numbers** — extract to a `const` block in `protocol.go` or a new `limits.go`: `circuitBreakerThreshold = 50`, `circuitBreakerProbeInterval = 25`, `maxRecordingDuration = 2h`, `maxMsgOwners = 10000`, `maxPinnedPerChannel = 25`.
- [ ] **`ControlMsg` field count** — the struct has 100+ fields. Add a doc comment grouping fields by message type so it's clear which fields are used for which type, or generate a table in `protocol.go`.
- [ ] **`store.go`: consistent "not found" return** — `GetSetting()` returns `(string, bool, error)` but `GetFile()` returns `(*FileRecord, error)` where `error` doubles as not-found. Pick one convention.
- [ ] **`tls.go`: configurable Common Name and SANs** — CN is hardcoded as `"bken"` and `DNSNames` only includes `"localhost"`. Accept the listen hostname and populate SANs accordingly.
- [ ] **`api.go` invite page** — the HTML for `/invite` is an inline string (lines ~265–273). Extract to a template file or embed via `//go:embed`.
- [ ] **Remove phase comments** — comments like `// Phase 8:`, `// Phase 10:` are development notes that have leaked into production code. Clean them up.

### Testing

- [ ] **Test `processControl()` handlers individually** — current tests exercise the full WebSocket path. Unit-test each handler function once decomposed.
- [ ] **Test circuit breaker behaviour** — no test verifies that a client with 50+ consecutive send failures is actually isolated.
- [ ] **Test slow mode enforcement** — `CheckSlowMode()` has no dedicated test; the existing suite does not verify the rate is actually enforced per-channel.
- [ ] **Test recording start/stop/duration-limit** — `recording_test.go` exists but confirm max-duration auto-stop is covered.

---

## Client Go (`client/`)

### Architecture & Structure

- [ ] **Decompose `transport.go`** — at 1574 lines it handles WebSocket I/O, WebRTC peer lifecycle, audio routing, metrics, and reconnect. Split into: `websocket.go` (connection + message loop), `peers.go` (WebRTC peer management), `metrics.go` (RTT/loss tracking).
- [ ] **Decompose `readControl()` switch** — the 230-line switch in `transport.go` mirrors the server problem. Extract WebRTC signal handlers, channel event handlers, and chat event handlers into separate methods.
- [ ] **Collapse 20+ identical callback setters** — every `SetOnXxx(fn func(...))` setter in `transport.go` is boilerplate. Replace with a single `Callbacks` struct passed to `NewTransport()`, eliminating the setters entirely.
- [ ] **`app.go`: merge duplicate chat handlers** — `onChatMessage` and `onChannelChatMessage` are ~95% identical. Unify them; the only difference is the presence of a `channel_id`.
- [ ] **`Transporter` interface** — 92 methods is a sign the interface is too wide. Split into `Connector`, `Chatter`, `ChannelManager`, `MediaController` interfaces and compose them.

### Performance & Memory

- [ ] **Peer stats TTL eviction** — peer RTT/loss maps in `transport.go` are never GC'd unless explicitly deleted. Add time-based eviction: if no packet from a peer for >30s, remove their stats entry.
- [ ] **Decoder map pruning** — in `audio.go`, the Opus decoder map is pruned on a frame counter (`decoderPruneInterval = 500`), not wall-clock time. Replace with time-based eviction so long-quiet users don't hold decoders indefinitely.
- [ ] **Per-user volume lookup cache** — `audio.go` playback loop does a map lookup per decoded frame per user. Cache the float32 volume locally and invalidate only on `SetUserVolume()`.

### Error Handling & Safety

- [ ] **Type assertion guards in `transport.go`** — several `syncMap.Load()` results are type-asserted without ok-checks (e.g. `v.(int64)`). Each should be `v, ok := ...; if !ok { ... }`.
- [ ] **`config.go`: distinguish error types in `Load()`** — `os.IsNotExist` should silently return defaults; permission errors and parse errors should be surfaced to the caller, not swallowed.
- [ ] **Validate cert fingerprint** — the client dials with `InsecureSkipVerify: true`. Optionally verify the server's certificate SHA-256 fingerprint (logged by the server at startup) to prevent MITM on untrusted networks.

### Code Quality

- [ ] **Deduplicate `maxFileSize`** — defined independently in `app.go` (10 MB) and also in `server/protocol.go`. The client should read it from the `user_list` welcome message's server config, or define it once in a shared constant.
- [ ] **`audio.go`: extract constants** — `FrameSize`, `opusBitrate`, jitter buffer sizes, and EWMA alphas are scattered magic numbers. Group them in a `const` block with units in the name (`frameSizeSamples`, `defaultBitrateKbps`).
- [ ] **`adaptBitrateLoop()` clarity** — the function in `app.go` mixes bitrate adaptation, jitter depth, and metrics reporting. Split into `updateBitrate()` and `emitMetrics()` helpers.

---

## Frontend (`client/frontend/src/`)

### Architecture & State Management

- [ ] **Extract `App.vue` into composables** — `App.vue` is the god component. Split into:
  - `useConnection.ts` — server connect/disconnect, auto-reconnect, startup address
  - `useChannels.ts` — channel list, user-channel mapping, auto-join
  - `useChat.ts` — message store, chat ID counter, file uploads
  - `useTypingIndicators.ts` — typing state and cleanup interval
  - `useKeyboardShortcuts.ts` — PTT and global shortcut handlers
- [ ] **Centralise localStorage/config keys** — `'bken:last-connected-addr'` (App.vue), `'bken-theme'` (useTheme.ts), and the `servers` config key are scattered magic strings. Export from a single `constants.ts`.
- [ ] **Centralise `bken://` protocol prefix** — appears in App.vue, TitleBar.vue, Room.vue, and Sidebar.vue. Define `BKEN_SCHEME = 'bken://'` in `constants.ts`.
- [ ] **Extract "fetch-update-save" config pattern** — the pattern `const cfg = await GetConfig(); await SaveConfig({...cfg, key: value})` repeats in every settings component and composable. Create a `patchConfig(patch: Partial<Config>): Promise<void>` helper in `config.ts`.

### Component Complexity

- [ ] **`ChannelChatroom.vue`: collapse density variants** — the three density modes (comfortable / default / compact) repeat the same action-button row and file attachment block. Extract a `<MessageRow>` component with a `density` prop to eliminate the triplication.
- [ ] **`ChannelChatroom.vue`: fix `send()` no-op branch** — the `replyingTo` branch and the else branch both emit `emit('send', text)` with no difference. Remove the dead branch.
- [ ] **`ServerChannels.vue`: extract drag-drop logic** — the drag-reorder handlers (`handleDragStart`, `handleDragOver`, `handleDrop`) are 60+ lines in the template setup. Extract to a `useDragSort(list, onReorder)` composable.

### Type Safety

- [ ] **Split `ChatMessage` type** — `types.ts` has a single `ChatMessage` with 15+ optional fields covering system messages, file messages, reply messages, and link previews. Split into discriminated union types: `SystemMessage | FileMessage | ReplyMessage | TextMessage`.
- [ ] **`max_users: 0` convention** — document or replace the magic `0 = unlimited` convention in `Channel` type with `max_users?: number` (absent = unlimited) and remove the comment.
- [ ] **`config.ts` Wails bridge type** — the `(window as any)['go']['main']['App']` cast bypasses all type checking. Define a typed `WailsBridge` interface matching the Go `App` methods and cast to that instead of `any`.
- [ ] **Route type safety** — route matching uses raw string comparison (`'#/settings'`). Define a `Route` type or enum and centralise route resolution.

### Dead Code

- [ ] **Delete `ServerBrowser.vue`** — the file exists but is not imported anywhere. Remove it.
- [ ] **`RequestVideoQuality()` in `config.ts`** — the function is defined but never called. Remove or implement the call site.
- [ ] **`chatIdCounter` reuse** — the counter is never reset between reconnects. Either reset it on disconnect or use a UUID per message to avoid stale IDs.

### Performance

- [ ] **`useTheme.ts`: await `SaveConfig()`** — `SaveConfig()` is called without `await`, so failures are silently swallowed. Await the call and surface errors.
- [ ] **`MetricsBar.vue`: extract poll threshold constants** — the 5-second poll interval and 5% packet loss threshold are inline magic numbers. Extract as `const METRICS_POLL_MS = 5000` and `const LOSS_WARN_THRESHOLD = 0.05`.
- [ ] **Debounce drag reorder saves** — `ServerChannels.vue` saves the channel order to config on every drop. If someone drags multiple channels quickly, this fires repeatedly. Debounce the save by ~300ms.

### UX & Consistency

- [ ] **`UserControls.vue` username `maxlength`** — hardcoded as `maxlength="32"` in the template. Should derive from the same `MaxNameLength` constant (50 bytes) used by the server, or at minimum be defined as a constant.
- [ ] **`TitleBar.vue` copy-link timeout** — `setTimeout(..., 2000)` for the copy confirmation is a magic number. Define `const COPY_FEEDBACK_MS = 2000`.
- [ ] **`SettingsPage.vue`: add error boundary** — dynamically imported settings sub-components have no fallback if the component fails to load. Add an `<ErrorBoundary>` or a `:error` slot.
- [ ] **Typing indicator `expiresAt` cleanup** — the 1-second interval in `App.vue` to prune expired typing entries runs even when no one is connected. Start/stop the interval based on connection state.
