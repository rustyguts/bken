// Package scoring ports cli/src/bken/density.py + scoring.py to Go.
// No external math deps beyond stdlib.
package scoring

import "sort"

// Segment is a scored interval in video seconds. Shared by density + sliding.
type Segment struct {
	Start    float64
	End      float64
	Score    float64
	Features map[string]any
}

// iou returns intersection-over-union of two closed intervals on the number line.
func iou(a1, a2, b1, b2 float64) float64 {
	s := a1
	if b1 > s {
		s = b1
	}
	e := a2
	if b2 < e {
		e = b2
	}
	overlap := e - s
	if overlap < 0 {
		overlap = 0
	}
	union := (a2 - a1) + (b2 - b1) - overlap
	if union <= 0 {
		return 0
	}
	return overlap / union
}

// nms drops overlapping segments by IoU, keeping higher-scored survivors first.
func nms(segments []Segment, threshold float64) []Segment {
	if len(segments) == 0 {
		return nil
	}
	ranked := make([]Segment, len(segments))
	copy(ranked, segments)
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].Score > ranked[j].Score
	})
	kept := make([]Segment, 0, len(ranked))
	for _, r := range ranked {
		keep := true
		for _, k := range kept {
			if iou(r.Start, r.End, k.Start, k.End) > threshold {
				keep = false
				break
			}
		}
		if keep {
			kept = append(kept, r)
		}
	}
	return kept
}

// percentile returns the linear-interpolated percentile of xs. xs is sorted in place.
// pct is in [0, 100].
func percentile(xs []float64, pct float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sort.Float64s(xs)
	if len(xs) == 1 {
		return xs[0]
	}
	if pct <= 0 {
		return xs[0]
	}
	if pct >= 100 {
		return xs[len(xs)-1]
	}
	// numpy default: linear interpolation on rank = pct/100 * (n-1).
	rank := pct / 100.0 * float64(len(xs)-1)
	lo := int(rank)
	hi := lo + 1
	if hi >= len(xs) {
		return xs[len(xs)-1]
	}
	frac := rank - float64(lo)
	return xs[lo] + frac*(xs[hi]-xs[lo])
}

// percentilePos mirrors density.py _percentile_pos: percentile of strictly
// positive samples. Returns +Inf when signal is all zero so callers bail
// cleanly without forming regions.
func percentilePos(I []float32, pct float64) float64 {
	nz := make([]float64, 0, len(I))
	for _, v := range I {
		if v > 0 {
			nz = append(nz, float64(v))
		}
	}
	if len(nz) == 0 {
		// Sentinel: no finite percentile, hysteresis bails.
		return posInf
	}
	return percentile(nz, pct)
}
