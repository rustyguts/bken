package jitter

import (
	"testing"
	"time"
)

func TestNewClampDepth(t *testing.T) {
	b := New(0)
	if b.depth != 1 {
		t.Errorf("depth 0 should clamp to 1, got %d", b.depth)
	}
	b = New(100)
	if b.depth != ringSize/2 {
		t.Errorf("depth 100 should clamp to %d, got %d", ringSize/2, b.depth)
	}
}

func TestSingleSenderInOrder(t *testing.T) {
	b := New(2) // 40ms depth

	// Push 2 frames to prime.
	b.Push(1, 100, []byte{0xAA})
	b.Push(1, 101, []byte{0xBB})

	// First pop should yield frame 100.
	frames := b.Pop()
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}
	if frames[0].SenderID != 1 {
		t.Errorf("sender: got %d, want 1", frames[0].SenderID)
	}
	if string(frames[0].OpusData) != string([]byte{0xAA}) {
		t.Errorf("data: got %v, want [0xAA]", frames[0].OpusData)
	}

	// Second pop should yield frame 101.
	frames = b.Pop()
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}
	if string(frames[0].OpusData) != string([]byte{0xBB}) {
		t.Errorf("data: got %v, want [0xBB]", frames[0].OpusData)
	}
}

func TestReordering(t *testing.T) {
	b := New(3)

	// Push frames out of order: 10, 12, 11.
	b.Push(1, 10, []byte{10})
	b.Push(1, 12, []byte{12})
	b.Push(1, 11, []byte{11})

	// All 3 frames primed. Pop should yield them in order: 10, 11, 12.
	f := b.Pop()
	if len(f) != 1 || f[0].OpusData[0] != 10 {
		t.Fatalf("pop 1: expected seq 10, got %v", f)
	}

	f = b.Pop()
	if len(f) != 1 || f[0].OpusData[0] != 11 {
		t.Fatalf("pop 2: expected seq 11, got %v", f)
	}

	f = b.Pop()
	if len(f) != 1 || f[0].OpusData[0] != 12 {
		t.Fatalf("pop 3: expected seq 12, got %v", f)
	}
}

func TestMissingFramePLC(t *testing.T) {
	b := New(2)

	// Push seq 50 and 51 to prime.
	b.Push(1, 50, []byte{50})
	b.Push(1, 51, []byte{51})

	// Pop seq 50 — present.
	f := b.Pop()
	if f[0].OpusData == nil {
		t.Fatal("frame 50 should be present")
	}

	// Pop seq 51 — present.
	f = b.Pop()
	if f[0].OpusData == nil {
		t.Fatal("frame 51 should be present")
	}

	// Push seq 53 (skipping 52).
	b.Push(1, 53, []byte{53})

	// Pop seq 52 — missing, should signal PLC.
	f = b.Pop()
	if len(f) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(f))
	}
	if f[0].OpusData != nil {
		t.Error("frame 52 should be nil (PLC)")
	}

	// Pop seq 53 — present.
	f = b.Pop()
	if len(f) != 1 || f[0].OpusData == nil {
		t.Fatal("frame 53 should be present")
	}
}

func TestMissingFrameFECDataFromNextFrame(t *testing.T) {
	b := New(2)

	// Push seq 50 and 51 to prime.
	b.Push(1, 50, []byte{50})
	b.Push(1, 51, []byte{51})

	// Pop seq 50 — present.
	f := b.Pop()
	if f[0].OpusData == nil {
		t.Fatal("frame 50 should be present")
	}

	// Pop seq 51 — present.
	f = b.Pop()
	if f[0].OpusData == nil {
		t.Fatal("frame 51 should be present")
	}

	// Push seq 53 (skipping 52) — seq 53 is in the buffer as the "next" frame.
	b.Push(1, 53, []byte{53})

	// Pop seq 52 — missing, but seq 53 is available for FEC.
	f = b.Pop()
	if len(f) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(f))
	}
	if f[0].OpusData != nil {
		t.Error("frame 52 OpusData should be nil (missing)")
	}
	if f[0].FECData == nil {
		t.Fatal("frame 52 FECData should contain seq 53's data for FEC recovery")
	}
	if f[0].FECData[0] != 53 {
		t.Errorf("FECData should be seq 53's opus data, got %v", f[0].FECData)
	}

	// Pop seq 53 — present and normal.
	f = b.Pop()
	if len(f) != 1 || f[0].OpusData == nil {
		t.Fatal("frame 53 should be present")
	}
}

