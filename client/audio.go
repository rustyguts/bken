package main

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	"client/internal/aec"
	"client/internal/agc"
	"client/internal/vad"

	"github.com/gordonklaus/portaudio"
	"gopkg.in/hraban/opus.v2"
)

const (
	sampleRate  = 48000
	channels    = 1
	FrameSize   = 960 // 20ms @ 48kHz — exported so other packages can reference it
	opusBitrate = 32000

	captureChannelBuf  = 30 // ~600ms @ 50 fps — low latency; drops if consumer falls behind
	playbackChannelBuf = 30 // ~600ms @ 50 fps — low latency; silence fills gaps
	opusMaxPacketBytes = 1275 // RFC 6716 max Opus packet size
)

// AudioDevice describes an available audio device.
type AudioDevice struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// paStream abstracts a PortAudio stream for testing.
type paStream interface {
	Start() error
	Stop() error
	Close() error
	Read() error
	Write() error
}

// opusEncoder abstracts Opus encoding for testing.
type opusEncoder interface {
	Encode(pcm []int16, data []byte) (int, error)
	SetBitrate(bitrate int) error
}

// opusDecoder abstracts Opus decoding for testing.
type opusDecoder interface {
	Decode(data []byte, pcm []int16) (int, error)
}

// AudioEngine manages audio capture, playback, Opus encoding/decoding.
type AudioEngine struct {
	mu sync.Mutex

	inputDeviceID  int
	outputDeviceID int
	volume         float64
	nc             *NoiseCanceller

	encoder opusEncoder
	decoder opusDecoder

	captureStream  paStream
	playbackStream paStream

	// CaptureOut carries encoded Opus frames ready to send over the network.
	CaptureOut chan []byte
	// PlaybackIn carries encoded Opus frames received from the network.
	PlaybackIn chan []byte
	// notifCh carries pre-chunked raw PCM float32 frames (FrameSize each)
	// synthesised by PlayNotification. Mixed into the output after voice decoding.
	notifCh chan []float32

	aecProc    *aec.AEC
	aecEnabled atomic.Bool

	agcProc    *agc.AGC
	agcEnabled atomic.Bool

	vadProc *vad.VAD

	running        atomic.Bool
	testMode       atomic.Bool
	muted          atomic.Bool
	deafened       atomic.Bool
	currentBitrate atomic.Int32 // kbps; set in Start() and updated by SetBitrate()

	stopCh     chan struct{}
	wg         sync.WaitGroup // tracks captureLoop + playbackLoop goroutines
	OnSpeaking func()         // called (throttled) when mic audio exceeds speaking threshold
}

// notifChannelBuf is the number of 20 ms PCM frames the notification channel
// can buffer — enough for ~4 s of queued notification audio.
const notifChannelBuf = 200

// NewAudioEngine returns an AudioEngine with default settings.
func NewAudioEngine() *AudioEngine {
	return &AudioEngine{
		inputDeviceID:  -1,
		outputDeviceID: -1,
		volume:         1.0,
		aecProc:        aec.New(FrameSize),
		agcProc:        agc.New(),
		vadProc:        vad.New(),
		CaptureOut:     make(chan []byte, captureChannelBuf),
		PlaybackIn:     make(chan []byte, playbackChannelBuf),
		notifCh:        make(chan []float32, notifChannelBuf),
		stopCh:         make(chan struct{}),
	}
}

// SetNoiseCanceller attaches (or detaches when nc is nil) a NoiseCanceller.
func (ae *AudioEngine) SetNoiseCanceller(nc *NoiseCanceller) {
	ae.mu.Lock()
	ae.nc = nc
	ae.mu.Unlock()
}

// Done returns a channel that is closed when the audio engine stops.
func (ae *AudioEngine) Done() <-chan struct{} {
	return ae.stopCh
}

// ListInputDevices returns available audio input devices.
func (ae *AudioEngine) ListInputDevices() []AudioDevice {
	return listDevices(func(d *portaudio.DeviceInfo) bool { return d.MaxInputChannels > 0 })
}

// ListOutputDevices returns available audio output devices.
func (ae *AudioEngine) ListOutputDevices() []AudioDevice {
	return listDevices(func(d *portaudio.DeviceInfo) bool { return d.MaxOutputChannels > 0 })
}

