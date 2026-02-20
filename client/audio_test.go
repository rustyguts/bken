package main

import (
	"math"
	"testing"

	"gopkg.in/hraban/opus.v2"
)

func TestOpusEncodeDecodeRoundTrip(t *testing.T) {
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		t.Fatalf("new encoder: %v", err)
	}
	enc.SetBitrate(opusBitrate)

	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		t.Fatalf("new decoder: %v", err)
	}

	// Generate a 440Hz sine wave (20ms frame).
	pcmIn := make([]int16, frameSize)
	for i := range pcmIn {
		pcmIn[i] = int16(math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate)) * 16000)
	}

	// Encode.
	opusBuf := make([]byte, 1024)
	n, err := enc.Encode(pcmIn, opusBuf)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if n == 0 {
		t.Fatal("encoded 0 bytes")
	}

	encoded := opusBuf[:n]
	t.Logf("encoded %d samples to %d bytes (%.1f kbps)", frameSize, n, float64(n)*8*50/1000)

	// Decode.
	pcmOut := make([]int16, frameSize)
	samplesDecoded, err := dec.Decode(encoded, pcmOut)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if samplesDecoded != frameSize {
		t.Errorf("expected %d decoded samples, got %d", frameSize, samplesDecoded)
	}

	// Verify the decoded signal is reasonable (not silence).
	var maxAmp int16
	for _, s := range pcmOut {
		if s > maxAmp {
			maxAmp = s
		}
		if -s > maxAmp {
			maxAmp = -s
		}
	}

	if maxAmp < 1000 {
		t.Errorf("decoded signal too quiet: max amplitude %d", maxAmp)
	}
}

func TestOpusMultipleFrames(t *testing.T) {
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		t.Fatalf("new encoder: %v", err)
	}
	enc.SetBitrate(opusBitrate)

	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		t.Fatalf("new decoder: %v", err)
	}

	// Encode and decode 10 frames.
	for frame := 0; frame < 10; frame++ {
		pcm := make([]int16, frameSize)
		for i := range pcm {
			pcm[i] = int16(math.Sin(2*math.Pi*440*float64(i+frame*frameSize)/float64(sampleRate)) * 16000)
		}

		buf := make([]byte, 1024)
		n, err := enc.Encode(pcm, buf)
		if err != nil {
			t.Fatalf("frame %d encode: %v", frame, err)
		}

		out := make([]int16, frameSize)
		_, err = dec.Decode(buf[:n], out)
		if err != nil {
			t.Fatalf("frame %d decode: %v", frame, err)
		}
	}
}