func TestMissingFrameNoFECWhenNextAlsoMissing(t *testing.T) {
	b := New(2)

	// Push seq 50 and 51 to prime.
	b.Push(1, 50, []byte{50})
	b.Push(1, 51, []byte{51})

	// Pop both.
	b.Pop()
	b.Pop()

	// Push seq 54 (skipping 52 and 53).
	b.Push(1, 54, []byte{54})

	// Pop seq 52 — missing, and seq 53 is also missing, so no FEC.
	f := b.Pop()
	if len(f) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(f))
	}
	if f[0].OpusData != nil {
		t.Error("frame 52 OpusData should be nil")
	}
	if f[0].FECData != nil {
		t.Error("frame 52 FECData should be nil when next frame is also missing")
	}
}

func TestConsecutiveLossOnlyFirstGetsFEC(t *testing.T) {
	b := New(2)

	b.Push(1, 50, []byte{50})
	b.Push(1, 51, []byte{51})

	// Pop both primed frames.
	b.Pop()
	b.Pop()

	// Push seq 54 only (52 and 53 are lost).
	b.Push(1, 54, []byte{54})

	// Pop seq 52: missing, next (53) also missing → no FEC.
	f := b.Pop()
	if f[0].FECData != nil {
		t.Error("seq 52: no FEC expected when seq 53 is also missing")
	}

	// Pop seq 53: missing, but next (54) is present → FEC available.
	f = b.Pop()
	if f[0].OpusData != nil {
		t.Error("seq 53 OpusData should be nil")
	}
	if f[0].FECData == nil || f[0].FECData[0] != 54 {
		t.Error("seq 53 should have FEC data from seq 54")
	}
}

func TestMultipleSenders(t *testing.T) {
	b := New(1) // depth 1 for fast priming

	b.Push(1, 0, []byte{0x01})
	b.Push(2, 0, []byte{0x02})

	frames := b.Pop()
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(frames))
	}

	// Both senders should have a frame.
	seen := map[uint16]bool{}
	for _, f := range frames {
		seen[f.SenderID] = true
		if f.OpusData == nil {
			t.Errorf("sender %d data should not be nil", f.SenderID)
		}
	}
	if !seen[1] || !seen[2] {
		t.Error("expected frames from both senders")
	}
}

func TestStaleSenderPruned(t *testing.T) {
	b := New(1)

	b.Push(1, 0, []byte{0x01})
	b.Pop() // consume

	// Artificially age the sender.
	b.streams[1].lastRecv = time.Now().Add(-time.Second)

	frames := b.Pop()
	if len(frames) != 0 {
		t.Errorf("expected 0 frames after stale timeout, got %d", len(frames))
	}
	if len(b.streams) != 0 {
		t.Errorf("stale sender should be pruned, streams=%d", len(b.streams))
	}
}

func TestLateArrivalDropped(t *testing.T) {
	b := New(1)

	b.Push(1, 10, []byte{10})
	b.Pop() // consume seq 10, nextPlay = 11

	// Push seq 10 again (late arrival). Should be dropped.
	b.Push(1, 10, []byte{99})

	// Push seq 11.
	b.Push(1, 11, []byte{11})

	f := b.Pop()
	if len(f) != 1 || f[0].OpusData[0] != 11 {
		t.Fatalf("expected seq 11, got %v", f)
	}
}

func TestUint16Wraparound(t *testing.T) {
	b := New(2)

	// Start near uint16 max.
	b.Push(1, 65534, []byte{0xFE})
	b.Push(1, 65535, []byte{0xFF})

	f := b.Pop()
	if f[0].OpusData[0] != 0xFE {
		t.Fatalf("expected 0xFE, got %v", f[0].OpusData)
	}

	// Push wrapped-around sequences.
	b.Push(1, 0, []byte{0x00})
	b.Push(1, 1, []byte{0x01})

	f = b.Pop() // seq 65535
	if f[0].OpusData[0] != 0xFF {
		t.Fatalf("expected 0xFF, got %v", f[0].OpusData)
	}

	f = b.Pop() // seq 0
	if f[0].OpusData[0] != 0x00 {
		t.Fatalf("expected 0x00, got %v", f[0].OpusData)
	}

	f = b.Pop() // seq 1
	if f[0].OpusData[0] != 0x01 {
		t.Fatalf("expected 0x01, got %v", f[0].OpusData)
	}
}