// listDevices returns devices matching the given predicate.
func listDevices(match func(*portaudio.DeviceInfo) bool) []AudioDevice {
	devices, err := portaudio.Devices()
	if err != nil {
		log.Printf("[audio] list devices: %v", err)
		return nil
	}
	var out []AudioDevice
	for i, d := range devices {
		if match(d) {
			out = append(out, AudioDevice{ID: i, Name: d.Name})
		}
	}
	return out
}

// SetInputDevice sets the input device by index.
func (ae *AudioEngine) SetInputDevice(id int) {
	ae.mu.Lock()
	ae.inputDeviceID = id
	ae.mu.Unlock()
}

// SetOutputDevice sets the output device by index.
func (ae *AudioEngine) SetOutputDevice(id int) {
	ae.mu.Lock()
	ae.outputDeviceID = id
	ae.mu.Unlock()
}

// SetVolume sets the playback volume in [0.0, 1.0].
func (ae *AudioEngine) SetVolume(vol float64) {
	if vol < 0 {
		vol = 0
	}
	if vol > 1 {
		vol = 1
	}
	ae.mu.Lock()
	ae.volume = vol
	ae.mu.Unlock()
}

// SetAEC enables or disables acoustic echo cancellation on the capture path.
// Enabling resets the adaptive filter weights for a clean start.
func (ae *AudioEngine) SetAEC(enabled bool) {
	ae.aecProc.SetEnabled(enabled)
	ae.aecEnabled.Store(enabled)
}

// SetAGC enables or disables automatic gain control on the capture path.
func (ae *AudioEngine) SetAGC(enabled bool) {
	if enabled {
		ae.agcProc.Reset()
	}
	ae.agcEnabled.Store(enabled)
}

// SetAGCLevel sets the AGC target loudness. level is in [0, 100] and maps to
// an RMS target of [0.01, 0.50] (see agc.SetTarget).
func (ae *AudioEngine) SetAGCLevel(level int) {
	ae.agcProc.SetTarget(level)
}

// SetVAD enables or disables voice activity detection on the capture path.
// When enabled, silent frames are not encoded or sent to the network.
func (ae *AudioEngine) SetVAD(enabled bool) {
	ae.vadProc.SetEnabled(enabled)
}

// SetVADThreshold sets the sensitivity of the VAD. level is in [0, 100] where
// higher values suppress more (require louder speech to be considered active).
func (ae *AudioEngine) SetVADThreshold(level int) {
	ae.vadProc.SetThreshold(level)
}

// SetBitrate changes the Opus encoder target bitrate (kbps) on the fly.
// The value is clamped to the valid Opus range [6, 510].
// Safe to call concurrently with audio capture.
func (ae *AudioEngine) SetBitrate(kbps int) {
	if kbps < 6 {
		kbps = 6
	}
	if kbps > 510 {
		kbps = 510
	}
	ae.mu.Lock()
	if ae.encoder != nil {
		if err := ae.encoder.SetBitrate(kbps * 1000); err != nil {
			log.Printf("[audio] SetBitrate %d kbps: %v", kbps, err)
		}
	}
	ae.mu.Unlock()
	ae.currentBitrate.Store(int32(kbps))
}

// CurrentBitrate returns the current Opus encoder target bitrate (kbps).
func (ae *AudioEngine) CurrentBitrate() int {
	return int(ae.currentBitrate.Load())
}

// Start initializes the Opus codec and starts capture/playback streams.
func (ae *AudioEngine) Start() error {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	if ae.running.Load() {
		return nil
	}

	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		return err
	}
	enc.SetBitrate(opusBitrate)
	ae.encoder = enc
	ae.currentBitrate.Store(opusBitrate / 1000)

	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return err
	}
	ae.decoder = dec

	devices, err := portaudio.Devices()
	if err != nil {
		return err
	}

	inputDev, err := resolveDevice(devices, ae.inputDeviceID, portaudio.DefaultInputDevice)
	if err != nil {
		return err
	}

	outputDev, err := resolveDevice(devices, ae.outputDeviceID, portaudio.DefaultOutputDevice)
	if err != nil {
		return err
	}

	captureBuf := make([]float32, FrameSize)
	captureParams := portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   inputDev,
			Channels: channels,
			Latency:  inputDev.DefaultLowInputLatency,
		},
		SampleRate:      sampleRate,
		FramesPerBuffer: FrameSize,
	}
	captureStream, err := portaudio.OpenStream(captureParams, captureBuf)
	if err != nil {
		return err
	}

	playbackBuf := make([]float32, FrameSize)
	playbackParams := portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   outputDev,
			Channels: channels,
			Latency:  outputDev.DefaultLowOutputLatency,
		},
		SampleRate:      sampleRate,
		FramesPerBuffer: FrameSize,
	}
	playbackStream, err := portaudio.OpenStream(playbackParams, playbackBuf)
	if err != nil {
		captureStream.Close()
		return err
	}

	if err := captureStream.Start(); err != nil {
		captureStream.Close()
		playbackStream.Close()
		return err
	}
	if err := playbackStream.Start(); err != nil {
		captureStream.Stop()
		captureStream.Close()
		playbackStream.Close()
		return err
	}

	ae.captureStream = captureStream
	ae.playbackStream = playbackStream
	ae.stopCh = make(chan struct{})
	ae.notifCh = make(chan []float32, notifChannelBuf)
	ae.running.Store(true)

	ae.wg.Add(2)
	go func() { defer ae.wg.Done(); ae.captureLoop(captureBuf) }()
	go func() { defer ae.wg.Done(); ae.playbackLoop(playbackBuf) }()

	log.Printf("[audio] started capture=%s playback=%s", inputDev.Name, outputDev.Name)
	return nil
}

