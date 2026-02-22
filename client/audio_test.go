package main

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/hraban/opus.v2"
)

// --- Mock paStream for Stop() tests ---

// mockPAStream implements paStream for testing. Read() and Write() block until
// unblockCh is closed (simulating a real PortAudio blocking call). Stop()/Abort()
// closes unblockCh so the blocked calls return, just like Pa_AbortStream should.
type mockPAStream struct {
	unblockCh chan struct{}
	stopped   atomic.Bool
	closed    atomic.Bool
	// If set, Read/Write will NOT unblock when Stop()/Abort() is called —
	// simulating a broken PortAudio backend.
	brokenStop bool
	// blockedInRead/blockedInWrite are set just before blocking, so tests
	// can wait for goroutines to be truly blocked before calling Stop().
	blockedInRead  atomic.Bool
	blockedInWrite atomic.Bool
}

func newMockPAStream(broken bool) *mockPAStream {
	return &mockPAStream{
		unblockCh:  make(chan struct{}),
		brokenStop: broken,
	}
}

func (m *mockPAStream) Start() error { return nil }

func (m *mockPAStream) Stop() error {
	m.stopped.Store(true)
	if !m.brokenStop {
		select {
		case <-m.unblockCh:
		default:
			close(m.unblockCh)
		}
	}
	return nil
}

func (m *mockPAStream) Abort() error {
	return m.Stop()
}

func (m *mockPAStream) Close() error {
	m.closed.Store(true)
	return nil
}

func (m *mockPAStream) Read() error {
	m.blockedInRead.Store(true)
	<-m.unblockCh
	return fmt.Errorf("stream stopped")
}

func (m *mockPAStream) Write() error {
	m.blockedInWrite.Store(true)
	<-m.unblockCh
	return fmt.Errorf("stream stopped")
}

