package main

import "testing"

func TestNoiseCancellerNoopProcess(t *testing.T) {
	nc := NewNoiseCanceller()
	buf := make([]float32, FrameSize)
	for i := range buf {
		buf[i] = float32(i) / float32(FrameSize)
	}
	original := append([]float32(nil), buf...)

	nc.SetEnabled(true)
	nc.Process(buf)

	for i := range buf {
		if buf[i] != original[i] {
			t.Fatalf("sample[%d]: got %v, want %v", i, buf[i], original[i])
		}
	}
}

func TestNoiseCancellerCompatibilityMethods(t *testing.T) {
	nc := NewNoiseCanceller()
	nc.SetEnabled(true)
	nc.SetLevel(0.75)
	if got := nc.VADProbability(); got != 0 {
		t.Fatalf("VADProbability = %v, want 0", got)
	}
	nc.Destroy()
}