// resolveDevice returns the device at idx if valid, otherwise calls fallback.
func resolveDevice(devices []*portaudio.DeviceInfo, idx int, fallback func() (*portaudio.DeviceInfo, error)) (*portaudio.DeviceInfo, error) {
	if idx >= 0 && idx < len(devices) {
		return devices[idx], nil
	}
	return fallback()
}

// Stop halts audio capture and playback.
//
// Sequence matters here: Pa_StopStream is thread-safe and causes any blocking
// Pa_ReadStream/Pa_WriteStream calls to return, which lets the goroutines exit.
// We must wait for them via wg before calling Pa_CloseStream, otherwise we free
// the native stream object while a goroutine may still be touching it (SIGSEGV).
func (ae *AudioEngine) Stop() {
	if !ae.running.CompareAndSwap(true, false) {
		return
	}
	close(ae.stopCh)

	// Stop streams first — this unblocks any Read/Write calls in the goroutines.
	ae.mu.Lock()
	if ae.captureStream != nil {
		ae.captureStream.Stop()
	}
	if ae.playbackStream != nil {
		ae.playbackStream.Stop()
	}
	ae.mu.Unlock()

	// Wait for goroutines to fully exit before freeing stream objects.
	ae.wg.Wait()

	ae.mu.Lock()
	if ae.captureStream != nil {
		ae.captureStream.Close()
		ae.captureStream = nil
	}
	if ae.playbackStream != nil {
		ae.playbackStream.Close()
		ae.playbackStream = nil
	}
	ae.mu.Unlock()

	// Drain stale frames so they don't bleed into the next session.
	for {
		select {
		case <-ae.PlaybackIn:
		default:
			log.Println("[audio] stopped")
			return
		}
	}
}

// zeroFloat32 zeroes all elements of buf.
func zeroFloat32(buf []float32) {
	for i := range buf {
		buf[i] = 0
	}
}

// clampFloat32 clamps v to [-1.0, 1.0].
func clampFloat32(v float32) float32 {
	if v > 1.0 {
		return 1.0
	}
	if v < -1.0 {
		return -1.0
	}
	return v
}

