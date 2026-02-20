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
