package main

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"client/internal/adapt"

	"github.com/gordonklaus/portaudio"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App bridges the Go backend with the Wails/Vue frontend.
// Wails-bound methods (Connect, Disconnect, Get*, Set*) are callable from JS.
// Keep this struct thin — delegate to Transport and AudioEngine.
type App struct {
	ctx       context.Context
	audio     *AudioEngine
	transport Transporter
	nc        *NoiseCanceller
	connected bool
	testUser  *TestUser // non-nil when BKEN_TEST_USER is configured

	// Metrics cache: updated every 5 s by adaptBitrateLoop; read by GetMetrics.
	metricsMu     sync.Mutex
	cachedMetrics Metrics
}

// NewApp creates a new App.
func NewApp() *App {
	return &App{
		audio:     NewAudioEngine(),
		transport: NewTransport(),
	}
}

// startup is called when the Wails app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := portaudio.Initialize(); err != nil {
		log.Printf("[app] portaudio init: %v", err)
	}
}

// shutdown is called when the Wails app is closing.
func (a *App) shutdown(_ context.Context) {
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
// Empty Username means no auto-login is configured.
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

// SetInputDevice sets the active audio input device by index.
func (a *App) SetInputDevice(id int) {
	a.audio.SetInputDevice(id)
}

// SetOutputDevice sets the active audio output device by index.
func (a *App) SetOutputDevice(id int) {
	a.audio.SetOutputDevice(id)
}

// SetVolume sets playback volume in the range [0.0, 1.0].
func (a *App) SetVolume(vol float64) {
	a.audio.SetVolume(vol)
}

// SetAGC enables or disables automatic gain control on the capture path.
func (a *App) SetAGC(enabled bool) {
	a.audio.SetAGC(enabled)
}

// SetAGCLevel sets the AGC target loudness level (0–100).
func (a *App) SetAGCLevel(level int) {
	a.audio.SetAGCLevel(level)
}

// SetNoiseSuppression enables or disables noise suppression.
// The NoiseCanceller is created lazily on first call.
func (a *App) SetNoiseSuppression(enabled bool) {
	if a.nc == nil {
		a.nc = NewNoiseCanceller()
		a.audio.SetNoiseCanceller(a.nc)
	}
	a.nc.SetEnabled(enabled)
}

// SetNoiseSuppressionLevel sets the suppression blend level (0–100 → 0.0–1.0).
// The NoiseCanceller is created lazily on first call.
func (a *App) SetNoiseSuppressionLevel(level int) {
	if a.nc == nil {
		a.nc = NewNoiseCanceller()
		a.audio.SetNoiseCanceller(a.nc)
	}
	a.nc.SetLevel(float32(level) / 100.0)
}

// StartTest starts the audio loopback test.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) StartTest() string {
	if err := a.audio.StartTest(); err != nil {
		return err.Error()
	}
	return ""
}

// StopTest stops the audio loopback test.
func (a *App) StopTest() {
	a.audio.StopTest()
}

// SetMuted mutes or unmutes the microphone.
func (a *App) SetMuted(muted bool) {
	a.audio.SetMuted(muted)
	if muted {
		a.audio.PlayNotification(SoundMute)
	} else {
		a.audio.PlayNotification(SoundUnmute)
	}
}

// SetDeafened enables or disables audio playback.
func (a *App) SetDeafened(deafened bool) {
	a.audio.SetDeafened(deafened)
	if deafened {
		a.audio.PlayNotification(SoundMute)
	} else {
		a.audio.PlayNotification(SoundUnmute)
	}
}

// Connect establishes a voice session with the server.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) Connect(addr, username string) string {
	if a.connected {
		return "already connected"
	}

	a.wireCallbacks()

	if err := a.transport.Connect(context.Background(), addr, username); err != nil {
		return err.Error()
	}

	if err := a.audio.Start(); err != nil {
		a.transport.Disconnect()
		return err.Error()
	}

	a.transport.StartReceiving(context.Background(), a.audio.PlaybackIn)
	go a.sendLoop()
	go a.adaptBitrateLoop(a.audio.Done())

	a.audio.PlayNotification(SoundConnect)

	if err := a.startTestUser(addr); err != nil {
		log.Printf("[app] test user: %v", err)
	}

	a.connected = true
	log.Printf("[app] connected to %s as %s", addr, username)
	return ""
}