func (ae *AudioEngine) captureLoop(buf []float32) {
	// Reuse allocations across frames.
	pcm := make([]int16, FrameSize)
	opusBuf := make([]byte, opusMaxPacketBytes)
	var lastSpeakEmit time.Time

	for ae.running.Load() {
		if err := ae.captureStream.Read(); err != nil {
			if ae.running.Load() {
				log.Printf("[audio] capture read: %v", err)
			}
			return
		}

		// Apply acoustic echo cancellation before any other processing so the
		// downstream stages (noise suppression, AGC, VAD) see a cleaner signal.
		if ae.aecEnabled.Load() {
			ae.aecProc.Process(buf)
		}

		// Compute RMS once; reuse for both speaking detection and VAD.
		rms := vad.RMS(buf)

		if ae.OnSpeaking != nil && rms > 0.01 && time.Since(lastSpeakEmit) > 80*time.Millisecond {
			lastSpeakEmit = time.Now()
			ae.OnSpeaking()
		}

		// Apply noise cancellation if enabled.
		ae.mu.Lock()
		nc := ae.nc
		ae.mu.Unlock()
		if nc != nil {
			nc.Process(buf)
		}

		// Apply AGC if enabled.
		if ae.agcEnabled.Load() {
			ae.agcProc.Process(buf)
		}

		// Voice activity detection: skip silent frames entirely to save
		// CPU and bandwidth. Hangover keeps trailing frames so word endings
		// are not clipped. Re-measure RMS after processing so AGC/noise
		// changes are reflected in the VAD decision.
		if !ae.vadProc.ShouldSend(vad.RMS(buf)) {
			continue
		}

		// Convert float32 to int16 for Opus encoder.
		for i, s := range buf {
			pcm[i] = int16(clampFloat32(s) * 32767)
		}

		n, err := ae.encoder.Encode(pcm, opusBuf)
		if err != nil {
			log.Printf("[audio] encode: %v", err)
			continue
		}

		encoded := make([]byte, n)
		copy(encoded, opusBuf[:n])

		// In test mode, loop back directly to playback; otherwise send to network
		// (unless muted).
		if ae.testMode.Load() {
			select {
			case ae.PlaybackIn <- encoded:
			default:
			}
		} else if !ae.muted.Load() {
			select {
			case ae.CaptureOut <- encoded:
			default:
			}
		}
	}
}

func (ae *AudioEngine) playbackLoop(buf []float32) {
	pcm := make([]int16, FrameSize)

	for {
		// Check for stop before every write cycle.
		select {
		case <-ae.stopCh:
			return
		default:
		}

		// Non-blocking receive: decode a packet if one is ready, otherwise write
		// silence. playbackStream.Write() blocks until the hardware buffer needs
		// more samples, naturally pacing this loop without an external ticker.
		// Blocking here on the channel would starve the stream during silence
		// gaps and cause underruns.
		select {
		case data := <-ae.PlaybackIn:
			if ae.deafened.Load() {
				zeroFloat32(buf)
			} else {
				n, err := ae.decoder.Decode(data, pcm)
				if err != nil {
					log.Printf("[audio] decode: %v", err)
					zeroFloat32(buf)
				} else {
					ae.mu.Lock()
					vol := ae.volume
					ae.mu.Unlock()
					scale := float32(vol) / 32768.0
					for i := 0; i < n; i++ {
						buf[i] = float32(pcm[i]) * scale
					}
					zeroFloat32(buf[n:])
				}
			}
		default:
			// No packet ready — output silence to keep the stream fed.
			zeroFloat32(buf)
		}

		// Mix in one notification frame if available. Notifications bypass the
		// deafen check so UI sounds (mute, join/leave) are always audible.
		select {
		case notifFrame := <-ae.notifCh:
			for i, s := range notifFrame {
				buf[i] = clampFloat32(buf[i] + s)
			}
		default:
		}

		// Feed the final output buffer to the AEC as the far-end reference.
		// Done after all mixing (voice + notifications) so the reference
		// matches exactly what the speakers will emit.
		ae.aecProc.FeedFarEnd(buf)

		if err := ae.playbackStream.Write(); err != nil {
			if ae.running.Load() {
				log.Printf("[audio] playback write: %v", err)
			}
			return
		}
	}
}

// StartTest enables loopback test mode (capture goes directly to playback).
func (ae *AudioEngine) StartTest() error {
	ae.testMode.Store(true)
	return ae.Start()
}

// StopTest disables test mode and stops audio.
func (ae *AudioEngine) StopTest() {
	ae.testMode.Store(false)
	ae.Stop()
}

// SetMuted mutes or unmutes the microphone (stops sending audio).
func (ae *AudioEngine) SetMuted(muted bool) {
	ae.muted.Store(muted)
}

// SetDeafened enables or disables audio playback.
func (ae *AudioEngine) SetDeafened(deafened bool) {
	ae.deafened.Store(deafened)
}

// EncodeFrame encodes a PCM int16 frame to Opus. Exported for testing.
func (ae *AudioEngine) EncodeFrame(pcm []int16) ([]byte, error) {
	buf := make([]byte, opusMaxPacketBytes)
	n, err := ae.encoder.Encode(pcm, buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// DecodeFrame decodes an Opus frame to PCM int16. Exported for testing.
func (ae *AudioEngine) DecodeFrame(data []byte) ([]int16, error) {
	pcm := make([]int16, FrameSize)
	n, err := ae.decoder.Decode(data, pcm)
	if err != nil {
		return nil, err
	}
	return pcm[:n], nil
}
