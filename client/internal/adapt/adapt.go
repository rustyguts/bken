// Package adapt provides adaptive Opus bitrate selection based on connection
// quality metrics (packet loss rate and round-trip time).
package adapt

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
