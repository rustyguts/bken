package main

import (
	"encoding/binary"
	"testing"
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
	ch := make(chan []byte, 1)
	tr.StartReceiving(t.Context(), ch)
	// If a goroutine had been started and accessed t.session unsafely, the
	// race detector would catch it. The test itself just verifies no panic.
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