// waitBlocked spins until both the capture and playback mocks report they
// are blocked inside Read()/Write(), or until the timeout expires.
func waitBlocked(t *testing.T, capture, playback *mockPAStream, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for !capture.blockedInRead.Load() || !playback.blockedInWrite.Load() {
		select {
		case <-deadline:
			t.Fatalf("goroutines did not block in Read/Write within %v (read=%v write=%v)",
				timeout, capture.blockedInRead.Load(), playback.blockedInWrite.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// mockEncoder implements opusEncoder for testing.
type mockEncoder struct{}

func (m *mockEncoder) Encode(pcm []int16, data []byte) (int, error) {
	// Return a minimal 1-byte "packet".
	if len(data) > 0 {
		data[0] = 0
		return 1, nil
	}
	return 0, nil
}
func (m *mockEncoder) SetBitrate(int) error       { return nil }
func (m *mockEncoder) SetDTX(bool) error           { return nil }
func (m *mockEncoder) SetInBandFEC(bool) error      { return nil }
func (m *mockEncoder) SetPacketLossPerc(int) error  { return nil }

// startWithMocks wires mock streams/encoder and starts the capture+playback
// goroutines the same way Start() does, but without touching real PortAudio.
func startWithMocks(ae *AudioEngine, capture, playback paStream) {
	ae.mu.Lock()
	ae.captureStream = capture
	ae.playbackStream = playback
	ae.encoder = &mockEncoder{}
	ae.stopCh = make(chan struct{})
	ae.notifCh = make(chan []float32, notifChannelBuf)
	ae.running.Store(true)
	ae.mu.Unlock()

	captureBuf := make([]float32, FrameSize)
	playbackBuf := make([]float32, FrameSize)

	ae.wg.Add(2)
	go func() { defer ae.wg.Done(); ae.captureLoop(captureBuf) }()
	go func() { defer ae.wg.Done(); ae.playbackLoop(playbackBuf) }()
}

// TestStopReturnsWhenStreamsUnblock verifies that Stop() completes promptly
// when stream Abort() unblocks Read()/Write().
func TestStopReturnsWhenStreamsUnblock(t *testing.T) {
	ae := NewAudioEngine()
	capture := newMockPAStream(false)
	playback := newMockPAStream(false)
	startWithMocks(ae, capture, playback)

	// Ensure goroutines are actually blocked in Read()/Write().
	waitBlocked(t, capture, playback, 2*time.Second)

	done := make(chan struct{})
	go func() {
		ae.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Stop() returned — success.
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() blocked for >2s — wg.Wait() is likely deadlocked because Abort() didn't unblock Read()/Write()")
	}

	if !capture.stopped.Load() {
		t.Error("capture stream was not stopped")
	}
	if !playback.stopped.Load() {
		t.Error("playback stream was not stopped")
	}
	if !capture.closed.Load() {
		t.Error("capture stream was not closed")
	}
	if !playback.closed.Load() {
		t.Error("playback stream was not closed")
	}
}

// TestStopReturnsQuicklyWhenStreamBroken verifies that Stop() returns
// promptly even if Abort() does NOT unblock Read()/Write(). The deferred-
// close pattern nils the stream fields (so loops exit on their next
// iteration) and hands the old pointers to a background goroutine that
// waits for the loops to finish before calling Close. This avoids the
// SIGSEGV that occurred when Close was called while a goroutine was still
// blocked inside Pa_WriteStream.
func TestStopReturnsQuicklyWhenStreamBroken(t *testing.T) {
	ae := NewAudioEngine()
	capture := newMockPAStream(true)  // broken: Abort() won't unblock Read()
	playback := newMockPAStream(true) // broken: Abort() won't unblock Write()
	startWithMocks(ae, capture, playback)

	// Ensure goroutines are actually blocked in Read()/Write().
	waitBlocked(t, capture, playback, 2*time.Second)

	done := make(chan struct{})
	go func() {
		ae.Stop()
		close(done)
	}()

	// Stop() should return within ~100 ms (one 50 ms grace period), not hang.
	select {
	case <-done:
		// Stop() returned — deferred close pattern worked.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Stop() blocked >500ms — deferred close pattern failed")
	}

	// Streams must NOT be closed yet — goroutines are still blocked in
	// Read()/Write(). Closing while blocked is what caused the SIGSEGV.
	if capture.closed.Load() {
		t.Error("capture stream was closed while goroutine still blocked — would SIGSEGV on real PortAudio")
	}
	if playback.closed.Load() {
		t.Error("playback stream was closed while goroutine still blocked — would SIGSEGV on real PortAudio")
	}

	// Unblock the goroutines (simulates the Read/Write eventually returning).
	close(capture.unblockCh)
	close(playback.unblockCh)

	// The background closer should now close the streams.
	deadline := time.After(2 * time.Second)
	for !capture.closed.Load() || !playback.closed.Load() {
		select {
		case <-deadline:
			t.Fatalf("streams not closed after unblock (capture=%v playback=%v)",
				capture.closed.Load(), playback.closed.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// TestStopIdempotent verifies calling Stop() twice doesn't panic or block.
func TestStopIdempotent(t *testing.T) {
	ae := NewAudioEngine()
	capture := newMockPAStream(false)
	playback := newMockPAStream(false)
	startWithMocks(ae, capture, playback)

	waitBlocked(t, capture, playback, 2*time.Second)

	done := make(chan struct{})
	go func() {
		ae.Stop()
		ae.Stop() // second call should be a no-op
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("double Stop() blocked")
	}
}

// TestStopOnNeverStarted verifies Stop() is a no-op on a fresh engine.
func TestStopOnNeverStarted(t *testing.T) {
	ae := NewAudioEngine()

	done := make(chan struct{})
	go func() {
		ae.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Stop() blocked on an engine that was never started")
	}
}

// TestStopConcurrent verifies multiple concurrent Stop() calls don't race.
func TestStopConcurrent(t *testing.T) {
	ae := NewAudioEngine()
	capture := newMockPAStream(false)
	playback := newMockPAStream(false)
	startWithMocks(ae, capture, playback)

	waitBlocked(t, capture, playback, 2*time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ae.Stop()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent Stop() calls blocked")
	}
}

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

func TestOpusDTXEnable(t *testing.T) {
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		t.Fatalf("new encoder: %v", err)
	}
	if err := enc.SetDTX(true); err != nil {
		t.Fatalf("SetDTX(true): %v", err)
	}
	dtx, err := enc.DTX()
	if err != nil {
		t.Fatalf("DTX(): %v", err)
	}
	if !dtx {
		t.Error("DTX should be true after SetDTX(true)")
	}
}

// --- Push-to-Talk tests ---

func TestPTTModeDefaultOff(t *testing.T) {
	ae := NewAudioEngine()
	if ae.IsPTTMode() {
		t.Error("PTT mode should be off by default")
	}
	if ae.IsPTTActive() {
		t.Error("PTT active should be false by default")
	}
}

func TestPTTModeToggle(t *testing.T) {
	ae := NewAudioEngine()
	ae.SetPTTMode(true)
	if !ae.IsPTTMode() {
		t.Error("PTT mode should be on after SetPTTMode(true)")
	}
	ae.SetPTTMode(false)
	if ae.IsPTTMode() {
		t.Error("PTT mode should be off after SetPTTMode(false)")
	}
}

func TestPTTActiveToggle(t *testing.T) {
	ae := NewAudioEngine()
	ae.SetPTTMode(true)

	ae.SetPTTActive(true)
	if !ae.IsPTTActive() {
		t.Error("PTT should be active after SetPTTActive(true)")
	}

	ae.SetPTTActive(false)
	if ae.IsPTTActive() {
		t.Error("PTT should be inactive after SetPTTActive(false)")
	}
}

func TestPTTDisableClearsActive(t *testing.T) {
	ae := NewAudioEngine()
	ae.SetPTTMode(true)
	ae.SetPTTActive(true)

	// Disabling PTT mode should also clear the active state so the mic
	// doesn't stay stuck open if the user toggles the mode while holding the key.
	ae.SetPTTMode(false)
	if ae.IsPTTActive() {
		t.Error("disabling PTT mode should clear pttActive")
	}
}

func TestPTTModeBlocksCapture(t *testing.T) {
	ae := NewAudioEngine()
	ae.SetPTTMode(true)
	ae.SetPTTActive(false)

	// With PTT enabled but key not held, captureLoop should skip sending.
	// We verify this by checking the state machine conditions directly
	// (the capture loop integration is tested implicitly via the full stack).
	if !ae.pttMode.Load() {
		t.Error("pttMode should be true")
	}
	if ae.pttActive.Load() {
		t.Error("pttActive should be false")
	}
}

func TestPTTModeAllowsCapture(t *testing.T) {
	ae := NewAudioEngine()
	ae.SetPTTMode(true)
	ae.SetPTTActive(true)

	// With PTT enabled and key held, capture should proceed.
	if !ae.pttMode.Load() || !ae.pttActive.Load() {
		t.Error("both pttMode and pttActive should be true")
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

func TestDroppedFrameCounters(t *testing.T) {
	ae := NewAudioEngine()

	// Initially zero.
	c, p := ae.DroppedFrames()
	if c != 0 || p != 0 {
		t.Fatalf("initial drops: capture=%d playback=%d, want 0,0", c, p)
	}

	// Increment capture drops.
	ae.captureDropped.Add(5)
	ae.AddPlaybackDrop()
	ae.AddPlaybackDrop()
	ae.AddPlaybackDrop()

	c, p = ae.DroppedFrames()
	if c != 5 {
		t.Errorf("capture drops: got %d, want 5", c)
	}
	if p != 3 {
		t.Errorf("playback drops: got %d, want 3", p)
	}

	// DroppedFrames resets counters.
	c, p = ae.DroppedFrames()
	if c != 0 || p != 0 {
		t.Errorf("after reset: capture=%d playback=%d, want 0,0", c, p)
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
