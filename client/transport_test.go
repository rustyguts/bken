package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDialAddrsForWebsocketLocalhost(t *testing.T) {
	got := dialAddrsForWebsocket("localhost:8443")
	want := []string{"localhost:8443", "127.0.0.1:8443", "[::1]:8443"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDialAddrsForWebsocketIPv6Loopback(t *testing.T) {
	got := dialAddrsForWebsocket("[::1]:8443")
	want := []string{"[::1]:8443", "127.0.0.1:8443"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDialAddrsForWebsocketIPv4Loopback(t *testing.T) {
	got := dialAddrsForWebsocket("127.0.0.1:8443")
	want := []string{"127.0.0.1:8443", "[::1]:8443"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDialAddrsForWebsocketRemoteHost(t *testing.T) {
	got := dialAddrsForWebsocket("example.com:8443")
	if len(got) != 1 || got[0] != "example.com:8443" {
		t.Fatalf("got = %v, want [example.com:8443]", got)
	}
}

// --- mute set tests ---

func TestMuteUserBasic(t *testing.T) {
	tr := NewTransport()

	if tr.IsUserMuted(1) {
		t.Fatal("user 1 should not be muted initially")
	}
	tr.MuteUser(1)
	if !tr.IsUserMuted(1) {
		t.Fatal("user 1 should be muted after MuteUser")
	}
	tr.UnmuteUser(1)
	if tr.IsUserMuted(1) {
		t.Fatal("user 1 should not be muted after UnmuteUser")
	}
}

func TestMuteUserMultiple(t *testing.T) {
	tr := NewTransport()

	tr.MuteUser(10)
	tr.MuteUser(20)
	tr.MuteUser(30)

	for _, id := range []uint16{10, 20, 30} {
		if !tr.IsUserMuted(id) {
			t.Errorf("user %d should be muted", id)
		}
	}
	if tr.IsUserMuted(99) {
		t.Error("user 99 should not be muted")
	}

	ids := tr.MutedUsers()
	if len(ids) != 3 {
		t.Errorf("MutedUsers() len = %d, want 3", len(ids))
	}
}

func TestMutedSetClear(t *testing.T) {
	var ms mutedSet
	ms.Add(1)
	ms.Add(2)
	ms.Clear()

	if ms.Has(1) || ms.Has(2) {
		t.Error("all entries should be cleared")
	}
	if len(ms.Slice()) != 0 {
		t.Error("Slice should be empty after Clear")
	}
}

// TestStartReceivingNilSessionNoGoroutine verifies that StartReceiving returns
// immediately (and starts no goroutine) when the session is nil, i.e. before
// Connect has been called. Previously, a goroutine was always launched and
// the nil check happened inside the loop.
func TestStartReceivingNilSessionNoGoroutine(t *testing.T) {
	tr := NewTransport()
	// session is nil; should return immediately without panicking.
	ch := make(chan TaggedAudio, 1)
	tr.StartReceiving(t.Context(), ch)
	// If a goroutine had been started and accessed t.session unsafely, the
	// race detector would catch it. The test itself just verifies no panic.
}

// --- ControlMsg JSON tests ---

func TestChatControlMsgJSON(t *testing.T) {
	msg := ControlMsg{
		Type:     "chat",
		Username: "alice",
		Message:  "hello world",
		Ts:       1708456789000,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ControlMsg
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Type != "chat" {
		t.Errorf("type: got %q, want %q", out.Type, "chat")
	}
	if out.Username != "alice" {
		t.Errorf("username: got %q, want %q", out.Username, "alice")
	}
	if out.Message != "hello world" {
		t.Errorf("message: got %q, want %q", out.Message, "hello world")
	}
	if out.Ts != 1708456789000 {
		t.Errorf("ts: got %d, want %d", out.Ts, 1708456789000)
	}
}

func TestSendChatEmpty(t *testing.T) {
	tr := NewTransport()
	if err := tr.SendChat(""); err == nil {
		t.Error("expected error for empty message, got nil")
	}
}

func TestSendChatTooLong(t *testing.T) {
	tr := NewTransport()
	long := make([]byte, 501)
	for i := range long {
		long[i] = 'a'
	}
	if err := tr.SendChat(string(long)); err == nil {
		t.Error("expected error for oversized message, got nil")
	}
}

func TestConnectClearsMutes(t *testing.T) {
	tr := NewTransport()
	tr.MuteUser(5)
	tr.MuteUser(6)

	// Simulate the mute-clear that happens at the start of Connect.
	tr.muted.Clear()

	if tr.IsUserMuted(5) || tr.IsUserMuted(6) {
		t.Error("mutes should be cleared after reset")
	}
}

// --- disconnect reason tests ---

func TestDisconnectReasonDefault(t *testing.T) {
	// When no reason is explicitly set, readControl uses the default message.
	tr := NewTransport()

	// Verify that after clearing the reason, it's empty (so the default kicks in).
	tr.mu.Lock()
	tr.disconnectReason = ""
	tr.mu.Unlock()

	tr.mu.Lock()
	reason := tr.disconnectReason
	tr.mu.Unlock()

	if reason != "" {
		t.Errorf("expected empty disconnect reason, got %q", reason)
	}
}

func TestDisconnectReasonSetAndClear(t *testing.T) {
	tr := NewTransport()

	// Set a reason (simulating what pingLoop does).
	tr.mu.Lock()
	tr.disconnectReason = "Server unreachable (ping timeout)"
	tr.mu.Unlock()

	// Read and clear (simulating what readControl does).
	tr.mu.Lock()
	reason := tr.disconnectReason
	tr.disconnectReason = ""
	tr.mu.Unlock()

	if reason != "Server unreachable (ping timeout)" {
		t.Errorf("expected ping timeout reason, got %q", reason)
	}

	// After reading, reason should be cleared.
	tr.mu.Lock()
	after := tr.disconnectReason
	tr.mu.Unlock()
	if after != "" {
		t.Errorf("expected empty reason after read, got %q", after)
	}
}

func TestDisconnectReasonClearedOnReset(t *testing.T) {
	tr := NewTransport()

	// Simulate a stale reason from a prior session.
	tr.mu.Lock()
	tr.disconnectReason = "stale reason"
	tr.mu.Unlock()

	// Simulate what Connect does: reset the reason.
	tr.muted.Clear()
	tr.mu.Lock()
	tr.disconnectReason = ""
	tr.mu.Unlock()

	tr.mu.Lock()
	reason := tr.disconnectReason
	tr.mu.Unlock()

	if reason != "" {
		t.Errorf("expected empty reason after reset, got %q", reason)
	}
}

func TestConnectTimeoutConstant(t *testing.T) {
	// Ensure the connect timeout is reasonable (between 5s and 30s).
	if connectTimeout < 5*time.Second || connectTimeout > 30*time.Second {
		t.Errorf("connectTimeout = %v, expected between 5s and 30s", connectTimeout)
	}
}

func TestMetricsIncludesJitter(t *testing.T) {
	tr := NewTransport()
	// Initially jitter should be 0.
	m := tr.GetMetrics()
	if m.JitterMs != 0 {
		t.Errorf("initial JitterMs = %f, want 0", m.JitterMs)
	}
}

func TestJitterFieldInMetricsJSON(t *testing.T) {
	m := Metrics{
		RTTMs:       10.5,
		PacketLoss:  0.02,
		JitterMs:    3.5,
		BitrateKbps: 32.0,
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	jitter, ok := out["jitter_ms"]
	if !ok {
		t.Fatal("jitter_ms field missing from Metrics JSON")
	}
	if jitter.(float64) != 3.5 {
		t.Errorf("jitter_ms = %v, want 3.5", jitter)
	}
}

func TestPongTimeoutConstant(t *testing.T) {
	// Ensure the pong timeout is reasonable (between 3s and 15s).
	if pongTimeout < 3*time.Second || pongTimeout > 15*time.Second {
		t.Errorf("pongTimeout = %v, expected between 3s and 15s", pongTimeout)
	}
}

func TestQualityLevelGood(t *testing.T) {
	if q := qualityLevel(0.01, 50, 10, 0); q != "good" {
		t.Errorf("expected good, got %q", q)
	}
}

func TestQualityLevelModerate(t *testing.T) {
	// High loss but below poor threshold.
	if q := qualityLevel(0.05, 50, 10, 0); q != "moderate" {
		t.Errorf("expected moderate for loss=5%%, got %q", q)
	}
	// High RTT.
	if q := qualityLevel(0.01, 200, 10, 0); q != "moderate" {
		t.Errorf("expected moderate for rtt=200ms, got %q", q)
	}
	// High jitter.
	if q := qualityLevel(0.01, 50, 30, 0); q != "moderate" {
		t.Errorf("expected moderate for jitter=30ms, got %q", q)
	}
}

func TestQualityLevelPoor(t *testing.T) {
	if q := qualityLevel(0.15, 50, 10, 0); q != "poor" {
		t.Errorf("expected poor for loss=15%%, got %q", q)
	}
	if q := qualityLevel(0.01, 400, 10, 0); q != "poor" {
		t.Errorf("expected poor for rtt=400ms, got %q", q)
	}
	if q := qualityLevel(0.01, 50, 60, 0); q != "poor" {
		t.Errorf("expected poor for jitter=60ms, got %q", q)
	}
}

func TestQualityLevelBoundaries(t *testing.T) {
	// Exactly at moderate boundaries.
	if q := qualityLevel(0.02, 0, 0, 0); q != "moderate" {
		t.Errorf("loss=2%% boundary: expected moderate, got %q", q)
	}
	if q := qualityLevel(0, 100, 0, 0); q != "moderate" {
		t.Errorf("rtt=100ms boundary: expected moderate, got %q", q)
	}
	if q := qualityLevel(0, 0, 20, 0); q != "moderate" {
		t.Errorf("jitter=20ms boundary: expected moderate, got %q", q)
	}
	// Exactly at poor boundaries.
	if q := qualityLevel(0.10, 0, 0, 0); q != "poor" {
		t.Errorf("loss=10%% boundary: expected poor, got %q", q)
	}
	if q := qualityLevel(0, 300, 0, 0); q != "poor" {
		t.Errorf("rtt=300ms boundary: expected poor, got %q", q)
	}
	if q := qualityLevel(0, 0, 50, 0); q != "poor" {
		t.Errorf("jitter=50ms boundary: expected poor, got %q", q)
	}
}

// --- Drop-aware quality level tests ---

func TestQualityLevelDropsModerate(t *testing.T) {
	// Good network metrics but 1 drop/s → moderate.
	if q := qualityLevel(0, 0, 0, 1.0); q != "moderate" {
		t.Errorf("dropRate=1/s: expected moderate, got %q", q)
	}
}

func TestQualityLevelDropsPoor(t *testing.T) {
	// Good network metrics but 5 drops/s → poor.
	if q := qualityLevel(0, 0, 0, 5.0); q != "poor" {
		t.Errorf("dropRate=5/s: expected poor, got %q", q)
	}
}

func TestQualityLevelDropsBelowThreshold(t *testing.T) {
	// Less than 1 drop/s: still good if network is fine.
	if q := qualityLevel(0, 0, 0, 0.5); q != "good" {
		t.Errorf("dropRate=0.5/s: expected good, got %q", q)
	}
}

func TestQualityLevelDropsBoundaries(t *testing.T) {
	// Exactly at moderate boundary.
	if q := qualityLevel(0, 0, 0, 1); q != "moderate" {
		t.Errorf("dropRate=1 boundary: expected moderate, got %q", q)
	}
	// Exactly at poor boundary.
	if q := qualityLevel(0, 0, 0, 5); q != "poor" {
		t.Errorf("dropRate=5 boundary: expected poor, got %q", q)
	}
}

func TestMetricsIncludesDropFields(t *testing.T) {
	tr := NewTransport()
	// Simulate some playback drops.
	tr.playbackDropped.Add(3)
	m := tr.GetMetrics()
	if m.PlaybackDropped != 3 {
		t.Errorf("PlaybackDropped = %d, want 3", m.PlaybackDropped)
	}
	// Counter should be reset after GetMetrics.
	m2 := tr.GetMetrics()
	if m2.PlaybackDropped != 0 {
		t.Errorf("PlaybackDropped after reset = %d, want 0", m2.PlaybackDropped)
	}
}

func TestMetricsQualityLevelField(t *testing.T) {
	m := Metrics{
		RTTMs:       10.5,
		PacketLoss:  0.01,
		JitterMs:    3.5,
		BitrateKbps: 32.0,
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := out["quality_level"]; !ok {
		t.Fatal("quality_level field missing from Metrics JSON")
	}
}

func TestSendFailureThreshold(t *testing.T) {
	// Ensure the threshold is reasonable (between 10 and 200).
	if sendFailureThreshold < 10 || sendFailureThreshold > 200 {
		t.Errorf("sendFailureThreshold = %d, expected between 10 and 200", sendFailureThreshold)
	}
}

func TestPlaybackDroppedCounter(t *testing.T) {
	tr := NewTransport()
	// Initially zero.
	if d := tr.playbackDropped.Load(); d != 0 {
		t.Fatalf("initial playbackDropped = %d, want 0", d)
	}
	tr.playbackDropped.Add(7)
	if d := tr.playbackDropped.Load(); d != 7 {
		t.Errorf("playbackDropped = %d, want 7", d)
	}
}

func TestSendAudioNilSessionPoolSafe(t *testing.T) {
	// SendAudio with nil session should return nil without touching the pool
	// in a way that causes panics.
	tr := NewTransport()
	err := tr.SendAudio([]byte{0x01, 0x02, 0x03})
	if err != nil {
		t.Errorf("SendAudio with nil session returned error: %v", err)
	}
}

func TestOnDisconnectedCallbackSignature(t *testing.T) {
	// Verify that the callback receives a reason string.
	tr := NewTransport()
	var received string
	tr.SetOnDisconnected(func(reason string) {
		received = reason
	})

	// Simulate readControl calling the callback with a reason.
	tr.cbMu.RLock()
	cb := tr.onDisconnected
	tr.cbMu.RUnlock()
	if cb != nil {
		cb("test reason")
	}

	if received != "test reason" {
		t.Errorf("callback received %q, want %q", received, "test reason")
	}
}

// --- buildICEServers tests ---

func TestBuildICEServersDefault(t *testing.T) {
	// With no server-provided ICE servers, buildICEServers should return
	// a single entry pointing to Google STUN.
	tr := NewTransport()
	tr.mu.Lock()
	servers := tr.buildICEServers()
	tr.mu.Unlock()

	if len(servers) != 1 {
		t.Fatalf("expected 1 ICE server, got %d", len(servers))
	}
	if servers[0].URLs[0] != "stun:stun.l.google.com:19302" {
		t.Errorf("expected Google STUN URL, got %q", servers[0].URLs[0])
	}
}

func TestBuildICEServersFromServer(t *testing.T) {
	// When the server provides ICE servers (e.g. STUN + TURN), those should
	// be used instead of the default.
	tr := NewTransport()
	tr.mu.Lock()
	tr.iceServers = []ICEServerInfo{
		{URLs: []string{"stun:stun.example.com:3478"}},
		{URLs: []string{"turn:turn.example.com:3478"}, Username: "user", Credential: "pass"},
	}
	servers := tr.buildICEServers()
	tr.mu.Unlock()

	if len(servers) != 2 {
		t.Fatalf("expected 2 ICE servers, got %d", len(servers))
	}

	// First entry: STUN, no credentials.
	if servers[0].URLs[0] != "stun:stun.example.com:3478" {
		t.Errorf("server[0] URL = %q, want stun:stun.example.com:3478", servers[0].URLs[0])
	}
	if servers[0].Username != "" {
		t.Errorf("server[0] Username = %q, want empty", servers[0].Username)
	}

	// Second entry: TURN with credentials.
	if servers[1].URLs[0] != "turn:turn.example.com:3478" {
		t.Errorf("server[1] URL = %q, want turn:turn.example.com:3478", servers[1].URLs[0])
	}
	if servers[1].Username != "user" {
		t.Errorf("server[1] Username = %q, want %q", servers[1].Username, "user")
	}
	if servers[1].Credential != "pass" {
		t.Errorf("server[1] Credential = %v, want %q", servers[1].Credential, "pass")
	}
}

// --- Per-user volume tests ---

func TestUserVolumeDefault(t *testing.T) {
	tr := NewTransport()
	// Default volume should be 1.0 (100%).
	if v := tr.GetUserVolume(42); v != 1.0 {
		t.Errorf("default volume = %f, want 1.0", v)
	}
}

func TestUserVolumeSetAndGet(t *testing.T) {
	tr := NewTransport()
	tr.SetUserVolume(10, 1.5)
	if v := tr.GetUserVolume(10); v != 1.5 {
		t.Errorf("volume = %f, want 1.5", v)
	}
}

func TestUserVolumeClamped(t *testing.T) {
	tr := NewTransport()
	tr.SetUserVolume(1, -0.5)
	if v := tr.GetUserVolume(1); v != 0 {
		t.Errorf("negative volume should clamp to 0, got %f", v)
	}
	tr.SetUserVolume(1, 5.0)
	if v := tr.GetUserVolume(1); v != 2.0 {
		t.Errorf("volume > 2.0 should clamp to 2.0, got %f", v)
	}
}

func TestUserVolumeIsolated(t *testing.T) {
	tr := NewTransport()
	tr.SetUserVolume(1, 0.5)
	tr.SetUserVolume(2, 1.8)
	if v := tr.GetUserVolume(1); v != 0.5 {
		t.Errorf("user 1 volume = %f, want 0.5", v)
	}
	if v := tr.GetUserVolume(2); v != 1.8 {
		t.Errorf("user 2 volume = %f, want 1.8", v)
	}
	if v := tr.GetUserVolume(3); v != 1.0 {
		t.Errorf("unset user volume = %f, want 1.0", v)
	}
}
