package aec

import (
	"math"
	"testing"
)

const testFrameSize = 960

// rms returns the root-mean-square of the slice.
func rms(s []float32) float64 {
	var sum float64
	for _, v := range s {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(len(s)))
}

// sinFrame generates a sine wave frame at the given frequency.
func sinFrame(freq float64, frameIdx int) []float32 {
	out := make([]float32, testFrameSize)
	for i := range testFrameSize {
		t := float64(frameIdx*testFrameSize+i) / 48000.0
		out[i] = float32(0.5 * math.Sin(2*math.Pi*freq*t))
	}
	return out
}

// TestPassthroughWithNoReference verifies that when the far-end buffer is all
// zeros (nothing played) the captured signal passes through unchanged (within
// floating-point tolerance).
func TestPassthroughWithNoReference(t *testing.T) {
	a := New(testFrameSize)
	frame := sinFrame(440, 0)
	original := make([]float32, len(frame))
	copy(original, frame)

	a.Process(frame)

	for i, v := range frame {
		if math.Abs(float64(v-original[i])) > 1e-6 {
			t.Errorf("sample %d: expected %v, got %v", i, original[i], v)
		}
	}
}

// TestEchoConvergence verifies that when the captured signal is identical to
// the playback signal (pure echo, no near-end speech), the output RMS
// decreases significantly after many frames of adaptation.
func TestEchoConvergence(t *testing.T) {
	a := New(testFrameSize)

	const numWarmup = 300 // frames of adaptation (6 seconds)

	// Feed far-end and let AEC adapt. We simulate the delay by feeding the
	// far-end first, then presenting the same signal as near-end (as if the
	// echo arrives DefaultDelay + tapLen samples later, captured after one
	// round-trip through the AEC's assumed delay pipeline).
	//
	// For this test we use the simplest case: after N warmup frames the
	// near-end RMS should be much lower than the original signal RMS.
	freq := 440.0
	var initialRMS, finalRMS float64

	for frame := range numWarmup + 10 {
		far := sinFrame(freq, frame)
		near := sinFrame(freq, frame)
		a.FeedFarEnd(far)
		a.Process(near)
		if frame == 0 {
			initialRMS = rms(sinFrame(freq, frame))
		}
		if frame >= numWarmup {
			finalRMS += rms(near)
		}
	}
	finalRMS /= 10

	// After convergence the residual should be at least 10 dB below the input.
	ratio := initialRMS / (finalRMS + 1e-12)
	if ratio < 3.16 { // 10 dB
		t.Errorf("echo not suppressed enough: initial RMS=%.4f final RMS=%.4f ratio=%.2f (want >=3.16)",
			initialRMS, finalRMS, ratio)
	}
}

// TestDisabledPassthrough verifies that a disabled AEC passes frames unchanged.
func TestDisabledPassthrough(t *testing.T) {
	a := New(testFrameSize)
	a.SetEnabled(false)

	far := sinFrame(440, 0)
	near := sinFrame(440, 0)
	a.FeedFarEnd(far)

	original := make([]float32, len(near))
	copy(original, near)
	a.Process(near)

	for i, v := range near {
		if v != original[i] {
			t.Errorf("sample %d changed while disabled: %v → %v", i, original[i], v)
		}
	}
}

// TestSetEnabledResetsWeights verifies that re-enabling the AEC zeroes the
// filter weights.
func TestSetEnabledResetsWeights(t *testing.T) {
	a := New(testFrameSize)

	// Run a few frames to set non-zero weights.
	for i := range 20 {
		far := sinFrame(440, i)
		near := sinFrame(440, i)
		a.FeedFarEnd(far)
		a.Process(near)
	}

	// At least one weight should be non-zero now.
	anyNonZero := false
	for _, w := range a.weights {
		if w != 0 {
			anyNonZero = true
			break
		}
	}
	if !anyNonZero {
		t.Fatal("expected non-zero weights after adaptation")
	}

	// Re-enable resets weights.
	a.SetEnabled(true)
	for _, w := range a.weights {
		if w != 0 {
			t.Errorf("expected weight reset to 0 after SetEnabled(true), got %v", w)
		}
	}
}

