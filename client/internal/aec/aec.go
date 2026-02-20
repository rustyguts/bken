// Package aec provides a Normalized Least Mean Squares (NLMS) acoustic echo
// canceller. It is designed for the bken audio pipeline where capture and
// playback run as separate goroutines processing 20 ms frames at 48 kHz.
//
// Usage:
//
//	aecProc := aec.New(960)   // 960 samples = 20 ms @ 48 kHz
//
//	// In the playback goroutine, AFTER filling the output buffer:
//	aecProc.FeedFarEnd(buf)
//
//	// In the capture goroutine, BEFORE any other processing:
//	aecProc.Process(buf)     // modifies buf in-place
package aec

import "sync"

const (
	// DefaultDelay is the bulk delay (samples) assumed between playback and the
	// echo arriving at the microphone. 1920 samples = 40 ms at 48 kHz, which
	// covers typical system latency (DAC + acoustic path + ADC).
	DefaultDelay = 1920

	// DefaultTaps is the NLMS filter length (samples). 480 samples = 10 ms at
	// 48 kHz. The filter handles residual delay and room response within this
	// window after the bulk delay.
	DefaultTaps = 480

	// DefaultStep is the NLMS step size mu (0 < mu < 2). Smaller values
	// converge more slowly but are more stable; 0.1 is conservative.
	DefaultStep = 0.1
)

// AEC is an NLMS-based acoustic echo canceller.
//
// The far-end circular buffer is large enough that the writer (FeedFarEnd)
// and reader (Process) access disjoint regions, so the mutex is only held
// briefly for the reference copy and for configuration changes.
type AEC struct {
	mu      sync.Mutex
	enabled bool

	// NLMS filter state
	weights []float64 // adaptive filter coefficients [tapLen]
	tapLen  int
	step    float64 // NLMS step size (mu)

	// Shared circular buffer for the far-end (playback) reference signal.
	// Size = frameSize + delayLen + tapLen; large enough to ensure the writer
	// and reader are always in disjoint regions.
	farBuf    []float32
	farHead   int // next write position in farBuf
	bufLen    int
	delayLen  int
	frameSize int
}

// New creates an AEC for the given PCM frame size (in samples).
// frameSize = 960 for 20 ms at 48 kHz.
func New(frameSize int) *AEC {
	bufLen := frameSize + DefaultDelay + DefaultTaps
	return &AEC{
		enabled:   true,
		weights:   make([]float64, DefaultTaps),
		tapLen:    DefaultTaps,
		step:      DefaultStep,
		farBuf:    make([]float32, bufLen),
		bufLen:    bufLen,
		delayLen:  DefaultDelay,
		frameSize: frameSize,
	}
}

// SetEnabled enables or disables echo cancellation. Enabling resets the
// filter weights so it adapts cleanly from scratch.
func (a *AEC) SetEnabled(enabled bool) {
	a.mu.Lock()
	a.enabled = enabled
	if enabled {
		for i := range a.weights {
			a.weights[i] = 0
		}
	}
	a.mu.Unlock()
}

// FeedFarEnd stores the most recent playback frame as the far-end reference.
// Call this from the playback goroutine after filling the output buffer.
func (a *AEC) FeedFarEnd(frame []float32) {
	a.mu.Lock()
	for _, s := range frame {
		a.farBuf[a.farHead] = s
		a.farHead = (a.farHead + 1) % a.bufLen
	}
	a.mu.Unlock()
}

// Process applies echo cancellation to a captured frame in-place.
// Call this from the capture goroutine before any other processing.
//
// The algorithm:
//  1. Copies the relevant far-end reference window (locked briefly).
//  2. Runs NLMS sample-by-sample outside the lock.
//  3. Output sample = near_end[i] − Σ w[k]*far_end[i+tapLen−1−k].
//     The NLMS update adapts the weights toward the actual echo path.
func (a *AEC) Process(frame []float32) {
	a.mu.Lock()
	if !a.enabled {
		a.mu.Unlock()
		return
	}

	// Copy the reference window into a contiguous slice so NLMS runs outside
	// the mutex. We need frameSize+tapLen−1 samples, starting at:
	//   startIdx = farHead − frameSize − delayLen − tapLen + 1
	// For sample i, tap k: ref[i + tapLen − 1 − k].
	refLen := a.frameSize + a.tapLen - 1
	ref := make([]float32, refLen)
	startIdx := a.farHead - a.frameSize - a.delayLen - a.tapLen + 1
	for j := range refLen {
		// Add 3*bufLen to guarantee a positive dividend before modulo.
		idx := ((startIdx + j) % a.bufLen + 3*a.bufLen) % a.bufLen
		ref[j] = a.farBuf[idx]
	}
	a.mu.Unlock()

	// NLMS processing: weights are only modified here (single goroutine).
	for i := range frame {
		// refBase: index into ref of the most-recent tap (k=0) for sample i.
		refBase := i + a.tapLen - 1

		// Compute filter output y and power of the reference vector.
		var y, powerSum float64
		for k := 0; k < a.tapLen; k++ {
			x := float64(ref[refBase-k])
			y += a.weights[k] * x
			powerSum += x * x
		}

		// Error = near-end − echo estimate.
		e := float64(frame[i]) - y

		// Normalised weight update: w[k] += mu * e * x[k] / (||x||² + ε).
		if powerSum > 1e-10 {
			step := a.step * e / powerSum
			for k := 0; k < a.tapLen; k++ {
				a.weights[k] += step * float64(ref[refBase-k])
			}
		}

		frame[i] = float32(e)
	}
}
