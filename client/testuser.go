package main

import (
	"context"
	"log"
	"math"
	"time"

	"gopkg.in/hraban/opus.v2"
)

const (
	testFreq      = 440.0 // Hz – A4
	testAmplitude = 0.3   // 30% to avoid clipping
	beepOnMs      = 600
	beepOffMs     = 400
)

// TestUser is a virtual peer that connects to the server as a named bot and
// continuously sends a periodic 440 Hz tone. It reuses the existing Transport
// and Opus encoder so no server-side changes are required.
type TestUser struct {
	transport *Transport
	cancel    context.CancelFunc
}

func newTestUser() *TestUser {
	return &TestUser{transport: NewTransport()}
}

// start connects to addr under username and begins the tone loop.
func (tu *TestUser) start(addr, username string) error {
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

// toneLoop generates 20 ms Opus frames: 600 ms of 440 Hz sine wave followed
// by 400 ms of silence, repeating indefinitely.
func (tu *TestUser) toneLoop(ctx context.Context) {
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		log.Printf("[testuser] encoder init: %v", err)
		return
	}
	enc.SetBitrate(opusBitrate)

	pcm := make([]int16, frameSize)
	opusBuf := make([]byte, 1024)

	cycleLen := time.Duration(beepOnMs+beepOffMs) * time.Millisecond
	beepOn := time.Duration(beepOnMs) * time.Millisecond

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	var phase float64
	start := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

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
