package main

import (
	"context"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"client/internal/adapt"

	"github.com/gordonklaus/portaudio"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App bridges the Go backend with the Wails/Vue frontend.
// Wails-bound methods (Connect, Disconnect, Get*, Set*) are callable from JS.
// Keep this struct thin — delegate to Transport and AudioEngine.
type App struct {
	ctx         context.Context
	audio       *AudioEngine
	transport   Transporter
	nc          *NoiseCanceller
	connected   atomic.Bool // true while a voice session is active; safe for concurrent access
	startupAddr string      // host:port extracted from a bken:// CLI argument, if any

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

// GetStartupAddr returns the host:port extracted from a bken:// command-line
// argument passed when the app was launched (e.g. by clicking an invite link
// in a browser). Returns "" if no bken:// argument was provided.
func (a *App) GetStartupAddr() string {
	return a.startupAddr
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

// SetAEC enables or disables acoustic echo cancellation on the capture path.
// Enabling resets the adaptive filter for a clean start.
func (a *App) SetAEC(enabled bool) {
	a.audio.SetAEC(enabled)
}

// SetAGC enables or disables automatic gain control on the capture path.
func (a *App) SetAGC(enabled bool) {
	a.audio.SetAGC(enabled)
}

// SetAGCLevel sets the AGC target loudness level (0–100).
func (a *App) SetAGCLevel(level int) {
	a.audio.SetAGCLevel(level)
}

// SetVAD enables or disables voice activity detection.
// When enabled, silent frames are not encoded or transmitted.
func (a *App) SetVAD(enabled bool) {
	a.audio.SetVAD(enabled)
}

// SetVADThreshold sets VAD sensitivity (0–100). Higher values require louder
// speech to be considered active and suppress more background sound.
func (a *App) SetVADThreshold(level int) {
	a.audio.SetVADThreshold(level)
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
	if a.connected.Load() {
		return "already connected"
	}

	// Defensive cleanup in case a stale transport session survived a prior
	// failed/partial teardown while connected=false.
	a.transport.Disconnect()

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

	a.connected.Store(true)
	log.Printf("[app] connected to %s as %s", addr, username)
	return ""
}

// wireCallbacks registers transport and audio callbacks that forward events
// to the frontend via Wails events. Must be called before transport.Connect.
func (a *App) wireCallbacks() {
	a.transport.SetOnUserList(func(users []UserInfo) {
		runtime.EventsEmit(a.ctx, "user:list", users)
		runtime.EventsEmit(a.ctx, "user:me", map[string]any{"id": int(a.transport.MyID())})
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
		if !a.connected.Load() {
			return // user-initiated disconnect, ignore
		}
		a.connected.Store(false)
		a.audio.Stop()
		runtime.EventsEmit(a.ctx, "connection:lost", nil)
		log.Println("[app] connection lost unexpectedly")
	})
	a.transport.SetOnChatMessage(func(username, message string, ts int64) {
		runtime.EventsEmit(a.ctx, "chat:message", map[string]any{
			"username":   username,
			"message":    message,
			"ts":         ts,
			"channel_id": 0,
		})
	})
	a.transport.SetOnChannelChatMessage(func(channelID int64, username, message string, ts int64) {
		runtime.EventsEmit(a.ctx, "chat:message", map[string]any{
			"username":   username,
			"message":    message,
			"ts":         ts,
			"channel_id": channelID,
		})
	})
	a.transport.SetOnServerInfo(func(name string) {
		runtime.EventsEmit(a.ctx, "server:info", map[string]any{"name": name})
	})
	a.transport.SetOnOwnerChanged(func(ownerID uint16) {
		runtime.EventsEmit(a.ctx, "room:owner", map[string]any{"owner_id": int(ownerID)})
	})
	a.transport.SetOnKicked(func() {
		a.connected.Store(false)
		a.audio.Stop()
		runtime.EventsEmit(a.ctx, "connection:kicked", nil)
		log.Println("[app] kicked from server")
	})
	a.transport.SetOnChannelList(func(channels []ChannelInfo) {
		runtime.EventsEmit(a.ctx, "channel:list", channels)
	})
	a.transport.SetOnUserChannel(func(userID uint16, channelID int64) {
		runtime.EventsEmit(a.ctx, "channel:user_moved", map[string]any{
			"user_id":    int(userID),
			"channel_id": channelID,
		})
	})
	a.audio.OnSpeaking = func() {
		runtime.EventsEmit(a.ctx, "audio:speaking", map[string]any{"id": int(a.transport.MyID())})
	}
}

// Disconnect ends the voice session.
func (a *App) Disconnect() {
	wasConnected := a.connected.Swap(false)
	if wasConnected {
		log.Println("[app] disconnecting...")
	}
	a.audio.Stop()
	a.transport.Disconnect()
	a.metricsMu.Lock()
	a.cachedMetrics = Metrics{}
	a.metricsMu.Unlock()
	if wasConnected {
		log.Println("[app] disconnected")
	}
}

// DisconnectVoice stops audio capture/playback and moves the user to the
// lobby (channel 0) but keeps the WebTransport session alive so chat and
// control messages continue to flow.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) DisconnectVoice() string {
	a.audio.Stop()
	if err := a.transport.JoinChannel(0); err != nil {
		return err.Error()
	}
	a.audio.PlayNotification(SoundUserLeft)
	log.Println("[app] disconnected from voice (session still active)")
	return ""
}

// ConnectVoice restarts audio capture/playback and joins the given channel.
// Call this after DisconnectVoice to rejoin voice in a channel.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) ConnectVoice(channelID int) string {
	if err := a.audio.Start(); err != nil {
		return err.Error()
	}
	a.transport.StartReceiving(context.Background(), a.audio.PlaybackIn)
	go a.sendLoop()
	go a.adaptBitrateLoop(a.audio.Done())
	if err := a.transport.JoinChannel(int64(channelID)); err != nil {
		return err.Error()
	}
	a.audio.PlayNotification(SoundConnect)
	log.Printf("[app] reconnected voice in channel %d", channelID)
	return ""
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
	a.audio.SetAEC(cfg.AECEnabled)
	a.audio.SetAGC(cfg.AGCEnabled)
	a.audio.SetAGCLevel(cfg.AGCLevel)
	a.audio.SetVAD(cfg.VADEnabled)
	a.audio.SetVADThreshold(cfg.VADThreshold)
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
	return a.connected.Load()
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

// RenameServer updates the server name. Only succeeds if the caller is the
// room owner; the server enforces the check and broadcasts the update.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) RenameServer(name string) string {
	if err := a.transport.RenameServer(name); err != nil {
		return err.Error()
	}
	return ""
}

// KickUser removes the given user from the server. Only succeeds if the
// caller is the room owner; the server enforces the check.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) KickUser(id int) string {
	if err := a.transport.KickUser(uint16(id)); err != nil {
		return err.Error()
	}
	return ""
}

