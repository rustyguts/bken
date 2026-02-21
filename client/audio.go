package main

import (
	"log"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gordonklaus/portaudio"
	"gopkg.in/hraban/opus.v2"
)

const (
	sampleRate  = 48000
	channels    = 1
	FrameSize   = 960 // 20ms @ 48kHz — exported so other packages can reference it
	opusBitrate = 32000

	captureChannelBuf  = 30   // ~600ms @ 50 fps — low latency; drops if consumer falls behind
	playbackChannelBuf = 30   // ~600ms @ 50 fps — low latency; silence fills gaps
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
	SetDTX(dtx bool) error
	SetInBandFEC(fec bool) error
	SetPacketLossPerc(lossPerc int) error
}

// opusDecoder abstracts Opus decoding for testing.
type opusDecoder interface {
	Decode(data []byte, pcm []int16) (int, error)
	DecodeFEC(data []byte, pcm []int16) error
}

// AudioEngine manages audio capture, playback, Opus encoding/decoding.
type AudioEngine struct {
	mu sync.Mutex

	inputDeviceID  int
	outputDeviceID int
	volume         float64

	encoder opusEncoder
	decoder opusDecoder

	captureStream  paStream
	playbackStream paStream

	// CaptureOut carries encoded Opus frames ready to send over the network.
	CaptureOut chan []byte
	// PlaybackIn carries tagged Opus frames from the network (sender ID + seq + data).
	PlaybackIn chan TaggedAudio

	// UserVolumeFunc, if set, returns the per-user volume multiplier (0.0-2.0)
	// for the given sender ID. Default (nil) means 1.0 for all users.
	UserVolumeFunc func(senderID uint16) float64
	// notifCh carries pre-chunked raw PCM float32 frames (FrameSize each)
	// synthesised by PlayNotification. Mixed into the output after voice decoding.
	notifCh    chan []float32
	notifScale atomic.Uint32 // float32 bits: notification volume scale (default 1.0)

	echoCancellationEnabled atomic.Bool
	autoGainControlEnabled  atomic.Bool
	noiseSuppressionEnabled atomic.Bool

	running        atomic.Bool
	testMode       atomic.Bool
	muted          atomic.Bool
	deafened       atomic.Bool
	pttMode        atomic.Bool  // true = push-to-talk controls transmit
	pttActive      atomic.Bool  // true = PTT key is held, mic is hot
	currentBitrate atomic.Int32 // kbps; set in Start() and updated by SetBitrate()

	// Dropped frame counters: incremented when CaptureOut / PlaybackIn channels
	// are full and a frame is silently discarded. Read and reset by DroppedFrames().
	captureDropped  atomic.Uint64
	playbackDropped atomic.Uint64

	// inputLevel stores the most recent pre-gate RMS level (float32 bits)
	// for the input level meter. Updated every captureLoop iteration.
	inputLevel atomic.Uint32

	stopCh     chan struct{}
	wg         sync.WaitGroup // tracks captureLoop + playbackLoop goroutines
	OnSpeaking func()         // called (throttled) when mic audio exceeds speaking threshold
}

// notifChannelBuf is the number of 20 ms PCM frames the notification channel
// can buffer — enough for ~4 s of queued notification audio.
const notifChannelBuf = 200