func TestWayAheadResetsStream(t *testing.T) {
	b := New(1)

	b.Push(1, 0, []byte{0})
	b.Pop() // consume seq 0, nextPlay = 1

	// Push seq 100 (way ahead of 1 by 99, exceeds ringSize).
	b.Push(1, 100, []byte{100})

	// Stream should have reset and re-primed at seq 100.
	if !b.streams[1].primed {
		t.Fatal("stream should be primed after reset (depth=1)")
	}

	f := b.Pop()
	if len(f) != 1 || f[0].OpusData[0] != 100 {
		t.Fatalf("expected seq 100, got %v", f)
	}
}

func TestReset(t *testing.T) {
	b := New(1)
	b.Push(1, 0, []byte{0})
	b.Push(2, 0, []byte{0})

	b.Reset()

	if len(b.streams) != 0 {
		t.Errorf("expected 0 streams after Reset, got %d", len(b.streams))
	}
}

func TestActiveSenders(t *testing.T) {
	b := New(2)

	if b.ActiveSenders() != 0 {
		t.Error("expected 0 active senders initially")
	}

	// One frame, not yet primed.
	b.Push(1, 0, []byte{0})
	if b.ActiveSenders() != 0 {
		t.Error("expected 0 active senders (not primed)")
	}

	// Prime sender 1.
	b.Push(1, 1, []byte{1})
	if b.ActiveSenders() != 1 {
		t.Errorf("expected 1 active sender, got %d", b.ActiveSenders())
	}
}

func TestSetDepthClamps(t *testing.T) {
	b := New(3)

	b.SetDepth(0)
	if b.Depth() != 1 {
		t.Errorf("SetDepth(0) should clamp to 1, got %d", b.Depth())
	}

	b.SetDepth(100)
	if b.Depth() != ringSize/2 {
		t.Errorf("SetDepth(100) should clamp to %d, got %d", ringSize/2, b.Depth())
	}

	b.SetDepth(5)
	if b.Depth() != 5 {
		t.Errorf("SetDepth(5) should set to 5, got %d", b.Depth())
	}
}

func TestSetDepthAffectsNewStreams(t *testing.T) {
	b := New(2)

	// Prime sender 1 with depth=2.
	b.Push(1, 10, []byte{10})
	b.Push(1, 11, []byte{11})
	if b.ActiveSenders() != 1 {
		t.Fatal("sender 1 should be primed")
	}

	// Change depth to 4. Sender 1 is already primed; new senders should use depth=4.
	b.SetDepth(4)

	// New sender 2 needs 4 frames to prime.
	b.Push(2, 0, []byte{0})
	b.Push(2, 1, []byte{1})
	b.Push(2, 2, []byte{2})
	if b.ActiveSenders() != 1 {
		t.Error("sender 2 should NOT be primed after 3 frames with depth=4")
	}

	b.Push(2, 3, []byte{3})
	if b.ActiveSenders() != 2 {
		t.Error("sender 2 should be primed after 4 frames")
	}
}

func TestDepthGetter(t *testing.T) {
	b := New(5)
	if b.Depth() != 5 {
		t.Errorf("Depth() = %d, want 5", b.Depth())
	}
}

func TestPrimingDoesNotConsume(t *testing.T) {
	b := New(3)

	// Push 2 frames (not enough to prime with depth=3).
	b.Push(1, 0, []byte{0})
	b.Push(1, 1, []byte{1})

	// Pop should return nothing (not primed).
	frames := b.Pop()
	if len(frames) != 0 {
		t.Errorf("expected 0 frames during priming, got %d", len(frames))
	}

	// Push 3rd frame to prime.
	b.Push(1, 2, []byte{2})

	// Now Pop should work.
	frames = b.Pop()
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame after priming, got %d", len(frames))
	}
	if frames[0].OpusData[0] != 0 {
		t.Errorf("expected seq 0, got %d", frames[0].OpusData[0])
	}
}