// TestFeedFarEndAdvancesHead verifies that FeedFarEnd writes samples into the
// buffer and advances the write head by exactly frameSize.
func TestFeedFarEndAdvancesHead(t *testing.T) {
	a := New(testFrameSize)
	before := a.farHead

	frame := sinFrame(220, 0)
	a.FeedFarEnd(frame)

	expected := (before + testFrameSize) % a.bufLen
	if a.farHead != expected {
		t.Errorf("farHead: want %d, got %d", expected, a.farHead)
	}
}

// TestFarEndBufferWraps verifies that the ring buffer wraps correctly when
// more frames are fed than the buffer size.
func TestFarEndBufferWraps(t *testing.T) {
	a := New(testFrameSize)

	// Fill the buffer more than once.
	totalFrames := a.bufLen/testFrameSize + 5
	for i := range totalFrames {
		a.FeedFarEnd(sinFrame(440, i))
	}

	// farHead should be within [0, bufLen).
	if a.farHead < 0 || a.farHead >= a.bufLen {
		t.Errorf("farHead out of range: %d (bufLen=%d)", a.farHead, a.bufLen)
	}
}

// TestProcessOutputBounded verifies that the AEC does not produce samples
// outside the [-2, 2] range (a generous bound; in practice much tighter).
func TestProcessOutputBounded(t *testing.T) {
	a := New(testFrameSize)
	for i := range 50 {
		far := sinFrame(440, i)
		near := sinFrame(440, i)
		a.FeedFarEnd(far)
		a.Process(near)
		for j, v := range near {
			if v < -2 || v > 2 {
				t.Errorf("frame %d sample %d out of bounds: %v", i, j, v)
			}
		}
	}
}

// BenchmarkAECProcess measures the hot-path cost of Process (reference copy +
// NLMS update) for a single 20 ms frame.
func BenchmarkAECProcess(b *testing.B) {
	a := New(testFrameSize)
	// Warm up the far-end buffer so the reference window has real data.
	for i := range 10 {
		a.FeedFarEnd(sinFrame(440, i))
	}
	frame := sinFrame(440, 0)
	buf := make([]float32, testFrameSize)

	b.ResetTimer()
	for b.Loop() {
		copy(buf, frame)
		a.Process(buf)
	}
}

// BenchmarkAECFeedFarEnd measures the cost of writing one 20 ms frame into
// the circular far-end buffer.
func BenchmarkAECFeedFarEnd(b *testing.B) {
	a := New(testFrameSize)
	frame := sinFrame(440, 0)

	b.ResetTimer()
	for b.Loop() {
		a.FeedFarEnd(frame)
	}
}

// TestNewDefaults verifies the AEC is created with correct defaults.
func TestNewDefaults(t *testing.T) {
	a := New(testFrameSize)

	if !a.enabled {
		t.Error("AEC should be enabled by default")
	}
	if a.tapLen != DefaultTaps {
		t.Errorf("tapLen: want %d, got %d", DefaultTaps, a.tapLen)
	}
	if a.delayLen != DefaultDelay {
		t.Errorf("delayLen: want %d, got %d", DefaultDelay, a.delayLen)
	}
	if a.step != DefaultStep {
		t.Errorf("step: want %v, got %v", DefaultStep, a.step)
	}
	if len(a.weights) != DefaultTaps {
		t.Errorf("weights len: want %d, got %d", DefaultTaps, len(a.weights))
	}
	expectedBuf := testFrameSize + DefaultDelay + DefaultTaps
	if a.bufLen != expectedBuf {
		t.Errorf("bufLen: want %d, got %d", expectedBuf, a.bufLen)
	}
}
