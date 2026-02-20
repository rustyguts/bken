package main

import (
	"context"
	"log"
	"os"

	"github.com/gordonklaus/portaudio"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct - methods are bound to the frontend via Wails.
type App struct {
	ctx       context.Context
	audio     *AudioEngine
	transport *Transport
	nc        *NoiseCanceller
	connected bool
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{
		audio:     NewAudioEngine(),
		transport: NewTransport(),
	}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := portaudio.Initialize(); err != nil {
		log.Printf("[app] portaudio init: %v", err)
	}
}

// shutdown is called when the app is closing.
func (a *App) shutdown(ctx context.Context) {
	a.Disconnect()
	if a.nc != nil {
		a.nc.Destroy()
		a.nc = nil
	}
	portaudio.Terminate()
}

// AutoLogin holds credentials pre-populated from environment variables.
type AutoLogin struct {
	Username string `json:"username"`
	Addr     string `json:"addr"`
}

// GetAutoLogin returns credentials from BKEN_USERNAME / BKEN_ADDR env vars.
// Empty strings mean no auto-login is configured.
func (a *App) GetAutoLogin() AutoLogin {
	addr := os.Getenv("BKEN_ADDR")
	if addr == "" {
		addr = "localhost:4433"
	}
	return AutoLogin{
		Username: os.Getenv("BKEN_USERNAME"),
		Addr:     addr,
	}
}

// GetInputDevices returns available audio input devices.
func (a *App) GetInputDevices() []AudioDevice {
	return a.audio.ListInputDevices()
}

// GetOutputDevices returns available audio output devices.
func (a *App) GetOutputDevices() []AudioDevice {
	return a.audio.ListOutputDevices()
}

// SetInputDevice sets the input device.
func (a *App) SetInputDevice(id int) {
	a.audio.SetInputDevice(id)
}

// SetOutputDevice sets the output device.
func (a *App) SetOutputDevice(id int) {
	a.audio.SetOutputDevice(id)
}

// SetVolume sets the playback volume (0.0 - 1.0).
func (a *App) SetVolume(vol float64) {
	a.audio.SetVolume(vol)
}

// SetNoiseSuppression enables or disables noise suppression. Lazily creates the
// NoiseCanceller on first call.
func (a *App) SetNoiseSuppression(enabled bool) {
	if a.nc == nil {
		a.nc = NewNoiseCanceller()
		a.audio.SetNoiseCanceller(a.nc)
	}
	a.nc.SetEnabled(enabled)
}

// SetNoiseSuppressionLevel sets the suppression blend level (0–100 mapped to 0.0–1.0).
// Lazily creates the NoiseCanceller on first call.
func (a *App) SetNoiseSuppressionLevel(level int) {
	if a.nc == nil {
		a.nc = NewNoiseCanceller()
		a.audio.SetNoiseCanceller(a.nc)
	}
	a.nc.SetLevel(float32(level) / 100.0)
}

// StartTest starts audio loopback test.
func (a *App) StartTest() string {
	if err := a.audio.StartTest(); err != nil {
		return err.Error()
	}
	return ""
}

// StopTest stops audio loopback test.
func (a *App) StopTest() {
	a.audio.StopTest()
}

// Connect connects to a voice server.
func (a *App) Connect(addr, username string) string {
	if a.connected {
		return "already connected"
	}

	// Set up control message callbacks.
	a.transport.OnUserList = func(users []UserInfo) {
		runtime.EventsEmit(a.ctx, "user:list", users)
	}
	a.transport.OnUserJoined = func(id uint16, name string) {
		runtime.EventsEmit(a.ctx, "user:joined", map[string]interface{}{"id": id, "username": name})
	}
	a.transport.OnUserLeft = func(id uint16) {
		runtime.EventsEmit(a.ctx, "user:left", map[string]interface{}{"id": id})
	}

	// Connect transport.
	if err := a.transport.Connect(context.Background(), addr, username); err != nil {
		return err.Error()
	}

	// Speaking detection events.
	a.audio.OnSpeaking = func() {
		runtime.EventsEmit(a.ctx, "audio:speaking", map[string]any{"id": int(a.transport.MyID())})
	}
	a.transport.OnAudioReceived = func(userID uint16) {
		runtime.EventsEmit(a.ctx, "audio:speaking", map[string]any{"id": int(userID)})
	}

	// Unexpected disconnect — signal frontend to auto-reconnect.
	a.transport.OnDisconnected = func() {
		if !a.connected {
			return // user-initiated disconnect, ignore
		}
		a.connected = false
		a.audio.Stop()
		runtime.EventsEmit(a.ctx, "connection:lost", nil)
		log.Println("[app] connection lost unexpectedly")
	}

	// Start audio engine.
	if err := a.audio.Start(); err != nil {
		a.transport.Disconnect()
		return err.Error()
	}

	// Start receiving datagrams → playback.
	a.transport.StartReceiving(context.Background(), a.audio.PlaybackIn)

	// Start sending capture → datagrams.
	go a.sendLoop()

	a.connected = true
	log.Printf("[app] connected to %s as %s", addr, username)
	return ""
}

// Disconnect disconnects from the voice server.
func (a *App) Disconnect() {
	if !a.connected {
		return
	}
	a.audio.Stop()
	a.transport.Disconnect()
	a.connected = false
	log.Println("[app] disconnected")
}

// GetMetrics returns current connection metrics.
func (a *App) GetMetrics() Metrics {
	return a.transport.GetMetrics()
}

// IsConnected returns whether currently connected.
func (a *App) IsConnected() bool {
	return a.connected
}

// sendLoop reads encoded audio from capture and sends via transport.
// Exits when the audio engine stops (Done() closes) or on send error.
func (a *App) sendLoop() {
	done := a.audio.Done()
	for {
		select {
		case <-done:
			return
		case data := <-a.audio.CaptureOut:
			if err := a.transport.SendAudio(data); err != nil {
				log.Printf("[app] send audio: %v", err)
				return
			}
		}
	}
}
