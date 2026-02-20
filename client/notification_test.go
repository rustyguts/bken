package main

import (
	"math"
	"testing"
)

func TestGenerateSineToneFrameCount(t *testing.T) {
	durationMs := 100
	frames := generateSineTone(440, durationMs)
	totalSamples := sampleRate * durationMs / 1000
	// Ceiling division: how many FrameSize chunks do we need?
	wantFrames := (totalSamples + FrameSize - 1) / FrameSize
	if len(frames) != wantFrames {
		t.Errorf("frame count: got %d, want %d", len(frames), wantFrames)
	}
	for i, f := range frames {
		if len(f) != FrameSize {
			t.Errorf("frame %d length: got %d, want %d", i, len(f), FrameSize)
		}
	}
}

func TestGenerateSineToneAmplitude(t *testing.T) {
	frames := generateSineTone(440, 100)
	var maxAmp float32
	for _, f := range frames {
		for _, s := range f {
			if a := float32(math.Abs(float64(s))); a > maxAmp {
				maxAmp = a
			}
		}
	}
	if maxAmp > 1.0 {
		t.Errorf("amplitude clipped: max %f", maxAmp)
	}
	if maxAmp > notifVolume+0.01 {
		t.Errorf("amplitude exceeds notifVolume: max %f, notifVolume %f", maxAmp, notifVolume)
	}
	if maxAmp < notifVolume*0.5 {
		t.Errorf("amplitude too low: max %f, expected ~%f", maxAmp, notifVolume)
	}
}

func TestGenerateSineToneFadeEnds(t *testing.T) {
	// First and last samples should be near zero due to fade envelope.
	frames := generateSineTone(440, 100)
	if len(frames) == 0 {
		t.Fatal("no frames generated")
	}
	first := frames[0][0]
	if math.Abs(float64(first)) > 0.01 {
		t.Errorf("first sample not near zero (got %f): fade-in not applied", first)
	}
	last := frames[len(frames)-1]
	// Trailing samples in the last frame may be padded silence.
	lastNonZero := float32(0)
	durationMs := 100
	totalSamples := sampleRate * durationMs / 1000
	lastRealFrame := (totalSamples - 1) / FrameSize
	lastRealOffset := (totalSamples - 1) % FrameSize
	if lastRealFrame < len(frames) {
		lastNonZero = frames[lastRealFrame][lastRealOffset]
	} else {
		lastNonZero = last[len(last)-1]
	}
	if math.Abs(float64(lastNonZero)) > 0.01 {
		t.Errorf("last real sample not near zero (got %f): fade-out not applied", lastNonZero)
	}
}

func TestGenerateNotificationFramesAllSounds(t *testing.T) {
	sounds := []NotificationSound{
		SoundConnect,
		SoundDisconnect,
		SoundUserJoined,
		SoundUserLeft,
		SoundMute,
		SoundUnmute,
	}
	for _, s := range sounds {
		frames := generateNotificationFrames(s)
		if len(frames) == 0 {
			t.Errorf("sound %d: no frames generated", s)
			continue
		}
		for i, f := range frames {
			if len(f) != FrameSize {
				t.Errorf("sound %d frame %d: length %d, want %d", s, i, len(f), FrameSize)
			}
		}
	}
}

func TestGenerateNotificationFramesUnknownSound(t *testing.T) {
	frames := generateNotificationFrames(NotificationSound(99))
	if frames != nil {
		t.Errorf("unknown sound should return nil, got %d frames", len(frames))
	}
}
