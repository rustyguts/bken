package adapt

import (
	"math"
	"testing"
)

func TestNextBitrateStepsDown(t *testing.T) {
	// High packet loss should lower the bitrate.
	got := NextBitrate(32, 0.10, 50)
	want := 24
	if got != want {
		t.Errorf("high loss: NextBitrate(32, 0.10, 50) = %d, want %d", got, want)
	}
}

func TestNextBitrateStepsUp(t *testing.T) {
	// Good conditions: low loss, low RTT, and RTT is non-zero.
	got := NextBitrate(32, 0.00, 20)
	want := 48
	if got != want {
		t.Errorf("good conditions: NextBitrate(32, 0.00, 20) = %d, want %d", got, want)
	}
}

func TestNextBitrateHoldsOnZeroRTT(t *testing.T) {
	// RTT == 0 means no measurement yet; must not step up.
	got := NextBitrate(32, 0.00, 0)
	if got != 32 {
		t.Errorf("zero RTT: NextBitrate(32, 0.00, 0) = %d, want 32 (hold)", got)
	}
}

func TestNextBitrateHoldsOnHighRTT(t *testing.T) {
	// Low loss but high RTT: hold.
	got := NextBitrate(32, 0.00, 200)
	if got != 32 {
		t.Errorf("high RTT: NextBitrate(32, 0.00, 200) = %d, want 32 (hold)", got)
	}
}

func TestNextBitrateHoldsOnModerateLoss(t *testing.T) {
	// Loss between thresholds: hold.
	got := NextBitrate(32, 0.03, 50)
	if got != 32 {
		t.Errorf("moderate loss: NextBitrate(32, 0.03, 50) = %d, want 32 (hold)", got)
	}
}

func TestNextBitrateCannotExceedMax(t *testing.T) {
	top := Ladder[len(Ladder)-1]
	got := NextBitrate(top, 0.00, 10)
	if got != top {
		t.Errorf("at max rung: NextBitrate(%d, 0, 10) = %d, want %d", top, got, top)
	}
}

func TestNextBitrateCannotGoBelowMin(t *testing.T) {
	bottom := Ladder[0]
	got := NextBitrate(bottom, 0.99, 500)
	if got != bottom {
		t.Errorf("at min rung: NextBitrate(%d, 0.99, 500) = %d, want %d", bottom, got, bottom)
	}
}

func TestNextBitrateUnknownValueSnapsToClosestRung(t *testing.T) {
	// 20 kbps is equidistant between 16 and 24; the lower rung wins (16).
	// High loss then steps down one more → 12.
	got := NextBitrate(20, 0.10, 50)
	want := 12
	if got != want {
		t.Errorf("snap+step: NextBitrate(20, 0.10, 50) = %d, want %d", got, want)
	}
}

func TestStepIndex(t *testing.T) {
	for i, step := range Ladder {
		if got := stepIndex(step); got != i {
			t.Errorf("stepIndex(%d) = %d, want %d", step, got, i)
		}
	}
}

// --- TargetJitterDepth tests ---

func TestTargetJitterDepthNoData(t *testing.T) {
	got := TargetJitterDepth(0, 0)
	if got != DefaultJitterDepth {
		t.Errorf("no jitter data: got %d, want %d", got, DefaultJitterDepth)
	}
}

func TestTargetJitterDepthLowJitter(t *testing.T) {
	// 0.5 ms jitter → ceil(0.5/20) + 1 = 1 + 1 = 2
	got := TargetJitterDepth(0.5, 0)
	if got != 2 {
		t.Errorf("low jitter: got %d, want 2", got)
	}
}

func TestTargetJitterDepthModerateJitter(t *testing.T) {
	// 30 ms jitter → ceil(30/20) + 1 = 2 + 1 = 3
	got := TargetJitterDepth(30, 0)
	if got != 3 {
		t.Errorf("moderate jitter: got %d, want 3", got)
	}
}

