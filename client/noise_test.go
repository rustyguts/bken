package main

import (
	"math/rand"
	"testing"

	"client/internal/vad"
)

func TestNoiseCancellerBypass(t *testing.T) {
	nc := NewNoiseCanceller()
	defer nc.Destroy()

	nc.SetEnabled(true)
	nc.SetLevel(0.0) // dry signal — output must equal input exactly

	buf := make([]float32, FrameSize)
	for i := range buf {
		buf[i] = float32(i) / float32(FrameSize)
	}
	original := make([]float32, FrameSize)
	copy(original, buf)

	nc.Process(buf)

	for i := range buf {
		if buf[i] != original[i] {
			t.Fatalf("sample[%d]: got %v, want %v (level=0 must leave signal unchanged)", i, buf[i], original[i])
		}
	}
}

func TestNoiseCancellerDisabled(t *testing.T) {
	nc := NewNoiseCanceller()
	defer nc.Destroy()

	nc.SetEnabled(false)
	nc.SetLevel(1.0) // full suppression requested but enabled=false

	buf := make([]float32, FrameSize)
	for i := range buf {
		buf[i] = rand.Float32()*2 - 1
	}
	original := make([]float32, FrameSize)
	copy(original, buf)

	nc.Process(buf)

	for i := range buf {
		if buf[i] != original[i] {
			t.Fatalf("sample[%d]: got %v, want %v (disabled must leave signal unchanged)", i, buf[i], original[i])
		}
	}
}

func TestNoiseCancellerVADProbabilityWhiteNoise(t *testing.T) {
	nc := NewNoiseCanceller()
	defer nc.Destroy()

	nc.SetEnabled(true)
	nc.SetLevel(1.0)

	rng := rand.New(rand.NewSource(42))

	// Warm up RNNoise with several frames.
	buf := make([]float32, FrameSize)
	for f := 0; f < 10; f++ {
		for i := range buf {
			buf[i] = rng.Float32()*2 - 1
		}
		nc.Process(buf)
	}

	// After processing pure noise, VAD probability should be low.
	prob := nc.VADProbability()
	if prob > 0.5 {
		t.Errorf("white noise VAD probability should be <= 0.5, got %f", prob)
	}
	t.Logf("white noise VAD probability: %.4f", prob)
}

func TestNoiseCancellerVADProbabilityInitialZero(t *testing.T) {
	nc := NewNoiseCanceller()
	defer nc.Destroy()

	// Before any processing, VAD probability should be 0.
	if prob := nc.VADProbability(); prob != 0 {
		t.Errorf("initial VAD probability should be 0, got %f", prob)
	}
}

func TestNoiseCancellerReducesWhiteNoise(t *testing.T) {
	nc := NewNoiseCanceller()
	defer nc.Destroy()

	nc.SetEnabled(true)
	nc.SetLevel(1.0)

	rng := rand.New(rand.NewSource(42))

	// Warm up RNNoise with several frames so its internal state stabilises.
	const warmupFrames = 10
	warmup := make([]float32, FrameSize)
	for f := 0; f < warmupFrames; f++ {
		for i := range warmup {
			warmup[i] = rng.Float32()*2 - 1
		}
		nc.Process(warmup)
	}

	// Now measure suppression over 20 frames.
	const testFrames = 20
	var inputRMS, outputRMS float64
	buf := make([]float32, FrameSize)
	for f := 0; f < testFrames; f++ {
		for i := range buf {
			buf[i] = rng.Float32()*2 - 1
		}
		inputRMS += float64(vad.RMS(buf))

		nc.Process(buf)
		outputRMS += float64(vad.RMS(buf))
	}
	inputRMS /= testFrames
	outputRMS /= testFrames

	if outputRMS >= inputRMS {
		t.Fatalf("noise suppression had no effect: input RMS=%.4f output RMS=%.4f", inputRMS, outputRMS)
	}
	t.Logf("input RMS=%.4f output RMS=%.4f (reduction=%.1f%%)", inputRMS, outputRMS, (1-outputRMS/inputRMS)*100)
}
