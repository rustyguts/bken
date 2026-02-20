package adapt

import "testing"

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
