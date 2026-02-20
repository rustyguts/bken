package vad

import (
	"math"
	"testing"
)

func TestNewDefaults(t *testing.T) {
	v := New()
	if v.threshold != DefaultThreshold {
		t.Errorf("threshold: got %f, want %f", v.threshold, DefaultThreshold)
	}
	if v.hangover != DefaultHangover {
		t.Errorf("hangover: got %d, want %d", v.hangover, DefaultHangover)
	}
	if !v.enabled {
		t.Error("expected enabled by default")
	}
}

func TestShouldSendDisabled(t *testing.T) {
	v := New()
	v.SetEnabled(false)
	// Silence should always pass through when disabled.
	if !v.ShouldSend(0) {
		t.Error("disabled VAD should always return true")
	}
}

func TestShouldSendSpeech(t *testing.T) {
	v := New()
	// A frame above the threshold must always pass.
	if !v.ShouldSend(DefaultThreshold * 2) {
		t.Error("speech frame should return true")
	}
}

func TestShouldSendSilence(t *testing.T) {
	v := New()
	// Drive hangover to zero by sending DefaultHangover+1 silent frames.
	for range DefaultHangover + 1 {
		v.ShouldSend(0)
	}
	// Now a silent frame should be suppressed.
	if v.ShouldSend(0) {
		t.Error("silent frame after hangover expired should return false")
	}
}

func TestHangoverDelay(t *testing.T) {
	v := New()
	// Trigger a speech frame to set hangover.
	v.ShouldSend(DefaultThreshold * 10)
	// For the next DefaultHangover frames of silence we should still send.
	for i := range DefaultHangover {
		if !v.ShouldSend(0) {
			t.Errorf("hangover frame %d should still return true", i)
		}
	}
	// After hangover expires, silence is suppressed.
	if v.ShouldSend(0) {
		t.Error("frame after hangover should return false")
	}
}

func TestHangoverResetOnSpeech(t *testing.T) {
	v := New()
	// Drain hangover to 1 frame remaining.
	v.ShouldSend(DefaultThreshold * 10)
	for range DefaultHangover - 1 {
		v.ShouldSend(0)
	}
	// Speech should reset hangover back to DefaultHangover.
	v.ShouldSend(DefaultThreshold * 10)
	for i := range DefaultHangover {
		if !v.ShouldSend(0) {
			t.Errorf("hangover frame %d after speech reset should return true", i)
		}
	}
}

func TestSetThresholdClamping(t *testing.T) {
	v := New()
	v.SetThreshold(-10)
	if v.threshold < 0.001 {
		t.Errorf("threshold below min after negative input: %f", v.threshold)
	}
	v.SetThreshold(200)
	if v.threshold > 0.05 {
		t.Errorf("threshold above max after oversized input: %f", v.threshold)
	}
}

func TestSetThresholdMapping(t *testing.T) {
	v := New()
	v.SetThreshold(0)
	if math.Abs(float64(v.threshold)-0.001) > 1e-6 {
		t.Errorf("level 0: got %f, want 0.001", v.threshold)
	}
	v.SetThreshold(100)
	if math.Abs(float64(v.threshold)-0.050) > 1e-6 {
		t.Errorf("level 100: got %f, want 0.050", v.threshold)
	}
}

func TestReset(t *testing.T) {
	v := New()
	v.ShouldSend(DefaultThreshold * 10) // sets remaining = DefaultHangover
	v.Reset()
	// After reset, a single silence frame should move remaining to DefaultHangover-1
	// without jumping straight to false (remaining starts at 0 post-Reset,
	// so first silence frame returns false).
	if v.ShouldSend(0) {
		t.Error("first silence after Reset should return false")
	}
}

func TestRMSZeroFrame(t *testing.T) {
	if RMS(nil) != 0 {
		t.Error("nil frame should return 0")
	}
	if RMS([]float32{}) != 0 {
		t.Error("empty frame should return 0")
	}
}

func TestRMSSine(t *testing.T) {
	// RMS of a full-amplitude sine is 1/sqrt(2) ≈ 0.7071
	const n = 960
	frame := make([]float32, n)
	for i := range frame {
		frame[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / 48000))
	}
	got := RMS(frame)
	want := float32(1.0 / math.Sqrt2)
	if math.Abs(float64(got-want)) > 0.005 {
		t.Errorf("RMS: got %f, want ~%f", got, want)
	}
}
