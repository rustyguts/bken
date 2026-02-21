package noisegate

import (
	"math"
	"testing"
)

func makeSineFrame(amplitude float32, size int) []float32 {
	frame := make([]float32, size)
	for i := range frame {
		t := float64(i) / 48000.0
		frame[i] = amplitude * float32(math.Sin(2*math.Pi*440*t))
	}
	return frame
}

func makeSilentFrame(size int) []float32 {
	return make([]float32, size)
}

func TestGateZeroesSilentFrames(t *testing.T) {
	g := New()
	// A very quiet frame should be zeroed.
	frame := makeSineFrame(0.0005, 960) // well below default threshold
	g.Process(frame)
	for i, s := range frame {
		if s != 0 {
			t.Fatalf("frame[%d] = %f, expected 0 (gated)", i, s)
		}
	}
}

func TestGatePassesLoudFrames(t *testing.T) {
	g := New()
	frame := makeSineFrame(0.5, 960) // well above threshold
	orig := make([]float32, len(frame))
	copy(orig, frame)
	g.Process(frame)
	// Frame should not be zeroed.
	nonZero := false
	for _, s := range frame {
		if s != 0 {
			nonZero = true
			break
		}
	}
	if !nonZero {
		t.Fatal("loud frame was zeroed; gate should pass it through")
	}
}

func TestGateHoldPreventsChatter(t *testing.T) {
	g := New()
	g.hold = 3

	// Open the gate with a loud frame.
	loud := makeSineFrame(0.5, 960)
	g.Process(loud)
	if !g.IsOpen() {
		t.Fatal("gate should be open after loud frame")
	}

	// Next 3 silent frames should still pass (hold period).
	for i := 0; i < 3; i++ {
		silent := makeSilentFrame(960)
		g.Process(silent)
		if !g.IsOpen() {
			t.Fatalf("gate closed during hold period at frame %d", i)
		}
	}

	// 4th silent frame should be gated.
	silent := makeSilentFrame(960)
	g.Process(silent)
	if g.IsOpen() {
		t.Fatal("gate should be closed after hold expired")
	}
}

func TestGateDisabledIsNoOp(t *testing.T) {
	g := New()
	g.SetEnabled(false)

	frame := makeSineFrame(0.0001, 960) // very quiet
	orig := make([]float32, len(frame))
	copy(orig, frame)
	g.Process(frame)

	// Frame should be unchanged.
	for i := range frame {
		if frame[i] != orig[i] {
			t.Fatalf("frame[%d] modified when gate disabled: got %f, want %f", i, frame[i], orig[i])
		}
	}
}

func TestGateSetThreshold(t *testing.T) {
	g := New()
	g.SetThreshold(0)
	if g.Threshold() < 0.001 || g.Threshold() > 0.002 {
		t.Errorf("threshold at level 0: got %f, expected ~0.001", g.Threshold())
	}
	g.SetThreshold(100)
	if g.Threshold() < 0.099 || g.Threshold() > 0.101 {
		t.Errorf("threshold at level 100: got %f, expected ~0.10", g.Threshold())
	}
	g.SetThreshold(50)
	expected := float32(0.001 + 0.099*0.5)
	if math.Abs(float64(g.Threshold()-expected)) > 0.001 {
		t.Errorf("threshold at level 50: got %f, expected ~%f", g.Threshold(), expected)
	}
}

func TestGateSetThresholdClamp(t *testing.T) {
	g := New()
	g.SetThreshold(-10)
	if g.Threshold() < 0.001 {
		t.Error("negative level should clamp to 0")
	}
	g.SetThreshold(200)
	if g.Threshold() > 0.101 {
		t.Error("level > 100 should clamp to 100")
	}
}

func TestGateReturnsRMS(t *testing.T) {
	g := New()
	frame := makeSineFrame(0.5, 960)
	rms := g.Process(frame)
	if rms <= 0 {
		t.Errorf("Process returned rms=%f, expected > 0", rms)
	}
}

func TestGateReset(t *testing.T) {
	g := New()
	// Open gate and start hold.
	loud := makeSineFrame(0.5, 960)
	g.Process(loud)
	g.Reset()
	if g.IsOpen() {
		t.Fatal("gate should be closed after Reset")
	}
	// Silent frame should now be gated.
	silent := makeSilentFrame(960)
	g.Process(silent)
	if g.IsOpen() {
		t.Fatal("gate should remain closed for silent frame after Reset")
	}
}

func TestGateInteractionWithVAD(t *testing.T) {
	// Gate cleans audio, then VAD decides transmission.
	// Simulate: gate zeroes quiet noise, VAD sees silence and suppresses.
	g := New()
	g.SetThreshold(50) // moderate threshold

	quiet := makeSineFrame(0.002, 960) // below gate threshold
	g.Process(quiet)

	// After gating, frame should be silent.
	allZero := true
	for _, s := range quiet {
		if s != 0 {
			allZero = false
			break
		}
	}
	if !allZero {
		t.Fatal("gate should zero quiet frames so VAD sees silence")
	}
}
