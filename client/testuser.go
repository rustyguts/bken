package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"time"

	"gopkg.in/hraban/opus.v2"
)

const (
	testFreq      = 440.0 // Hz – A4, used when no audio file is provided
	testAmplitude = 0.3   // 30% to avoid clipping
	beepOnMs      = 600
	beepOffMs     = 400
)

// TestUser is a virtual peer that connects to the server as a named bot and
// continuously streams audio. If BKEN_TEST_AUDIO points to a 48 kHz mono
// 16-bit PCM WAV file, that file is looped; otherwise a 440 Hz beep pattern
// is generated synthetically.
type TestUser struct {
	transport    *Transport
	cancel       context.CancelFunc
	audioSamples []int16 // nil → use synthetic beep
}

func newTestUser() *TestUser {
	return &TestUser{transport: NewTransport()}
}

// start connects to addr under username, optionally loads a WAV file from the
// BKEN_TEST_AUDIO environment variable, then begins streaming audio.
func (tu *TestUser) start(addr, username string) error {
	if path := os.Getenv("BKEN_TEST_AUDIO"); path != "" {
		samples, err := loadWAV(path)
		if err != nil {
			log.Printf("[testuser] cannot load %s: %v – falling back to sine wave", path, err)
		} else {
			tu.audioSamples = samples
			log.Printf("[testuser] loaded %s (%d samples, %.1fs)", path, len(samples), float64(len(samples))/float64(sampleRate))
		}
	}

	if err := tu.transport.Connect(context.Background(), addr, username); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	tu.cancel = cancel
	go tu.toneLoop(ctx)
	return nil
}

// stop disconnects the test user cleanly.
func (tu *TestUser) stop() {
	if tu.cancel != nil {
		tu.cancel()
		tu.cancel = nil
	}
	tu.transport.Disconnect()
}

// toneLoop streams audio in 20 ms Opus frames at 50 fps.
// When audioSamples is set the file is looped continuously.
// Otherwise a 440 Hz beep (600 ms on / 400 ms off) is synthesised.
func (tu *TestUser) toneLoop(ctx context.Context) {
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		log.Printf("[testuser] encoder init: %v", err)
		return
	}
	enc.SetBitrate(opusBitrate)

	pcm := make([]int16, frameSize)
	opusBuf := make([]byte, 1024)

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	// WAV playback state.
	var wavPos int

	// Sine wave beep state.
	var phase float64
	cycleLen := time.Duration(beepOnMs+beepOffMs) * time.Millisecond
	beepOn := time.Duration(beepOnMs) * time.Millisecond
	start := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		if len(tu.audioSamples) > 0 {
			// Loop the WAV file.
			for i := range pcm {
				pcm[i] = tu.audioSamples[wavPos]
				wavPos = (wavPos + 1) % len(tu.audioSamples)
			}
		} else {
			// Synthetic beep pattern.
			if time.Since(start)%cycleLen < beepOn {
				for i := range pcm {
					s := testAmplitude * math.Sin(2*math.Pi*testFreq*phase/float64(sampleRate))
					pcm[i] = int16(s * 32767)
					phase++
				}
			} else {
				for i := range pcm {
					pcm[i] = 0
				}
				phase = 0 // reset to zero-crossing for the next beep
			}
		}

		n, err := enc.Encode(pcm, opusBuf)
		if err != nil {
			log.Printf("[testuser] encode: %v", err)
			continue
		}
		if err := tu.transport.SendAudio(opusBuf[:n]); err != nil {
			log.Printf("[testuser] send: %v", err)
			return
		}
	}
}

// loadWAV reads a WAV file and returns its samples as 16-bit signed PCM.
// The file must be 48 kHz, mono, 16-bit PCM (format tag 1).
// Use ffmpeg to convert: ffmpeg -i input.mp3 -ar 48000 -ac 1 output.wav
func loadWAV(path string) ([]int16, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// RIFF header: "RIFF" <size:4> "WAVE"
	var riff [4]byte
	if _, err := io.ReadFull(f, riff[:]); err != nil {
		return nil, fmt.Errorf("read RIFF: %w", err)
	}
	if string(riff[:]) != "RIFF" {
		return nil, fmt.Errorf("not a RIFF file")
	}
	var chunkSize uint32
	binary.Read(f, binary.LittleEndian, &chunkSize)
	var wave [4]byte
	if _, err := io.ReadFull(f, wave[:]); err != nil {
		return nil, fmt.Errorf("read WAVE: %w", err)
	}
	if string(wave[:]) != "WAVE" {
		return nil, fmt.Errorf("not a WAVE file")
	}

	// Iterate sub-chunks until we have both fmt and data.
	var (
		audioFormat   uint16
		numChannels   uint16
		sampleRateHz  uint32
		bitsPerSample uint16
		fmtFound      bool
		samples       []int16
	)

	for {
		var id [4]byte
		if _, err := io.ReadFull(f, id[:]); err != nil {
			break // EOF or truncated
		}
		var size uint32
		if err := binary.Read(f, binary.LittleEndian, &size); err != nil {
			break
		}

		switch string(id[:]) {
		case "fmt ":
			binary.Read(f, binary.LittleEndian, &audioFormat)
			binary.Read(f, binary.LittleEndian, &numChannels)
			binary.Read(f, binary.LittleEndian, &sampleRateHz)
			var byteRate uint32
			binary.Read(f, binary.LittleEndian, &byteRate)
			var blockAlign uint16
			binary.Read(f, binary.LittleEndian, &blockAlign)
			binary.Read(f, binary.LittleEndian, &bitsPerSample)
			// Skip any extra fmt bytes (e.g. extensible WAV).
			if size > 16 {
				io.CopyN(io.Discard, f, int64(size-16))
			}
			fmtFound = true

		case "data":
			if !fmtFound {
				return nil, fmt.Errorf("data chunk before fmt chunk")
			}
			if audioFormat != 1 {
				return nil, fmt.Errorf("WAV must be PCM (format 1, got %d)", audioFormat)
			}
			if numChannels != 1 {
				return nil, fmt.Errorf("WAV must be mono (got %d channels)", numChannels)
			}
			if sampleRateHz != uint32(sampleRate) {
				return nil, fmt.Errorf("WAV must be %d Hz (got %d Hz)", sampleRate, sampleRateHz)
			}
			if bitsPerSample != 16 {
				return nil, fmt.Errorf("WAV must be 16-bit (got %d-bit)", bitsPerSample)
			}
			samples = make([]int16, size/2)
			if err := binary.Read(f, binary.LittleEndian, samples); err != nil {
				return nil, fmt.Errorf("read samples: %w", err)
			}
			return samples, nil

		default:
			// Skip unknown chunks; pad to even byte boundary.
			skip := int64(size)
			if size%2 != 0 {
				skip++
			}
			io.CopyN(io.Discard, f, skip)
		}

		// Apply padding after fmt chunk too.
		if string(id[:]) == "fmt " && size%2 != 0 {
			io.CopyN(io.Discard, f, 1)
		}
	}

	return nil, fmt.Errorf("no data chunk found")
}
