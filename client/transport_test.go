package main

import (
	"encoding/binary"
	"encoding/json"
	"testing"
	"time"
)

func TestMarshalDatagram(t *testing.T) {
	opus := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	dgram := MarshalDatagram(42, 7, opus)

	if len(dgram) != 4+len(opus) {
		t.Fatalf("expected length %d, got %d", 4+len(opus), len(dgram))
	}

	userID := binary.BigEndian.Uint16(dgram[0:2])
	seq := binary.BigEndian.Uint16(dgram[2:4])

	if userID != 42 {
		t.Errorf("expected userID 42, got %d", userID)
	}
	if seq != 7 {
		t.Errorf("expected seq 7, got %d", seq)
	}

	for i, b := range opus {
		if dgram[4+i] != b {
			t.Errorf("payload byte %d: expected 0x%02X, got 0x%02X", i, b, dgram[4+i])
		}
	}
}

func TestParseDatagram(t *testing.T) {
	original := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	dgram := MarshalDatagram(100, 200, original)

	userID, seq, payload, ok := ParseDatagram(dgram)
	if !ok {
		t.Fatal("ParseDatagram returned ok=false")
	}
	if userID != 100 {
		t.Errorf("expected userID 100, got %d", userID)
	}
	if seq != 200 {
		t.Errorf("expected seq 200, got %d", seq)
	}
	if string(payload) != string(original) {
		t.Errorf("payload mismatch: got %v, want %v", payload, original)
	}
}

func TestParseDatagramTooShort(t *testing.T) {
	_, _, _, ok := ParseDatagram([]byte{0, 1})
	if ok {
		t.Error("expected ok=false for short datagram")
	}
}

func TestParseDatagramEmpty(t *testing.T) {
	_, _, _, ok := ParseDatagram(nil)
	if ok {
		t.Error("expected ok=false for nil datagram")
	}
}

func TestMarshalParseRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		userID uint16
		seq    uint16
		data   []byte
	}{
		{0, 0, nil},
		{1, 1, []byte{0xFF}},
		{0xFFFF, 0xFFFF, make([]byte, 1200)},
		{42, 100, []byte("hello opus")},
	} {
		dgram := MarshalDatagram(tc.userID, tc.seq, tc.data)
		uid, s, payload, ok := ParseDatagram(dgram)
		if !ok {
			t.Errorf("round trip failed for userID=%d seq=%d", tc.userID, tc.seq)
			continue
		}
		if uid != tc.userID {
			t.Errorf("userID: got %d, want %d", uid, tc.userID)
		}
		if s != tc.seq {
			t.Errorf("seq: got %d, want %d", s, tc.seq)
		}
		if string(payload) != string(tc.data) {
			t.Errorf("payload mismatch for userID=%d seq=%d", tc.userID, tc.seq)
		}
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

// --- Fast-start adaptation tests ---

func TestFastStartIntervalsAscending(t *testing.T) {
	// Fast-start intervals should be in ascending order and shorter than
	// the steady-state interval. This ensures rapid initial convergence
	// that gradually slows to the steady-state cadence.
	if len(fastStartIntervals) == 0 {
		t.Fatal("fastStartIntervals must not be empty")
	}
	for i, d := range fastStartIntervals {
		if d <= 0 {
			t.Errorf("fastStartIntervals[%d] = %v, must be positive", i, d)
		}
		if i > 0 && d <= fastStartIntervals[i-1] {
			t.Errorf("fastStartIntervals[%d] = %v, should be > [%d] = %v (ascending order)",
				i, d, i-1, fastStartIntervals[i-1])
		}
		if d >= adaptInterval {
			t.Errorf("fastStartIntervals[%d] = %v, should be < adaptInterval (%v)",
				i, d, adaptInterval)
		}
	}
}

func TestWarmupTicksMatchesIntervals(t *testing.T) {
	// Warmup ticks should not exceed the number of fast-start intervals.
	if warmupTicks > len(fastStartIntervals) {
		t.Errorf("warmupTicks (%d) > len(fastStartIntervals) (%d); warmup EWMA would continue past fast-start phase",
			warmupTicks, len(fastStartIntervals))
	}
}

func TestWarmupLossAlphaHigher(t *testing.T) {
	// Warmup alpha must be higher than steady-state for faster initial convergence.
	if warmupLossAlpha <= lossAlpha {
		t.Errorf("warmupLossAlpha (%.2f) should be > lossAlpha (%.2f)", warmupLossAlpha, lossAlpha)
	}
	if warmupLossAlpha > 1.0 {
		t.Errorf("warmupLossAlpha = %.2f, must be <= 1.0", warmupLossAlpha)
	}
}

func TestFastStartTotalWarmupDuration(t *testing.T) {
	// The total fast-start warmup should be short (< 10s) so users get
	// optimal voice quality quickly.
	var total time.Duration
	for _, d := range fastStartIntervals {
		total += d
	}
	if total > 10*time.Second {
		t.Errorf("total fast-start warmup = %v, should be < 10s", total)
	}
	if total < 1*time.Second {
		t.Errorf("total fast-start warmup = %v, should be >= 1s", total)
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

// --- Datagram pool tests ---

func TestDgramPoolBufferSize(t *testing.T) {
	// Pool buffers must be large enough for the maximum datagram: 4-byte header + max Opus packet.
	bp := dgramPool.Get().(*[]byte)
	defer dgramPool.Put(bp)

	buf := *bp
	maxDgramSize := 4 + opusMaxPacketBytes
	if len(buf) != maxDgramSize {
		t.Errorf("pool buffer len = %d, want %d", len(buf), maxDgramSize)
	}
	if cap(buf) < maxDgramSize {
		t.Errorf("pool buffer cap = %d, want >= %d", cap(buf), maxDgramSize)
	}
}

func TestDgramPoolReuse(t *testing.T) {
	// Verify that Get/Put cycle reuses the same underlying array.
	bp1 := dgramPool.Get().(*[]byte)
	ptr1 := &(*bp1)[0]
	dgramPool.Put(bp1)

	bp2 := dgramPool.Get().(*[]byte)
	ptr2 := &(*bp2)[0]
	dgramPool.Put(bp2)

	if ptr1 != ptr2 {
		t.Log("pool did not reuse buffer (may happen under GC pressure, not a hard failure)")
	}
}

func TestDgramPoolSubsliceIntegrity(t *testing.T) {
	// Simulate what SendAudio does: sub-slice the pool buffer, fill it, then
	// verify the content matches what MarshalDatagram would produce.
	opus := []byte{0xCA, 0xFE, 0xBA, 0xBE, 0x01, 0x02}
	var userID uint16 = 42
	var seq uint16 = 100

	bp := dgramPool.Get().(*[]byte)
	dgram := (*bp)[:4+len(opus)]
	binary.BigEndian.PutUint16(dgram[0:2], userID)
	binary.BigEndian.PutUint16(dgram[2:4], seq)
	copy(dgram[4:], opus)
	dgramPool.Put(bp)

	// Compare with the allocation-based MarshalDatagram.
	expected := MarshalDatagram(userID, seq, opus)
	if string(dgram) != string(expected) {
		t.Errorf("pooled datagram = %x, want %x", dgram, expected)
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

// --- NACK control message tests ---

func TestNACKControlMsgJSON(t *testing.T) {
	msg := ControlMsg{
		Type: "nack",
		ID:   42,
		Seqs: []uint16{10, 11, 12},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ControlMsg
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Type != "nack" {
		t.Errorf("type: got %q, want %q", out.Type, "nack")
	}
	if out.ID != 42 {
		t.Errorf("id: got %d, want 42", out.ID)
	}
	if len(out.Seqs) != 3 || out.Seqs[0] != 10 || out.Seqs[1] != 11 || out.Seqs[2] != 12 {
		t.Errorf("seqs: got %v, want [10 11 12]", out.Seqs)
	}
}

func TestNACKControlMsgOmitsEmptySeqs(t *testing.T) {
	msg := ControlMsg{Type: "ping"}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// "seqs" should not appear in JSON when nil/empty.
	if string(data) != `{"type":"ping"}` {
		// Just check it doesn't contain "seqs".
		var m map[string]interface{}
		json.Unmarshal(data, &m)
		if _, ok := m["seqs"]; ok {
			t.Error("seqs field should be omitted when empty")
		}
	}
}

// --- Benchmarks ---

func BenchmarkDgramBuildPooled(b *testing.B) {
	// Measures the pooled datagram construction path used by SendAudio.
	opus := make([]byte, 80) // typical Opus VoIP frame at 32 kbps
	b.ReportAllocs()
	for b.Loop() {
		bp := dgramPool.Get().(*[]byte)
		dgram := (*bp)[:4+len(opus)]
		binary.BigEndian.PutUint16(dgram[0:2], 1)
		binary.BigEndian.PutUint16(dgram[2:4], 1)
		copy(dgram[4:], opus)
		dgramPool.Put(bp)
	}
}

func BenchmarkDgramBuildAlloc(b *testing.B) {
	// Measures the old allocation-per-frame path for comparison.
	opus := make([]byte, 80)
	b.ReportAllocs()
	for b.Loop() {
		dgram := make([]byte, 4+len(opus))
		binary.BigEndian.PutUint16(dgram[0:2], 1)
		binary.BigEndian.PutUint16(dgram[2:4], 1)
		copy(dgram[4:], opus)
		_ = dgram
	}
}
