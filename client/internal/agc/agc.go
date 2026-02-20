// Package agc implements a simple software Automatic Gain Control processor
// for mono float32 PCM audio at 48 kHz, 960-sample (20 ms) frames.
//
// The AGC continuously monitors the short-term RMS of each frame and adjusts a
// multiplicative gain toward a desired target level using independent
// attack/release time constants. Gain is clamped to [minGain, maxGain] to
// prevent silence amplification from going wild.
package agc

import (
	"client/internal/vad"
)

const (
	// DefaultTarget is the desired RMS level (linear, ~-14 dBFS).
	DefaultTarget = 0.20

	// MinGain prevents boosting very quiet signals beyond 20 dB.
	MinGain = 0.1
	// MaxGain allows up to +20 dB of amplification.
	MaxGain = 10.0

	// AttackCoeff controls how quickly gain is reduced when level exceeds target.
	// Higher → faster attack. Value chosen for ~5 ms effective time at 48 kHz/960.
	AttackCoeff = 0.80
	// ReleaseCoeff controls how quickly gain recovers after a loud transient.
	// Slower than attack to avoid pumping artefacts.
	ReleaseCoeff = 0.02

	// minRMS suppresses gain updates on silent frames (below noise floor).
	minRMS = 0.001
)

// AGC is a single-channel automatic gain control processor. Zero value is not
// usable; use New().
type AGC struct {
	target float64 // desired RMS level [0.0, 1.0]
	gain   float64 // current linear gain multiplier
}

// New returns an AGC with DefaultTarget and unity gain.
func New() *AGC {
	return &AGC{target: DefaultTarget, gain: 1.0}
}

// SetTarget sets the desired RMS level. level is in the range [0, 100] and is
// mapped linearly to [0.01, 0.50].
func (a *AGC) SetTarget(level int) {
	if level < 0 {
		level = 0
	}
	if level > 100 {
		level = 100
	}
	// Map [0,100] → [0.01, 0.50]
	a.target = 0.01 + float64(level)/100.0*0.49
}

// Process applies gain to frame in-place and updates the gain estimate.
// frame must be mono float32 PCM. Returns the same slice for chaining.
func (a *AGC) Process(frame []float32) []float32 {
	if len(frame) == 0 {
		return frame
	}

	rms := float64(vad.RMS(frame))

	// Apply current gain before updating, so the listener hears the result.
	for i, s := range frame {
		v := s * float32(a.gain)
		if v > 1.0 {
			v = 1.0
		} else if v < -1.0 {
			v = -1.0
		}
		frame[i] = v
	}

	// Skip gain update on near-silence to avoid boosting noise floor.
	if rms < minRMS {
		return frame
	}

	// Desired gain to hit target.
	desired := a.target / rms
	if desired < MinGain {
		desired = MinGain
	} else if desired > MaxGain {
		desired = MaxGain
	}

	// Asymmetric smoothing: attack (gain down) is fast, release (gain up) slow.
	var coeff float64
	if desired < a.gain {
		coeff = AttackCoeff
	} else {
		coeff = ReleaseCoeff
	}
	a.gain = a.gain + coeff*(desired-a.gain)

	return frame
}

// Gain returns the current linear gain multiplier (informational).
func (a *AGC) Gain() float64 { return a.gain }

// Reset resets the gain to unity without changing the target.
func (a *AGC) Reset() { a.gain = 1.0 }