// wireCallbacks registers transport and audio callbacks that forward events
// to the frontend via Wails events. Must be called before transport.Connect.
func (a *App) wireCallbacks() {
	a.transport.SetOnUserList(func(users []UserInfo) {
		runtime.EventsEmit(a.ctx, "user:list", users)
	})
	a.transport.SetOnUserJoined(func(id uint16, name string) {
		runtime.EventsEmit(a.ctx, "user:joined", map[string]interface{}{"id": id, "username": name})
		a.audio.PlayNotification(SoundUserJoined)
	})
	a.transport.SetOnUserLeft(func(id uint16) {
		runtime.EventsEmit(a.ctx, "user:left", map[string]interface{}{"id": id})
		a.audio.PlayNotification(SoundUserLeft)
	})
	a.transport.SetOnAudioReceived(func(userID uint16) {
		runtime.EventsEmit(a.ctx, "audio:speaking", map[string]any{"id": int(userID)})
	})
	a.transport.SetOnDisconnected(func() {
		if !a.connected {
			return // user-initiated disconnect, ignore
		}
		a.connected = false
		a.audio.Stop()
		runtime.EventsEmit(a.ctx, "connection:lost", nil)
		log.Println("[app] connection lost unexpectedly")
	})
	a.audio.OnSpeaking = func() {
		runtime.EventsEmit(a.ctx, "audio:speaking", map[string]any{"id": int(a.transport.MyID())})
	}
}

// startTestUser connects a virtual bot peer if BKEN_TEST_USER is set.
// "1" or "true" uses the default name "TestUser"; any other value is the bot name.
func (a *App) startTestUser(addr string) error {
	name := os.Getenv("BKEN_TEST_USER")
	if name == "" {
		return nil
	}
	if name == "1" || name == "true" {
		name = "TestUser"
	}
	tu := newTestUser()
	if err := tu.start(addr, name); err != nil {
		return err
	}
	a.testUser = tu
	log.Printf("[app] test user %q connected to %s", name, addr)
	return nil
}

// Disconnect ends the voice session.
func (a *App) Disconnect() {
	if !a.connected {
		return
	}
	a.connected = false
	a.audio.Stop()
	a.transport.Disconnect()
	if a.testUser != nil {
		a.testUser.stop()
		a.testUser = nil
	}
	a.metricsMu.Lock()
	a.cachedMetrics = Metrics{}
	a.metricsMu.Unlock()
	log.Println("[app] disconnected")
}

// GetMetrics returns the most recently cached connection quality metrics.
// The cache is refreshed every 5 s by adaptBitrateLoop while connected.
func (a *App) GetMetrics() Metrics {
	a.metricsMu.Lock()
	defer a.metricsMu.Unlock()
	return a.cachedMetrics
}

// adaptBitrateLoop polls transport metrics every 5 s, adapts the Opus encoder
// bitrate based on observed packet loss and RTT, then caches the metrics for
// the frontend. It exits when done is closed (i.e. when audio stops).
func (a *App) adaptBitrateLoop(done <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			m := a.transport.GetMetrics()
			currentKbps := a.audio.CurrentBitrate()
			nextKbps := adapt.NextBitrate(currentKbps, m.PacketLoss, m.RTTMs)
			if nextKbps != currentKbps {
				log.Printf("[app] adapting bitrate %d→%d kbps (loss=%.1f%% rtt=%.0fms)",
					currentKbps, nextKbps, m.PacketLoss*100, m.RTTMs)
				a.audio.SetBitrate(nextKbps)
			}
			m.OpusTargetKbps = nextKbps
			a.metricsMu.Lock()
			a.cachedMetrics = m
			a.metricsMu.Unlock()
		}
	}
}

// GetConfig loads and returns the persisted user config.
func (a *App) GetConfig() Config {
	return LoadConfig()
}

// ApplyConfig reads the saved config from disk and applies all audio settings
// to the engine. Call this once on startup so settings are active before the
// user opens the settings panel for the first time.
func (a *App) ApplyConfig() {
	cfg := LoadConfig()
	a.audio.SetVolume(cfg.Volume)
	a.audio.SetAGC(cfg.AGCEnabled)
	a.audio.SetAGCLevel(cfg.AGCLevel)
	a.SetNoiseSuppression(cfg.NoiseEnabled)
	a.SetNoiseSuppressionLevel(cfg.NoiseLevel)
	if cfg.InputDeviceID >= 0 {
		a.audio.SetInputDevice(cfg.InputDeviceID)
	}
	if cfg.OutputDeviceID >= 0 {
		a.audio.SetOutputDevice(cfg.OutputDeviceID)
	}
}

// SaveConfig persists the given user config to disk.
func (a *App) SaveConfig(cfg Config) {
	if err := SaveConfig(cfg); err != nil {
		log.Printf("[app] save config: %v", err)
	}
}

// IsConnected reports whether a voice session is currently active.
func (a *App) IsConnected() bool {
	return a.connected
}

// MuteUser suppresses incoming audio from the given remote user.
// id is the server-assigned numeric user ID.
func (a *App) MuteUser(id int) {
	a.transport.MuteUser(uint16(id))
}

// UnmuteUser re-enables incoming audio from the given remote user.
func (a *App) UnmuteUser(id int) {
	a.transport.UnmuteUser(uint16(id))
}

// GetMutedUsers returns the IDs of all currently muted remote users.
func (a *App) GetMutedUsers() []int {
	ids := a.transport.MutedUsers()
	out := make([]int, len(ids))
	for i, id := range ids {
		out[i] = int(id)
	}
	return out
}

// sendLoop reads encoded audio from the capture channel and forwards it via
// transport. Exits when the audio engine stops or on send error.
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
