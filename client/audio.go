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
	sampleRate = 48000
	channels   = 1
	frameSize  = 960 // 20ms @ 48kHz
	opusBitrate = 32000
)

// AudioDevice describes an available audio device.
type AudioDevice struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// AudioEngine manages audio capture, playback, OPUS encoding/decoding.
type AudioEngine struct {
	mu sync.Mutex

	inputDeviceID  int
	outputDeviceID int
	volume         float64
	nc             *NoiseCanceller

	encoder *opus.Encoder
	decoder *opus.Decoder

	captureStream  *portaudio.Stream
	playbackStream *portaudio.Stream

	// Channels for PCM data flow.
	CaptureOut chan []byte // Encoded OPUS frames ready to send.
	PlaybackIn chan []byte // Encoded OPUS frames received from network.

	running   atomic.Bool
	testMode  atomic.Bool
	muted     atomic.Bool
	deafened  atomic.Bool

	stopCh     chan struct{}
	OnSpeaking func() // called (throttled) when mic audio exceeds speaking threshold
}

// SetNoiseCanceller attaches (or detaches when nc is nil) a NoiseCanceller.
func (ae *AudioEngine) SetNoiseCanceller(nc *NoiseCanceller) {
	ae.mu.Lock()
	ae.nc = nc
	ae.mu.Unlock()
}

func NewAudioEngine() *AudioEngine {
	return &AudioEngine{
		inputDeviceID:  -1,
		outputDeviceID: -1,
		volume:         1.0,
		CaptureOut:     make(chan []byte, 100),
		PlaybackIn:     make(chan []byte, 100),
		stopCh:         make(chan struct{}),
	}
}

// Done returns a channel that is closed when the audio engine stops.
func (ae *AudioEngine) Done() <-chan struct{} {
	return ae.stopCh
}

// ListInputDevices returns available audio input devices.
func (ae *AudioEngine) ListInputDevices() []AudioDevice {
	devices, err := portaudio.Devices()
	if err != nil {
		log.Printf("[audio] list devices: %v", err)
		return nil
	}

	var out []AudioDevice
	for i, d := range devices {
		if d.MaxInputChannels > 0 {
			out = append(out, AudioDevice{ID: i, Name: d.Name})
		}
	}
	return out
}

// ListOutputDevices returns available audio output devices.
func (ae *AudioEngine) ListOutputDevices() []AudioDevice {
	devices, err := portaudio.Devices()
	if err != nil {
		log.Printf("[audio] list devices: %v", err)
		return nil
	}

	var out []AudioDevice
	for i, d := range devices {
		if d.MaxOutputChannels > 0 {
			out = append(out, AudioDevice{ID: i, Name: d.Name})
		}
	}
	return out
}

// SetInputDevice sets the input device by index.
func (ae *AudioEngine) SetInputDevice(id int) {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	ae.inputDeviceID = id
}

// SetOutputDevice sets the output device by index.
func (ae *AudioEngine) SetOutputDevice(id int) {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	ae.outputDeviceID = id
}

// SetVolume sets the playback volume (0.0 - 1.0).
func (ae *AudioEngine) SetVolume(vol float64) {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	if vol < 0 {
		vol = 0
	}
	if vol > 1 {
		vol = 1
	}
	ae.volume = vol
}

// Start initializes OPUS codec and starts capture/playback streams.
func (ae *AudioEngine) Start() error {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	if ae.running.Load() {
		return nil
	}

	// Initialize OPUS encoder.
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		return err
	}
	enc.SetBitrate(opusBitrate)
	ae.encoder = enc

	// Initialize OPUS decoder.
	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return err
	}
	ae.decoder = dec

	// Get devices.
	devices, err := portaudio.Devices()
	if err != nil {
		return err
	}

	// Resolve input device.
	var inputDev *portaudio.DeviceInfo
	if ae.inputDeviceID >= 0 && ae.inputDeviceID < len(devices) {
		inputDev = devices[ae.inputDeviceID]
	} else {
		inputDev, err = portaudio.DefaultInputDevice()
		if err != nil {
			return err
		}
	}

	// Resolve output device.
	var outputDev *portaudio.DeviceInfo
	if ae.outputDeviceID >= 0 && ae.outputDeviceID < len(devices) {
		outputDev = devices[ae.outputDeviceID]
	} else {
		outputDev, err = portaudio.DefaultOutputDevice()
		if err != nil {
			return err
		}
	}

	// Start capture stream.
	captureParams := portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   inputDev,
			Channels: channels,
			Latency:  inputDev.DefaultLowInputLatency,
		},
		SampleRate:      sampleRate,
		FramesPerBuffer: frameSize,
	}

	captureBuf := make([]float32, frameSize)
	ae.captureStream, err = portaudio.OpenStream(captureParams, captureBuf)
	if err != nil {
		return err
	}

	// Start playback stream.
	playbackParams := portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   outputDev,
			Channels: channels,
			Latency:  outputDev.DefaultLowOutputLatency,
		},
		SampleRate:      sampleRate,
		FramesPerBuffer: frameSize,
	}

	playbackBuf := make([]float32, frameSize)
	ae.playbackStream, err = portaudio.OpenStream(playbackParams, playbackBuf)
	if err != nil {
		ae.captureStream.Close()
		return err
	}

	if err := ae.captureStream.Start(); err != nil {
		ae.captureStream.Close()
		ae.playbackStream.Close()
		return err
	}

	if err := ae.playbackStream.Start(); err != nil {
		ae.captureStream.Close()
		ae.playbackStream.Close()
		return err
	}

	ae.stopCh = make(chan struct{})
	ae.running.Store(true)

	// Capture goroutine: read PCM → encode OPUS → CaptureOut channel.
	go ae.captureLoop(captureBuf)

	// Playback goroutine: PlaybackIn channel → decode OPUS → write PCM.
	go ae.playbackLoop(playbackBuf)

	log.Printf("[audio] started capture=%s playback=%s", inputDev.Name, outputDev.Name)
	return nil
}