// JoinChannel sends a join_channel request for the given channel ID.
// Pass id=0 to leave all channels (return to lobby).
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) JoinChannel(id int) string {
	if err := a.transport.JoinChannel(int64(id)); err != nil {
		return err.Error()
	}
	return ""
}

// SendChannelChat sends a channel-scoped chat message to all users in that channel.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) SendChannelChat(channelID int, message string) string {
	if err := a.transport.SendChannelChat(int64(channelID), message); err != nil {
		return err.Error()
	}
	return ""
}

// SendChat sends a chat message to the server for fan-out to all participants.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) SendChat(message string) string {
	if err := a.transport.SendChat(message); err != nil {
		return err.Error()
	}
	return ""
}

// sendLoop reads encoded audio from the capture channel and forwards it via
// transport. Exits when the audio engine stops or on send error.
// On send error, it closes the transport session so that readControl detects
// the disconnect and fires onDisconnected → frontend reconnect banner.
func (a *App) sendLoop() {
	done := a.audio.Done()
	for {
		select {
		case <-done:
			return
		case data := <-a.audio.CaptureOut:
			if err := a.transport.SendAudio(data); err != nil {
				log.Printf("[app] send audio error, closing session: %v", err)
				a.transport.Disconnect()
				return
			}
		}
	}
}
