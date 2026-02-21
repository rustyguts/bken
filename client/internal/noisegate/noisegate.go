// Package noisegate implements a hard noise gate for mono float32 PCM audio.
//
// Audio frames with RMS below the configured threshold are zeroed out entirely.
// The gate is independent of VAD: it cleans the signal before VAD decides
// whether to transmit. A short hold period prevents the gate from chopping
// speech during brief pauses.
package noisegate

import "client/internal/vad"

const (
	// DefaultThreshold is the RMS level below which audio is gated (~-40 dBFS).
	DefaultThreshold = float32(0.01)

	// DefaultHold is the number of frames to keep the gate open after the
	// signal drops below threshold (200 ms at 20 ms / frame).
	DefaultHold = 10
)

// Gate is a hard noise gate that zeroes frames below a threshold.
type Gate struct {
	threshold float32
	hold      int // configured hold length in frames
	remaining int // frames left in current hold
	enabled   bool
	open      bool // true when the gate is currently passing audio
}

// New returns a Gate with DefaultThreshold and DefaultHold, enabled by default.
func New() *Gate {
	return &Gate{
		threshold: DefaultThreshold,
		hold:      DefaultHold,
		enabled:   true,
	}
}

// SetEnabled enables or disables the gate. When disabled, Process is a no-op.
func (g *Gate) SetEnabled(enabled bool) {
	g.enabled = enabled
	if !enabled {
		g.remaining = 0
		g.open = false
	}
}

// Enabled reports whether the gate is currently enabled.
func (g *Gate) Enabled() bool {
	return g.enabled
}

// SetThreshold sets the RMS gate threshold. level is in [0, 100] and maps
// to an RMS range of [0.001, 0.10]. Lower values open the gate more easily.
func (g *Gate) SetThreshold(level int) {
	if level < 0 {
		level = 0
	}
	if level > 100 {
		level = 100
	}
	// Map [0,100] -> [0.001, 0.10]
	g.threshold = 0.001 + float32(level)/100.0*0.099
}

// Threshold returns the current RMS threshold (linear amplitude).
func (g *Gate) Threshold() float32 {
	return g.threshold
}

// IsOpen reports whether the gate is currently passing audio.
func (g *Gate) IsOpen() bool {
	return g.open
}

// Process applies the gate to frame in-place. If the frame's RMS is below
// the threshold and the hold period has expired, the frame is zeroed.
// Returns the frame RMS before gating (useful for level meters).
func (g *Gate) Process(frame []float32) float32 {
	rms := vad.RMS(frame)

	if !g.enabled {
		g.open = true
		return rms
	}

	if rms >= g.threshold {
		g.remaining = g.hold
		g.open = true
		return rms
	}

	if g.remaining > 0 {
		g.remaining--
		g.open = true
		return rms
	}

	// Below threshold and hold expired: zero the frame.
	for i := range frame {
		frame[i] = 0
	}
	g.open = false
	return rms
}

// Reset clears the hold counter without changing settings.
func (g *Gate) Reset() {
	g.remaining = 0
	g.open = false
}
