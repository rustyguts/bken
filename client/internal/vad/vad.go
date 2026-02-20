// Package vad implements a simple energy-based Voice Activity Detector for
// mono float32 PCM audio at 48 kHz, 960-sample (20 ms) frames.
//
// The detector classifies each frame as speech or silence by comparing the
// frame RMS level against a threshold. A configurable "hangover" counter keeps
// the detector in the active (send) state for a fixed number of frames after
// the last speech frame, preventing abrupt cut-offs mid-word or between words.
package vad

import "math"

const (
	// DefaultThreshold is the RMS level below which a frame is treated as silence
	// (~-46 dBFS). Low enough to pass quiet speech, high enough to suppress
	// background hum and open-mic noise.
	DefaultThreshold = float32(0.005)

	// DefaultHangover is the number of silent frames to keep sending after
	// speech ends (~400 ms at 20 ms / frame). Prevents clipping word endings.
	DefaultHangover = 20
)

// VAD is a single-channel voice activity detector. Zero value is not usable;
// use New().
type VAD struct {
	threshold float32
	hangover  int // configured hangover length in frames
	remaining int // frames left in current hangover
	enabled   bool
}

// New returns a VAD with DefaultThreshold and DefaultHangover, enabled by default.
func New() *VAD {
	return &VAD{
		threshold: DefaultThreshold,
		hangover:  DefaultHangover,
		enabled:   true,
	}
}

// SetEnabled enables or disables the VAD. When disabled, ShouldSend always
// returns true (pass-through mode).
func (v *VAD) SetEnabled(enabled bool) {
	v.enabled = enabled
	if !enabled {
		v.remaining = 0
	}
}

// SetThreshold sets the RMS silence threshold. level is in [0, 100] and maps
// to an RMS range of [0.001, 0.05] (linear amplitude). Lower values are more
// sensitive (detect quieter speech); higher values suppress more.
func (v *VAD) SetThreshold(level int) {
	if level < 0 {
		level = 0
	}
	if level > 100 {
		level = 100
	}
	// Map [0,100] → [0.001, 0.05]
	v.threshold = 0.001 + float32(level)/100.0*0.049
}

// ShouldSend reports whether the frame with the given RMS energy should be
// transmitted. Updates internal hangover state.
func (v *VAD) ShouldSend(rms float32) bool {
	if !v.enabled {
		return true
	}
	if rms > v.threshold {
		v.remaining = v.hangover // speech — reset hangover
		return true
	}
	if v.remaining > 0 {
		v.remaining-- // in hangover — still send
		return true
	}
	return false // pure silence
}

// ShouldSendProb is like ShouldSend but takes a voice probability (0.0–1.0)
// instead of RMS energy. Used with ML-based VAD signals such as RNNoise,
// which provide more accurate speech/noise classification than energy
// thresholds. A probability above 0.5 is treated as speech.
func (v *VAD) ShouldSendProb(prob float32) bool {
	if !v.enabled {
		return true
	}
	if prob > 0.5 {
		v.remaining = v.hangover // speech — reset hangover
		return true
	}
	if v.remaining > 0 {
		v.remaining-- // in hangover — still send
		return true
	}
	return false // noise
}

// Enabled reports whether the VAD is currently enabled.
func (v *VAD) Enabled() bool {
	return v.enabled
}

// Reset clears the hangover counter without changing other settings.
func (v *VAD) Reset() {
	v.remaining = 0
}

// RMS returns the root-mean-square of a float32 PCM frame.
func RMS(frame []float32) float32 {
	if len(frame) == 0 {
		return 0
	}
	var sum float64
	for _, s := range frame {
		sum += float64(s) * float64(s)
	}
	return float32(math.Sqrt(sum / float64(len(frame))))
}
