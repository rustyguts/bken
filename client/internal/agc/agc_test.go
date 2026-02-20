package agc

import (
	"math"
	"testing"
)

func TestNew(t *testing.T) {
	a := New()
	if a.target != DefaultTarget {
		t.Errorf("target: got %f, want %f", a.target, DefaultTarget)
	}
	if a.gain != 1.0 {
		t.Errorf("initial gain: got %f, want 1.0", a.gain)
	}
}

func TestSetTargetClamping(t *testing.T) {
	a := New()
	a.SetTarget(-10)
	if a.target < 0.01 {
		t.Errorf("target below min after negative input: %f", a.target)
	}
	a.SetTarget(200)
	if a.target > 0.50 {
		t.Errorf("target above max after oversized input: %f", a.target)
	}
}

func TestSetTargetMapping(t *testing.T) {
	a := New()
	a.SetTarget(0)
	if math.Abs(a.target-0.01) > 1e-9 {
		t.Errorf("level 0: got %f, want 0.01", a.target)
	}
	a.SetTarget(100)
	if math.Abs(a.target-0.50) > 1e-9 {
		t.Errorf("level 100: got %f, want 0.50", a.target)
	}
}

// makeSine returns a float32 slice filled with a sine wave at the given
// amplitude (0.0–1.0).
func makeSine(samples int, amplitude float64) []float32 {
	f := make([]float32, samples)
	for i := range f {
		f[i] = float32(amplitude * math.Sin(2*math.Pi*440*float64(i)/48000))
	}
	return f
}

func rms(frame []float32) float64 {
	var sum float64
	for _, s := range frame {
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum / float64(len(frame)))
}

func TestProcessAmplifies(t *testing.T) {
	// A very quiet signal (5% amplitude) should be boosted toward DefaultTarget.
	a := New()
	a.SetTarget(50) // ~0.255

	// Run many frames so gain converges.
	frame := makeSine(960, 0.05)
	var out []float32
	for range 200 {
		cp := make([]float32, 960)
		copy(cp, frame)
		out = a.Process(cp)
	}
	got := rms(out)
	if got < DefaultTarget*0.5 {
		t.Errorf("amplification insufficient: output RMS %f, expected > %f", got, DefaultTarget*0.5)
	}
}

func TestProcessAttenuates(t *testing.T) {
	// A loud signal (90% amplitude) should be attenuated toward the target.
	a := New()
	a.SetTarget(30) // ~0.158

	frame := makeSine(960, 0.90)
	var out []float32
	for range 200 {
		cp := make([]float32, 960)
		copy(cp, frame)
		out = a.Process(cp)
	}
	got := rms(out)
	if got > 0.90 {
		t.Errorf("attenuation not applied: output RMS %f still too high", got)
	}
}

func TestProcessOutputClamped(t *testing.T) {
	// Even with very high gain the output must stay within [-1, 1].
	a := New()
	a.gain = MaxGain // force maximum gain immediately
	frame := makeSine(960, 0.5)
	a.Process(frame)
	for i, s := range frame {
		if s > 1.0 || s < -1.0 {
			t.Errorf("sample %d out of range: %f", i, s)
		}
	}
}

func TestProcessSilenceSkipsUpdate(t *testing.T) {
	// Near-silent frames should not change the gain estimate.
	a := New()
	before := a.gain
	silence := make([]float32, 960) // all zeros
	a.Process(silence)
	if a.gain != before {
		t.Errorf("gain changed on silence: %f → %f", before, a.gain)
	}
}

func TestGainBoundedByConstants(t *testing.T) {
	// Gain should never exceed [MinGain, MaxGain] after many frames.
	a := New()
	// Drive with silence-level input to push gain toward MaxGain.
	tiny := makeSine(960, 0.0001)
	for range 500 {
		cp := make([]float32, 960)
		copy(cp, tiny)
		a.Process(cp)
	}
	if a.gain > MaxGain+1e-9 {
		t.Errorf("gain exceeded MaxGain: %f", a.gain)
	}

	// Drive with very loud input to push gain toward MinGain.
	loud := makeSine(960, 0.99)
	for range 500 {
		cp := make([]float32, 960)
		copy(cp, loud)
		a.Process(cp)
	}
	if a.gain < MinGain-1e-9 {
		t.Errorf("gain below MinGain: %f", a.gain)
	}
}

func TestReset(t *testing.T) {
	a := New()
	a.gain = 5.0
	a.Reset()
	if a.gain != 1.0 {
		t.Errorf("Reset: gain %f, want 1.0", a.gain)
	}
}

func TestProcessEmptyFrame(t *testing.T) {
	a := New()
	out := a.Process(nil)
	if out != nil {
		t.Error("nil frame should return nil")
	}
	out = a.Process([]float32{})
	if len(out) != 0 {
		t.Error("empty frame should return empty slice")
	}
}
