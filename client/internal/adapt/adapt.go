// Package adapt provides adaptive Opus bitrate selection and jitter buffer
// depth tuning based on connection quality metrics.
package adapt

import "math"

// Ladder is the ordered list of Opus target bitrate steps in kbps.
// The range covers from barely-intelligible emergency quality (8 kbps)
// up to high-fidelity voice (48 kbps).
var Ladder = []int{8, 12, 16, 24, 32, 48}

// DefaultKbps is the starting bitrate for a new connection.
const DefaultKbps = 32

// NextBitrate returns the next Opus target bitrate (kbps) to use, given the
// current encoder setting and the connection quality observed over the last
// measurement interval.
//
// Adaptation rules:
//   - Step DOWN one rung when packet loss exceeds 5%.
//   - Step UP  one rung when loss < 1% and RTT > 0 and RTT < 150 ms.
//     (RTT == 0 means no measurement yet; hold rather than assume a great link.)
//   - Otherwise HOLD the current rung.
//
// The function always returns a value that is in Ladder.
func NextBitrate(current int, lossRate float64, rttMs float64) int {
	idx := stepIndex(current)
	switch {
	case lossRate > 0.05 && idx > 0:
		return Ladder[idx-1]
	case lossRate < 0.01 && rttMs > 0 && rttMs < 150 && idx < len(Ladder)-1:
		return Ladder[idx+1]
	default:
		return Ladder[idx]
	}
}

// stepIndex returns the index of the Ladder rung closest to kbps.
func stepIndex(kbps int) int {
	best, bestDist := 0, iabs(kbps-Ladder[0])
	for i, step := range Ladder {
		if d := iabs(kbps - step); d < bestDist {
			bestDist, best = d, i
		}
	}
	return best
}

func iabs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// DefaultJitterDepth is the jitter buffer depth used when no jitter data is
// available (e.g. before any packets are received). 1 frame = 20 ms — optimistic
// for LAN where jitter is typically <5 ms. The adaptive loop will increase
// depth within seconds if network conditions warrant it.
const DefaultJitterDepth = 1

const (
	frameDurationMs = 20.0 // one Opus frame = 20 ms
	minDepth        = 1
	maxDepth        = 8
)

// TargetJitterDepth computes the optimal jitter buffer depth (in 20 ms frames)
// from the measured inter-arrival jitter (ms) and loss rate (0.0–1.0).
//
// Depth = ceil(jitterMs / 20) + 1, with a +1 bonus when loss > 5%.
// Returns DefaultJitterDepth when jitterMs is 0 (no measurement).
// Result is clamped to [1, 8].
func TargetJitterDepth(jitterMs float64, lossRate float64) int {
	if jitterMs <= 0 {
		return DefaultJitterDepth
	}
	depth := int(math.Ceil(jitterMs/frameDurationMs)) + 1
	if lossRate > 0.05 {
		depth++
	}
	if depth < minDepth {
		depth = minDepth
	}
	if depth > maxDepth {
		depth = maxDepth
	}
	return depth
}

// SmoothLoss applies exponentially weighted moving average smoothing to a
// raw packet loss measurement. alpha controls the weight of the new sample
// (0.0 = ignore new, 1.0 = ignore old). Typical alpha: 0.3.
func SmoothLoss(smoothed, raw, alpha float64) float64 {
	return alpha*raw + (1-alpha)*smoothed
}
