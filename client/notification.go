package main

import (
	"math"
)

// NotificationSound identifies a UI audio cue.
type NotificationSound int

const (
	SoundConnect   NotificationSound = iota // ascending two-tone: C5 → G5
	SoundDisconnect                         // descending two-tone: G5 → C5
	SoundUserJoined                         // single high ping: A5
	SoundUserLeft                           // single low ping: A4
	SoundMute                               // descending tone: C5 → A4
	SoundUnmute                             // ascending tone: A4 → C5
)

// notifVolume is the peak amplitude of notification tones in the [-1, 1] range.
const notifVolume = 0.18

// PlayNotification enqueues synthesised PCM frames for sound onto notifCh.
// It runs asynchronously and drops frames if the channel is full so it never
// blocks the caller. The goroutine exits when the audio engine stops.
func (ae *AudioEngine) PlayNotification(sound NotificationSound) {
	frames := generateNotificationFrames(sound)
	if len(frames) == 0 {
		return
	}
	go func() {
		stopCh := ae.stopCh
		for _, frame := range frames {
			select {
			case <-stopCh:
				return
			case ae.notifCh <- frame:
			default:
				// Channel full — skip frame rather than block.
			}
		}
	}()
}

// generateNotificationFrames returns a slice of frameSize float32 PCM frames
// for the requested sound.
func generateNotificationFrames(sound NotificationSound) [][]float32 {
	type tone struct {
		freq int // Hz
		dur  int // ms
	}
	var tones []tone
	switch sound {
	case SoundConnect:
		tones = []tone{{523, 80}, {784, 120}} // C5, G5
	case SoundDisconnect:
		tones = []tone{{784, 80}, {523, 120}} // G5, C5
	case SoundUserJoined:
		tones = []tone{{880, 120}} // A5
	case SoundUserLeft:
		tones = []tone{{440, 120}} // A4
	case SoundMute:
		tones = []tone{{523, 80}, {440, 100}} // C5 → A4
	case SoundUnmute:
		tones = []tone{{440, 80}, {523, 100}} // A4 → C5
	default:
		return nil
	}

	var frames [][]float32
	for _, t := range tones {
		frames = append(frames, generateSineTone(float64(t.freq), t.dur)...)
	}
	return frames
}

// generateSineTone generates PCM frames for a sine tone at freq Hz lasting
// durationMs milliseconds. The signal uses a linear fade-in and fade-out
// envelope (5 ms each) to avoid clicks. The output is chunked into frameSize
// slices ready to push onto notifCh.
func generateSineTone(freq float64, durationMs int) [][]float32 {
	totalSamples := sampleRate * durationMs / 1000
	raw := make([]float32, totalSamples)

	fadeLen := sampleRate * 5 / 1000 // 5 ms fade
	if fadeLen > totalSamples/2 {
		fadeLen = totalSamples / 2
	}

	for i := range raw {
		t := float64(i) / float64(sampleRate)
		s := float32(math.Sin(2 * math.Pi * freq * t))

		// Linear envelope.
		var env float32 = 1.0
		if i < fadeLen {
			env = float32(i) / float32(fadeLen)
		} else if i >= totalSamples-fadeLen {
			env = float32(totalSamples-1-i) / float32(fadeLen)
		}
		raw[i] = s * env * notifVolume
	}

	// Chunk into frameSize slices.
	var frames [][]float32
	for off := 0; off < len(raw); off += frameSize {
		end := off + frameSize
		if end > len(raw) {
			// Pad final partial frame with silence.
			frame := make([]float32, frameSize)
			copy(frame, raw[off:])
			frames = append(frames, frame)
		} else {
			frame := make([]float32, frameSize)
			copy(frame, raw[off:end])
			frames = append(frames, frame)
		}
	}
	return frames
}
