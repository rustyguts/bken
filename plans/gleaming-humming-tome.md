# Dead Code Removal Plan

## Context

The codebase has accumulated no-op methods kept for "backward compatibility", stub functions that always fail, an unused composable, and one event emitted from Go that no frontend component listens to. Removing these reduces surface area, eliminates misleading code, and makes the real feature boundary clearer.

---

## Items to Remove

### 1. `useReconnect.ts` — entire file (never imported)

**File:** `client/frontend/src/composables/useReconnect.ts`

The file exports `useReconnect()` but is not imported anywhere in the codebase. Reconnect logic lives inline in `App.vue`.

**Action:** Delete file.

---

### 2. `SetNoiseSuppressionLevel` — no-op chain

`client/app.go:227-230` does `_ = level` and nothing else. Never called from any Vue component.

**Files to edit:**
- `client/app.go` — remove `SetNoiseSuppressionLevel` method
- `client/frontend/src/browser-transport.ts` — remove `SetNoiseSuppressionLevel: () => Promise.resolve()` entry
- `client/frontend/src/__tests__/setup.ts` — remove `SetNoiseSuppressionLevel: vi.fn()...` entry
- `client/frontend/wailsjs/go/main/App.d.ts` — remove binding (or regenerate)
- `client/frontend/wailsjs/go/main/App.js` — remove binding (or regenerate)

---

### 3. `SetAGCLevel` — no-op chain

`client/app.go:218-220` calls `audio.SetAGCLevel()`. `client/audio.go:206-208` is `_ = level`. Documented "legacy no-op" in both places. Never called from any Vue component (only defined in `config.ts`, never invoked).

**Files to edit:**
- `client/app.go` — remove `SetAGCLevel` method
- `client/audio.go` — remove `SetAGCLevel` method
- `client/frontend/src/config.ts` — remove `SetAGCLevel` export (lines 166–167)
- `client/frontend/src/browser-transport.ts` — remove `SetAGCLevel` mock entry
- `client/frontend/src/__tests__/setup.ts` — remove `SetAGCLevel` mock entry
- `client/app_test.go` — remove `TestSetAGCLevel` test (lines 1112–1115)
- `client/frontend/wailsjs/go/main/App.d.ts` — remove binding
- `client/frontend/wailsjs/go/main/App.js` — remove binding

---

### 4. `SendTyping` — stub that does nothing, never called

`client/transport.go:700-704`: validates `channelID != 0` then returns `nil` — no message is ever sent to the server. `client/app.go:1085-1094` wraps it. Imported in `App.vue` but the call site was never written.

**Files to edit:**
- `client/transport.go` — remove `SendTyping` method
- `client/app.go` — remove `SendTyping` wrapper method
- `client/interfaces.go` — remove `SendTyping(channelID int64) error` from `Transporter`
- `client/app_test.go` — remove mock `SendTyping` from `mockTransport`
- `client/frontend/src/config.ts` — remove `SendTyping` export (lines 256–258)
- `client/frontend/src/App.vue` — remove `SendTyping` from import line 3
- `client/frontend/src/browser-transport.ts` — remove `SendTyping` mock entry
- `client/frontend/src/__tests__/setup.ts` — remove `SendTyping: vi.fn()` entry
- `client/frontend/wailsjs/go/main/App.d.ts` — remove binding
- `client/frontend/wailsjs/go/main/App.js` — remove binding

---

### 5. `wireCallbacks()` — test helper embedded in production code

`client/app.go:369-382`: documented "keeps legacy tests/builders working." Called only from `TestWireCallbacksSetsAllCallbacks` in `app_test.go`. Production code only uses `wireSessionCallbacks()`.

**Files to edit:**
- `client/app.go` — remove `wireCallbacks()` method
- `client/app_test.go` — update `TestWireCallbacksSetsAllCallbacks` to call `a.wireSessionCallbacks("test", mt)` directly instead of `a.wireCallbacks()`

---

### 6. `video:quality_request` orphaned event

`client/app.go:659-665` emits `"video:quality_request"` via Wails. No `EventsOn('video:quality_request', ...)` listener exists anywhere in the frontend. The `onVideoQualityReq` callback is set but the event it fires is never consumed.

Note: `RequestVideoQuality` (frontend → Go → server, outgoing direction) is a separate method used by `VideoGrid.vue` and is **not** being removed.

**Files to edit:**
- `client/app.go` — remove `tr.SetOnVideoQualityRequest(...)` block (lines 659–665)
- `client/transport.go` — remove `onVideoQualityReq` field, `SetOnVideoQualityRequest` setter, and the `"set_video_quality"` case in `readControl`
- `client/interfaces.go` — remove `SetOnVideoQualityRequest` from `Transporter` interface (line 50)
- `client/app_test.go` — remove `onVideoQualityReq` field and `SetOnVideoQualityRequest` from `mockTransport`; remove `onVideoQualityReq` nil-check in `TestWireCallbacksSetsAllCallbacks`

---

## Files NOT being changed (scope boundary)

- Legacy protocol message handlers in `transport.go` (reactions, message editing, video state, typing, pinning, etc.) — these are intentional compatibility with a richer backend. The CLAUDE.md notes the client supports a richer protocol than the current server.
- `SendFileChat` stub and the `UploadFile`/`UploadFileFromPath` broken flow — that's a broken feature, not purely dead code; should be addressed separately.
- `video:layers` / `video:state` / all other events that have both emitters and listeners — already wired end-to-end.

---

## Verification

```bash
# Go tests (client)
cd client && go test ./...

# Frontend tests
cd client/frontend && bun run test

# Regenerate Wails bindings after removing Go methods
cd client && wails generate module
```

After removing Go methods, `wails generate module` will drop those entries from `wailsjs/go/main/App.d.ts` and `App.js` automatically, so manually editing those files is optional (do it to keep the repo clean, or rely on regeneration).