func TestTargetJitterDepthHighJitter(t *testing.T) {
	// 80 ms jitter → ceil(80/20) + 1 = 4 + 1 = 5
	got := TargetJitterDepth(80, 0)
	if got != 5 {
		t.Errorf("high jitter: got %d, want 5", got)
	}
}

func TestTargetJitterDepthLossBonus(t *testing.T) {
	// 30 ms jitter + 10% loss → ceil(30/20) + 1 + 1 = 4
	got := TargetJitterDepth(30, 0.10)
	if got != 4 {
		t.Errorf("jitter + loss: got %d, want 4", got)
	}
}

func TestTargetJitterDepthMaxClamp(t *testing.T) {
	// 200 ms jitter → ceil(200/20) + 1 = 11 → clamped to 8
	got := TargetJitterDepth(200, 0)
	if got != 8 {
		t.Errorf("max clamp: got %d, want 8", got)
	}
}

func TestTargetJitterDepthNegativeJitter(t *testing.T) {
	got := TargetJitterDepth(-5, 0)
	if got != DefaultJitterDepth {
		t.Errorf("negative jitter: got %d, want %d", got, DefaultJitterDepth)
	}
}

// --- SmoothLoss tests ---

func TestSmoothLossFromZero(t *testing.T) {
	got := SmoothLoss(0, 0.10, 0.3)
	want := 0.03
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("from zero: got %f, want %f", got, want)
	}
}

func TestSmoothLossConverges(t *testing.T) {
	// Starting from 0, feeding constant 10% loss, should converge towards 0.10.
	smoothed := 0.0
	for i := 0; i < 50; i++ {
		smoothed = SmoothLoss(smoothed, 0.10, 0.3)
	}
	if math.Abs(smoothed-0.10) > 0.001 {
		t.Errorf("after 50 iterations: got %f, want ~0.10", smoothed)
	}
}

func TestSmoothLossSpikeDampened(t *testing.T) {
	// Stable at 0, one 50% spike should not immediately jump.
	smoothed := SmoothLoss(0, 0.50, 0.3)
	if smoothed > 0.20 {
		t.Errorf("spike should be dampened: got %f, want < 0.20", smoothed)
	}
}

// --- LAN-optimistic default tests ---

func TestDefaultJitterDepthLANOptimistic(t *testing.T) {
	// bken is a LAN voice app — default should be 1 frame (20 ms) for
	// minimal latency. The adaptive loop increases depth if needed.
	if DefaultJitterDepth != 1 {
		t.Errorf("DefaultJitterDepth = %d, want 1 (optimistic for LAN)", DefaultJitterDepth)
	}
}

func TestTargetJitterDepthLowJitterConvergesToMinimal(t *testing.T) {
	// On a LAN with 2 ms jitter and no loss, depth should be 2 (ceil(2/20)+1).
	// This is close to the optimistic default and validates that the adaptive
	// system won't increase depth unnecessarily on good networks.
	got := TargetJitterDepth(2.0, 0)
	if got != 2 {
		t.Errorf("LAN jitter (2ms): got depth %d, want 2", got)
	}
}

func TestSmoothLossWarmupConvergesFaster(t *testing.T) {
	// Warmup alpha (0.5) should converge faster than steady-state alpha (0.3).
	warmupSmoothed := 0.0
	steadySmoothed := 0.0
	target := 0.05

	for i := 0; i < 3; i++ {
		warmupSmoothed = SmoothLoss(warmupSmoothed, target, 0.5)
		steadySmoothed = SmoothLoss(steadySmoothed, target, 0.3)
	}

	warmupError := math.Abs(warmupSmoothed - target)
	steadyError := math.Abs(steadySmoothed - target)
	if warmupError >= steadyError {
		t.Errorf("warmup α=0.5 should converge faster than α=0.3: warmup=%.4f steady=%.4f target=%.4f",
			warmupSmoothed, steadySmoothed, target)
	}
}