// Stop halts audio capture and playback.
func (ae *AudioEngine) Stop() {
	if !ae.running.CompareAndSwap(true, false) {
		return
	}
	close(ae.stopCh)

	ae.mu.Lock()
	defer ae.mu.Unlock()

	if ae.captureStream != nil {
		ae.captureStream.Stop()
		ae.captureStream.Close()
		ae.captureStream = nil
	}
	if ae.playbackStream != nil {
		ae.playbackStream.Stop()
		ae.playbackStream.Close()
		ae.playbackStream = nil
	}

	log.Println("[audio] stopped")
}

func computeRMS(buf []float32) float32 {
	var sum float32
	for _, s := range buf {
		sum += s * s
	}
	return float32(math.Sqrt(float64(sum / float32(len(buf)))))
}

func (ae *AudioEngine) captureLoop(buf []float32) {
	opusBuf := make([]byte, 1024)
	var lastSpeakEmit time.Time

	for ae.running.Load() {
		err := ae.captureStream.Read()
		if err != nil {
			if ae.running.Load() {
				log.Printf("[audio] capture read: %v", err)
				go ae.Stop() // signal app that audio failed
			}
			return
		}

		if ae.OnSpeaking != nil {
			if computeRMS(buf) > 0.01 && time.Since(lastSpeakEmit) > 80*time.Millisecond {
				lastSpeakEmit = time.Now()
				ae.OnSpeaking()
			}
		}

		// Apply noise cancellation if enabled.
		ae.mu.Lock()
		nc := ae.nc
		ae.mu.Unlock()
		if nc != nil {
			nc.Process(buf)
		}

		// Convert float32 to int16 for OPUS encoder.
		pcm := make([]int16, frameSize)
		for i, s := range buf {
			if s > 1.0 {
				s = 1.0
			}
			if s < -1.0 {
				s = -1.0
			}
			pcm[i] = int16(s * 32767)
		}

		n, err := ae.encoder.Encode(pcm, opusBuf)
		if err != nil {
			log.Printf("[audio] encode: %v", err)
			continue
		}

		encoded := make([]byte, n)
		copy(encoded, opusBuf[:n])

		// In test mode, loop back directly to playback.
		if ae.testMode.Load() {
			select {
			case ae.PlaybackIn <- encoded:
			default:
			}
		} else {
			if !ae.muted.Load() {
				select {
				case ae.CaptureOut <- encoded:
				default:
				}
			}
		}
	}
}

func (ae *AudioEngine) playbackLoop(buf []float32) {
	pcm := make([]int16, frameSize)

	for {
		select {
		case <-ae.stopCh:
			return
		case data := <-ae.PlaybackIn:
			if ae.deafened.Load() {
				// Write silence to keep the stream alive.
				for i := range buf {
					buf[i] = 0
				}
				if err := ae.playbackStream.Write(); err != nil {
					if ae.running.Load() {
						log.Printf("[audio] playback write: %v", err)
					}
					return
				}
				continue
			}
			n, err := ae.decoder.Decode(data, pcm)
			if err != nil {
				log.Printf("[audio] decode: %v", err)
				continue
			}

			ae.mu.Lock()
			vol := ae.volume
			ae.mu.Unlock()

			// Convert int16 to float32 with volume.
			for i := 0; i < n; i++ {
				buf[i] = float32(pcm[i]) / 32768.0 * float32(vol)
			}
			// Zero out remaining samples.
			for i := n; i < frameSize; i++ {
				buf[i] = 0
			}

			if err := ae.playbackStream.Write(); err != nil {
				if ae.running.Load() {
					log.Printf("[audio] playback write: %v", err)
				}
				return
			}
		}
	}
}

// StartTest enables test mode (loopback: capture goes directly to playback).
func (ae *AudioEngine) StartTest() error {
	ae.testMode.Store(true)
	return ae.Start()
}

// StopTest disables test mode and stops audio.
func (ae *AudioEngine) StopTest() {
	ae.testMode.Store(false)
	ae.Stop()
}

// SetMuted mutes/unmutes the microphone (stops sending audio).
func (ae *AudioEngine) SetMuted(muted bool) {
	ae.muted.Store(muted)
}

// SetDeafened enables/disables audio playback.
func (ae *AudioEngine) SetDeafened(deafened bool) {
	ae.deafened.Store(deafened)
}

// EncodeFrame encodes a PCM int16 frame to OPUS. Exported for testing.
func (ae *AudioEngine) EncodeFrame(pcm []int16) ([]byte, error) {
	buf := make([]byte, 1024)
	n, err := ae.encoder.Encode(pcm, buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// DecodeFrame decodes an OPUS frame to PCM int16. Exported for testing.
func (ae *AudioEngine) DecodeFrame(data []byte) ([]int16, error) {
	pcm := make([]int16, frameSize)
	n, err := ae.decoder.Decode(data, pcm)
	if err != nil {
		return nil, err
	}
	return pcm[:n], nil
}
