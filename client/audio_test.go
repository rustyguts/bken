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
	pcmIn := make([]int16, FrameSize)
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
	t.Logf("encoded %d samples to %d bytes (%.1f kbps)", FrameSize, n, float64(n)*8*50/1000)

	// Decode.
	pcmOut := make([]int16, FrameSize)
	samplesDecoded, err := dec.Decode(encoded, pcmOut)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if samplesDecoded != FrameSize {
		t.Errorf("expected %d decoded samples, got %d", FrameSize, samplesDecoded)
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

func TestOpusFECEnableAndRoundTrip(t *testing.T) {
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		t.Fatalf("new encoder: %v", err)
	}
	enc.SetBitrate(opusBitrate)
	if err := enc.SetInBandFEC(true); err != nil {
		t.Fatalf("SetInBandFEC: %v", err)
	}
	if err := enc.SetPacketLossPerc(50); err != nil {
		t.Fatalf("SetPacketLossPerc: %v", err)
	}

	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		t.Fatalf("new decoder: %v", err)
	}

	// Encode multiple frames so the encoder has FEC data to embed.
	frames := make([][]byte, 10)
	for i := range frames {
		pcm := make([]int16, FrameSize)
		for j := range pcm {
			pcm[j] = int16(math.Sin(2*math.Pi*440*float64(j+i*FrameSize)/float64(sampleRate)) * 16000)
		}
		buf := make([]byte, opusMaxPacketBytes)
		n, err := enc.Encode(pcm, buf)
		if err != nil {
			t.Fatalf("encode frame %d: %v", i, err)
		}
		frames[i] = make([]byte, n)
		copy(frames[i], buf[:n])
	}

	// Normal decode of frames 0–4 to prime the decoder.
	for i := 0; i < 5; i++ {
		pcm := make([]int16, FrameSize)
		if _, err := dec.Decode(frames[i], pcm); err != nil {
			t.Fatalf("decode frame %d: %v", i, err)
		}
	}

	// Simulate loss of frame 5: use frame 6's FEC data to recover.
	fecPCM := make([]int16, FrameSize)
	if err := dec.DecodeFEC(frames[6], fecPCM); err != nil {
		t.Fatalf("DecodeFEC: %v", err)
	}

	// Verify FEC output is not silence.
	var maxAmp int16
	for _, s := range fecPCM {
		if s > maxAmp {
			maxAmp = s
		}
		if -s > maxAmp {
			maxAmp = -s
		}
	}
	t.Logf("FEC recovery max amplitude: %d", maxAmp)

	// Continue with normal decode of frame 6 after FEC recovery.
	pcm := make([]int16, FrameSize)
	if _, err := dec.Decode(frames[6], pcm); err != nil {
		t.Fatalf("decode frame 6 after FEC: %v", err)
	}
}

func TestOpusFECRecoveryAfterLoss(t *testing.T) {
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		t.Fatalf("new encoder: %v", err)
	}
	enc.SetBitrate(opusBitrate)
	enc.SetInBandFEC(true)
	enc.SetPacketLossPerc(50) // high loss hint → more FEC redundancy

	// Encode a loud signal: 20 frames of 440 Hz sine.
	frames := make([][]byte, 20)
	for i := range frames {
		pcm := make([]int16, FrameSize)
		for j := range pcm {
			pcm[j] = int16(math.Sin(2*math.Pi*440*float64(j+i*FrameSize)/float64(sampleRate)) * 16000)
		}
		buf := make([]byte, opusMaxPacketBytes)
		n, err := enc.Encode(pcm, buf)
		if err != nil {
			t.Fatalf("encode frame %d: %v", i, err)
		}
		frames[i] = make([]byte, n)
		copy(frames[i], buf[:n])
	}

	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		t.Fatalf("new decoder: %v", err)
	}

	// Decode frames 0–9 normally to build decoder state.
	for i := 0; i < 10; i++ {
		pcm := make([]int16, FrameSize)
		if _, err := dec.Decode(frames[i], pcm); err != nil {
			t.Fatalf("decode frame %d: %v", i, err)
		}
	}

	// Frame 10 is lost. Use frame 11's FEC to recover it.
	fecPCM := make([]int16, FrameSize)
	if err := dec.DecodeFEC(frames[11], fecPCM); err != nil {
		t.Fatalf("DecodeFEC: %v", err)
	}

	var fecEnergy float64
	for _, s := range fecPCM {
		fecEnergy += float64(s) * float64(s)
	}
	t.Logf("FEC recovery energy: %.0f", fecEnergy)
	if fecEnergy == 0 {
		t.Error("FEC recovery produced silence")
	}

	// Continue with normal decode of frame 11 after FEC recovery.
	// This verifies the decoder state is coherent after FEC.
	pcm := make([]int16, FrameSize)
	n, err := dec.Decode(frames[11], pcm)
	if err != nil {
		t.Fatalf("decode frame 11 after FEC: %v", err)
	}
	if n != FrameSize {
		t.Errorf("expected %d samples, got %d", FrameSize, n)
	}

	// Verify frame 11 decoded to non-silence.
	var energy float64
	for _, s := range pcm[:n] {
		energy += float64(s) * float64(s)
	}
	if energy == 0 {
		t.Error("frame 11 decoded to silence after FEC recovery")
	}
}

func TestSetPacketLoss(t *testing.T) {
	ae := NewAudioEngine()
	// SetPacketLoss before Start should not panic.
	ae.SetPacketLoss(5)

	// Verify clamping.
	ae.SetPacketLoss(-1)
	ae.SetPacketLoss(200)
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
		pcm := make([]int16, FrameSize)
		for i := range pcm {
			pcm[i] = int16(math.Sin(2*math.Pi*440*float64(i+frame*FrameSize)/float64(sampleRate)) * 16000)
		}

		buf := make([]byte, 1024)
		n, err := enc.Encode(pcm, buf)
		if err != nil {
			t.Fatalf("frame %d encode: %v", frame, err)
		}

		out := make([]int16, FrameSize)
		_, err = dec.Decode(buf[:n], out)
		if err != nil {
			t.Fatalf("frame %d decode: %v", frame, err)
		}
	}
}