// NewAudioEngine returns an AudioEngine with default settings.
func NewAudioEngine() *AudioEngine {
	ae := &AudioEngine{
		inputDeviceID:  -1,
		outputDeviceID: -1,
		volume:         1.0,
		CaptureOut:     make(chan []byte, captureChannelBuf),
		PlaybackIn:     make(chan TaggedAudio, playbackChannelBuf),
		notifCh:        make(chan []float32, notifChannelBuf),
		stopCh:         make(chan struct{}),
	}
	ae.notifScale.Store(math.Float32bits(1.0))
	ae.echoCancellationEnabled.Store(true)
	ae.noiseSuppressionEnabled.Store(true)
	ae.autoGainControlEnabled.Store(true)
	return ae
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

// SetAEC enables or disables echo-cancellation preference.
func (ae *AudioEngine) SetAEC(enabled bool) {
	ae.echoCancellationEnabled.Store(enabled)
}

// SetAGC enables or disables automatic gain control preference.
func (ae *AudioEngine) SetAGC(enabled bool) {
	ae.autoGainControlEnabled.Store(enabled)
}

// SetNoiseSuppression enables or disables noise suppression preference.
func (ae *AudioEngine) SetNoiseSuppression(enabled bool) {
	ae.noiseSuppressionEnabled.Store(enabled)
}

// SetAGCLevel is a legacy no-op retained for backward compatibility.
func (ae *AudioEngine) SetAGCLevel(level int) {
	_ = level
}

// SetNotificationVolume sets the notification sound volume (0.0-1.0).
func (ae *AudioEngine) SetNotificationVolume(vol float32) {
	if vol < 0 {
		vol = 0
	}
	if vol > 1.0 {
		vol = 1.0
	}
	ae.notifScale.Store(math.Float32bits(vol))
}

// NotificationVolume returns the current notification volume (0.0-1.0).
func (ae *AudioEngine) NotificationVolume() float32 {
	return math.Float32frombits(ae.notifScale.Load())
}

// InputLevel returns the most recent pre-gate RMS mic input level (0.0-1.0).
// Suitable for driving a real-time level meter at ~15 fps.
func (ae *AudioEngine) InputLevel() float32 {
	return math.Float32frombits(ae.inputLevel.Load())
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

// SetPacketLoss tells the Opus encoder the expected packet loss percentage
// so it can tune how much FEC redundancy to embed. lossPercent is clamped
// to [0, 100].
func (ae *AudioEngine) SetPacketLoss(lossPercent int) {
	if lossPercent < 0 {
		lossPercent = 0
	}
	if lossPercent > 100 {
		lossPercent = 100
	}
	ae.mu.Lock()
	if ae.encoder != nil {
		if err := ae.encoder.SetPacketLossPerc(lossPercent); err != nil {
			log.Printf("[audio] SetPacketLossPerc %d%%: %v", lossPercent, err)
		}
	}
	ae.mu.Unlock()
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
	enc.SetDTX(true)
	enc.SetInBandFEC(true)
	enc.SetPacketLossPerc(5) // conservative default estimate
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

	// Drain stale tagged frames so they don't bleed into the next session.
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

func frameRMS(buf []float32) float32 {
	if len(buf) == 0 {
		return 0
	}
	var sum float64
	for _, s := range buf {
		x := float64(s)
		sum += x * x
	}
	return float32(math.Sqrt(sum / float64(len(buf))))
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

		rms := frameRMS(buf)
		ae.inputLevel.Store(math.Float32bits(rms))

		if ae.OnSpeaking != nil && !ae.muted.Load() && rms > 0.01 && time.Since(lastSpeakEmit) > 80*time.Millisecond {
			lastSpeakEmit = time.Now()
			ae.OnSpeaking()
		}

		// Push-to-talk gate: when PTT mode is enabled, only encode and
		// send while the PTT key is held. This check runs after speaking
		// detection so the indicator still updates while muted by PTT.
		if ae.pttMode.Load() && !ae.pttActive.Load() {
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
			case ae.PlaybackIn <- TaggedAudio{SenderID: 0, Seq: 0, OpusData: encoded}:
			default:
			}
		} else if !ae.muted.Load() {
			select {
			case ae.CaptureOut <- encoded:
			default:
				ae.captureDropped.Add(1)
			}
		}
	}
}

// decoderPruneInterval controls how often per-sender decoders are pruned
// for senders that have gone silent (every N playback cycles ≈ N*20 ms).
const decoderPruneInterval = 500 // ~10 s

func (ae *AudioEngine) playbackLoop(buf []float32) {
	pcm := make([]int16, FrameSize)
	decoders := make(map[uint16]opusDecoder)
	lastDecoded := make(map[uint16]time.Time)
	latestFrame := make(map[uint16]TaggedAudio)
	var pruneCounter int

	for {
		// Check for stop before every write cycle.
		select {
		case <-ae.stopCh:
			return
		default:
		}

		// Drain all available tagged frames; keep the most recent frame per sender.
	drain:
		for {
			select {
			case tagged := <-ae.PlaybackIn:
				latestFrame[tagged.SenderID] = tagged
			default:
				break drain
			}
		}

		// Start with silence.
		zeroFloat32(buf)

		if !ae.deafened.Load() {
			ae.mu.Lock()
			vol := ae.volume
			ae.mu.Unlock()
			scale := float32(vol) / 32768.0

			for senderID, tagged := range latestFrame {
				dec, ok := decoders[senderID]
				if !ok {
					d, err := opus.NewDecoder(sampleRate, channels)
					if err != nil {
						log.Printf("[audio] create decoder for sender %d: %v", senderID, err)
						continue
					}
					dec = d
					decoders[senderID] = dec
				}

				n, err := dec.Decode(tagged.OpusData, pcm)
				if err != nil {
					log.Printf("[audio] decode sender %d: %v", senderID, err)
					continue
				}
				lastDecoded[senderID] = time.Now()

				// Per-user volume multiplier.
				userScale := scale
				if ae.UserVolumeFunc != nil {
					userScale = scale * float32(ae.UserVolumeFunc(senderID))
				}

				// Additively mix this sender into the output buffer.
				for i := 0; i < n; i++ {
					buf[i] += float32(pcm[i]) * userScale
				}
			}

			// Clamp mixed output to [-1.0, 1.0].
			for i := range buf {
				buf[i] = clampFloat32(buf[i])
			}
		}

		for senderID := range latestFrame {
			delete(latestFrame, senderID)
		}

		// Periodically prune stale decoders for users that have gone silent.
		pruneCounter++
		if pruneCounter >= decoderPruneInterval {
			pruneCounter = 0
			cutoff := time.Now().Add(-30 * time.Second)
			for senderID, seen := range lastDecoded {
				if seen.Before(cutoff) {
					delete(lastDecoded, senderID)
					delete(decoders, senderID)
				}
			}
		}

		// Mix in one notification frame if available. Notifications bypass the
		// deafen check so UI sounds (mute, join/leave) are always audible.
		select {
		case notifFrame := <-ae.notifCh:
			ns := math.Float32frombits(ae.notifScale.Load())
			for i, s := range notifFrame {
				buf[i] = clampFloat32(buf[i] + s*ns)
			}
		default:
		}

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

// SetPTTMode enables or disables push-to-talk mode. When enabled, the
// microphone only transmits while the PTT key is held (pttActive=true).
// PTT mode gates transmission to key press state when enabled.
func (ae *AudioEngine) SetPTTMode(enabled bool) {
	ae.pttMode.Store(enabled)
	if !enabled {
		ae.pttActive.Store(false)
	}
}

// SetPTTActive sets whether the push-to-talk key is currently held.
// Only meaningful when PTT mode is enabled.
func (ae *AudioEngine) SetPTTActive(active bool) {
	ae.pttActive.Store(active)
}

// IsPTTMode reports whether push-to-talk mode is enabled.
func (ae *AudioEngine) IsPTTMode() bool {
	return ae.pttMode.Load()
}

// IsPTTActive reports whether the PTT key is currently held.
func (ae *AudioEngine) IsPTTActive() bool {
	return ae.pttActive.Load()
}

// DroppedFrames returns and resets the capture and playback drop counters.
// Call periodically (e.g. from adaptBitrateLoop) to include in metrics.
func (ae *AudioEngine) DroppedFrames() (capture, playback uint64) {
	return ae.captureDropped.Swap(0), ae.playbackDropped.Swap(0)
}

// AddPlaybackDrop increments the playback dropped-frame counter.
// Called from the transport receive goroutine when PlaybackIn is full.
func (ae *AudioEngine) AddPlaybackDrop() {
	ae.playbackDropped.Add(1)
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
