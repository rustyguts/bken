package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gordonklaus/portaudio"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App bridges the Go backend with the Wails/Vue frontend.
// Wails-bound methods (Connect, Disconnect, Get*, Set*) are callable from JS.
// Keep this struct thin — delegate to Transport and AudioEngine.
type App struct {
	ctx            context.Context
	audio          *AudioEngine
	transport      Transporter
	connected      atomic.Bool // true while a voice session is active; safe for concurrent access
	startupAddr    string      // host:port extracted from a bken:// CLI argument, if any
	sessionsMu     sync.RWMutex
	sessions       map[string]Transporter // key: normalized server addr
	activeServer   string                 // currently browsed server for text/control actions
	voiceServer    string                 // server currently used for voice channel/audio
	voiceTransport Transporter

	// Metrics cache: updated every 5 s by adaptBitrateLoop; read by GetMetrics.
	metricsMu     sync.Mutex
	cachedMetrics Metrics
}

var (
	buildCommit = "dev"
	buildTime   = ""
)

// BuildInfo contains local app build/runtime details shown in Settings > About.
type BuildInfo struct {
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
	Dirty     bool   `json:"dirty"`
}

// NewApp creates a new App.
func NewApp() *App {
	return &App{
		audio:     NewAudioEngine(),
		transport: NewTransport(),
		sessions:  make(map[string]Transporter),
	}
}

func (a *App) normalizedAddr(addr string) (string, error) {
	return normalizeServerAddr(addr)
}

func (a *App) getSession(addr string) Transporter {
	a.sessionsMu.RLock()
	defer a.sessionsMu.RUnlock()
	return a.sessions[addr]
}

func (a *App) setActiveServer(addr string) {
	a.sessionsMu.Lock()
	a.activeServer = addr
	a.sessionsMu.Unlock()
}

func (a *App) getActiveServer() string {
	a.sessionsMu.RLock()
	defer a.sessionsMu.RUnlock()
	return a.activeServer
}

func (a *App) activeTransport() Transporter {
	a.sessionsMu.RLock()
	defer a.sessionsMu.RUnlock()
	if a.activeServer != "" {
		if tr := a.sessions[a.activeServer]; tr != nil {
			return tr
		}
	}
	// Backward-compatible fallback for tests/legacy single-session code.
	if a.transport != nil {
		return a.transport
	}
	for _, tr := range a.sessions {
		return tr
	}
	return nil
}

func (a *App) requireActiveTransport() (Transporter, error) {
	tr := a.activeTransport()
	if tr == nil {
		return nil, fmt.Errorf("no active server session")
	}
	return tr, nil
}

func (a *App) voiceSession() (addr string, tr Transporter) {
	a.sessionsMu.RLock()
	defer a.sessionsMu.RUnlock()
	return a.voiceServer, a.voiceTransport
}

func (a *App) setVoiceSession(addr string, tr Transporter) {
	a.sessionsMu.Lock()
	a.voiceServer = addr
	a.voiceTransport = tr
	a.sessionsMu.Unlock()
}

func (a *App) preferredAudioTransport() Transporter {
	if _, tr := a.voiceSession(); tr != nil {
		return tr
	}
	return a.activeTransport()
}

// startup is called when the Wails app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := portaudio.Initialize(); err != nil {
		log.Printf("[app] portaudio init: %v", err)
	}

	// Handle files dropped onto elements with --wails-drop-target: drop.
	wailsrt.OnFileDrop(ctx, func(x, y int, paths []string) {
		if len(paths) == 0 {
			return
		}
		// Emit a frontend event so the Vue layer can pick the channel and upload.
		wailsrt.EventsEmit(ctx, "file:dropped", map[string]any{
			"paths": paths,
		})
	})
}

// shutdown is called when the Wails app is closing.
func (a *App) shutdown(_ context.Context) {
	a.Disconnect()
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
		addr = "localhost:8080"
	}
	return AutoLogin{
		Username: os.Getenv("BKEN_USERNAME"),
		Addr:     addr,
	}
}

// GetBuildInfo returns application build/runtime details for diagnostics.
func (a *App) GetBuildInfo() BuildInfo {
	info := BuildInfo{
		Commit:    buildCommit,
		BuildTime: buildTime,
		GoVersion: runtime.Version(),
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
	}

	if bi, ok := debug.ReadBuildInfo(); ok {
		if bi.GoVersion != "" {
			info.GoVersion = bi.GoVersion
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Commit == "" || info.Commit == "dev" {
					info.Commit = s.Value
				}
			case "vcs.time":
				if info.BuildTime == "" {
					info.BuildTime = s.Value
				}
			case "vcs.modified":
				info.Dirty = s.Value == "true"
			}
		}
	}

	return info
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

// SetAudioBitrate sets the Opus target bitrate in kbps.
func (a *App) SetAudioBitrate(kbps int) {
	a.audio.SetBitrate(kbps)
}

// GetAudioBitrate returns the current Opus target bitrate in kbps.
func (a *App) GetAudioBitrate() int {
	return a.audio.CurrentBitrate()
}

// SetAEC enables or disables echo-cancellation preference.
func (a *App) SetAEC(enabled bool) {
	a.audio.SetAEC(enabled)
}

// SetAGC enables or disables automatic gain control on the capture path.
func (a *App) SetAGC(enabled bool) {
	a.audio.SetAGC(enabled)
}

// SetAGCLevel is retained as a no-op for backward compatibility.
func (a *App) SetAGCLevel(level int) {
	a.audio.SetAGCLevel(level)
}

// SetNoiseSuppression enables or disables noise suppression.
func (a *App) SetNoiseSuppression(enabled bool) {
	a.audio.SetNoiseSuppression(enabled)
}

// SetNoiseSuppressionLevel is retained as a no-op for backward compatibility.
func (a *App) SetNoiseSuppressionLevel(level int) {
	_ = level
}

// SetNotificationVolume sets the notification/soundboard volume (0.0-1.0).
func (a *App) SetNotificationVolume(vol float64) {
	a.audio.SetNotificationVolume(float32(vol))
}

// GetNotificationVolume returns the notification volume (0.0-1.0).
func (a *App) GetNotificationVolume() float64 {
	return float64(a.audio.NotificationVolume())
}

// GetInputLevel returns the current mic input RMS level (0.0-1.0).
// Designed to be polled at ~15fps for the input level meter.
func (a *App) GetInputLevel() float64 {
	return float64(a.audio.InputLevel())
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

// SetPTTMode enables or disables push-to-talk mode. When enabled, the
// microphone only transmits while the PTT key is held (via PTTKeyDown/Up).
func (a *App) SetPTTMode(enabled bool) {
	a.audio.SetPTTMode(enabled)
}

// PTTKeyDown signals that the push-to-talk key was pressed. Audio capture
// begins transmitting immediately. No-op when PTT mode is disabled.
func (a *App) PTTKeyDown() {
	a.audio.SetPTTActive(true)
}

// PTTKeyUp signals that the push-to-talk key was released. Audio capture
// stops transmitting. No-op when PTT mode is disabled.
func (a *App) PTTKeyUp() {
	a.audio.SetPTTActive(false)
}

// Connect establishes (or reuses) a control session with the server.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) Connect(addr, username string) string {
	normalizedAddr, err := a.normalizedAddr(addr)
	if err != nil {
		return err.Error()
	}

	// Reuse an existing control session if we are already connected.
	if existing := a.getSession(normalizedAddr); existing != nil {
		a.setActiveServer(normalizedAddr)
		return ""
	}

	// First session reuses App.transport for backward compatibility in tests.
	var tr Transporter
	a.sessionsMu.Lock()
	sessionCount := len(a.sessions)
	if sessionCount == 0 && a.transport != nil {
		tr = a.transport
	} else {
		tr = NewTransport()
	}
	a.sessionsMu.Unlock()

	a.wireSessionCallbacks(normalizedAddr, tr)

	if err := tr.Connect(context.Background(), normalizedAddr, username); err != nil {
		return err.Error()
	}

	a.sessionsMu.Lock()
	a.sessions[normalizedAddr] = tr
	if a.activeServer == "" {
		a.activeServer = normalizedAddr
	}
	a.activeServer = normalizedAddr
	if a.transport == nil {
		a.transport = tr
	}
	a.sessionsMu.Unlock()

	if a.ctx != nil {
		wailsrt.EventsEmit(a.ctx, "server:connected", map[string]any{"server_addr": normalizedAddr})
	}
	log.Printf("[app] connected control session to %s as %s", normalizedAddr, username)
	return ""
}

// wireCallbacks keeps legacy tests/builders working when they set app.transport
// directly without creating named server sessions.
func (a *App) wireCallbacks() {
	if a.transport == nil {
		return
	}
	serverAddr := a.getActiveServer()
	if serverAddr == "" {
		serverAddr = "legacy"
	}
	a.wireSessionCallbacks(serverAddr, a.transport)
}

// wireSessionCallbacks registers transport callbacks and tags each event with
// its server address.
func (a *App) wireSessionCallbacks(serverAddr string, tr Transporter) {
	tr.SetOnUserList(func(users []UserInfo) {
		wailsrt.EventsEmit(a.ctx, "user:list", map[string]any{
			"server_addr": serverAddr,
			"users":       users,
		})
		wailsrt.EventsEmit(a.ctx, "user:me", map[string]any{
			"server_addr": serverAddr,
			"id":          int(tr.MyID()),
		})
	})
	tr.SetOnUserJoined(func(id uint16, name string) {
		wailsrt.EventsEmit(a.ctx, "user:joined", map[string]any{
			"server_addr": serverAddr,
			"id":          id,
			"username":    name,
		})
		a.audio.PlayNotification(SoundUserJoined)
	})
	tr.SetOnUserLeft(func(id uint16) {
		wailsrt.EventsEmit(a.ctx, "user:left", map[string]any{
			"server_addr": serverAddr,
			"id":          id,
		})
		a.audio.PlayNotification(SoundUserLeft)
	})
	tr.SetOnAudioReceived(func(userID uint16) {
		wailsrt.EventsEmit(a.ctx, "audio:speaking", map[string]any{
			"server_addr": serverAddr,
			"id":          int(userID),
		})
	})
	tr.SetOnDisconnected(func(reason string) {
		a.sessionsMu.Lock()
		delete(a.sessions, serverAddr)
		if a.transport == tr {
			a.transport = nil
			for _, nextTr := range a.sessions {
				a.transport = nextTr
				break
			}
		}
		if a.activeServer == serverAddr {
			a.activeServer = ""
			for addr := range a.sessions {
				a.activeServer = addr
				break
			}
		}
		voiceLost := a.voiceServer == serverAddr
		if voiceLost {
			a.voiceServer = ""
			a.voiceTransport = nil
		}
		a.sessionsMu.Unlock()

		if voiceLost && a.connected.Load() {
			a.connected.Store(false)
			a.audio.Stop()
		}

		wailsrt.EventsEmit(a.ctx, "connection:lost", map[string]any{
			"server_addr": serverAddr,
			"reason":      reason,
		})
		wailsrt.EventsEmit(a.ctx, "server:disconnected", map[string]any{
			"server_addr": serverAddr,
			"reason":      reason,
		})
		log.Printf("[app] connection lost (%s): %s", serverAddr, reason)
	})
	tr.SetOnChatMessage(func(msgID uint64, senderID uint16, username, message string, ts int64, fileID int64, fileName string, fileSize int64, mentions []uint16, replyTo uint64, replyPreview *ReplyPreview) {
		payload := map[string]any{
			"server_addr": serverAddr,
			"username":    username,
			"message":     message,
			"ts":          ts,
			"channel_id":  0,
			"msg_id":      msgID,
			"sender_id":   int(senderID),
		}
		if fileID != 0 {
			payload["file_id"] = fileID
			payload["file_name"] = fileName
			payload["file_size"] = fileSize
			payload["file_url"] = fileURLForTransport(tr, fileID)
		}
		if len(mentions) > 0 {
			intMentions := make([]int, len(mentions))
			for i, m := range mentions {
				intMentions[i] = int(m)
			}
			payload["mentions"] = intMentions
		}
		if replyTo != 0 {
			payload["reply_to"] = replyTo
		}
		if replyPreview != nil {
			payload["reply_preview"] = map[string]any{
				"msg_id":   replyPreview.MsgID,
				"username": replyPreview.Username,
				"message":  replyPreview.Message,
				"deleted":  replyPreview.Deleted,
			}
		}
		wailsrt.EventsEmit(a.ctx, "chat:message", payload)
	})
	tr.SetOnChannelChatMessage(func(msgID uint64, senderID uint16, channelID int64, username, message string, ts int64, fileID int64, fileName string, fileSize int64, mentions []uint16, replyTo uint64, replyPreview *ReplyPreview) {
		payload := map[string]any{
			"server_addr": serverAddr,
			"username":    username,
			"message":     message,
			"ts":          ts,
			"channel_id":  channelID,
			"msg_id":      msgID,
			"sender_id":   int(senderID),
		}
		if fileID != 0 {
			payload["file_id"] = fileID
			payload["file_name"] = fileName
			payload["file_size"] = fileSize
			payload["file_url"] = fileURLForTransport(tr, fileID)
		}
		if len(mentions) > 0 {
			intMentions := make([]int, len(mentions))
			for i, m := range mentions {
				intMentions[i] = int(m)
			}
			payload["mentions"] = intMentions
		}
		if replyTo != 0 {
			payload["reply_to"] = replyTo
		}
		if replyPreview != nil {
			payload["reply_preview"] = map[string]any{
				"msg_id":   replyPreview.MsgID,
				"username": replyPreview.Username,
				"message":  replyPreview.Message,
				"deleted":  replyPreview.Deleted,
			}
		}
		wailsrt.EventsEmit(a.ctx, "chat:message", payload)
	})
	tr.SetOnLinkPreview(func(msgID uint64, channelID int64, url, title, desc, image, siteName string) {
		wailsrt.EventsEmit(a.ctx, "chat:link_preview", map[string]any{
			"server_addr": serverAddr,
			"msg_id":      msgID,
			"channel_id":  channelID,
			"url":         url,
			"title":       title,
			"description": desc,
			"image":       image,
			"site_name":   siteName,
		})
	})
	tr.SetOnServerInfo(func(name string) {
		wailsrt.EventsEmit(a.ctx, "server:info", map[string]any{
			"server_addr": serverAddr,
			"name":        name,
		})
	})
	tr.SetOnOwnerChanged(func(ownerID uint16) {
		wailsrt.EventsEmit(a.ctx, "room:owner", map[string]any{
			"server_addr": serverAddr,
			"owner_id":    int(ownerID),
		})
	})
	tr.SetOnKicked(func() {
		_, voiceTr := a.voiceSession()
		if voiceTr == tr {
			a.connected.Store(false)
			a.audio.Stop()
			a.setVoiceSession("", nil)
		}
		wailsrt.EventsEmit(a.ctx, "connection:kicked", map[string]any{
			"server_addr": serverAddr,
		})
		log.Printf("[app] kicked from server %s", serverAddr)
	})
	tr.SetOnChannelList(func(channels []ChannelInfo) {
		wailsrt.EventsEmit(a.ctx, "channel:list", map[string]any{
			"server_addr": serverAddr,
			"channels":    channels,
		})
	})
	tr.SetOnUserChannel(func(userID uint16, channelID int64) {
		wailsrt.EventsEmit(a.ctx, "channel:user_moved", map[string]any{
			"server_addr": serverAddr,
			"user_id":     int(userID),
			"channel_id":  channelID,
		})
	})
	tr.SetOnUserRenamed(func(userID uint16, username string) {
		wailsrt.EventsEmit(a.ctx, "user:renamed", map[string]any{
			"server_addr": serverAddr,
			"id":          int(userID),
			"username":    username,
		})
	})
	tr.SetOnMessageEdited(func(msgID uint64, message string, ts int64) {
		wailsrt.EventsEmit(a.ctx, "chat:message_edited", map[string]any{
			"server_addr": serverAddr,
			"msg_id":      msgID,
			"message":     message,
			"ts":          ts,
		})
	})
	tr.SetOnMessageDeleted(func(msgID uint64) {
		wailsrt.EventsEmit(a.ctx, "chat:message_deleted", map[string]any{
			"server_addr": serverAddr,
			"msg_id":      msgID,
		})
	})
	tr.SetOnVideoState(func(userID uint16, active bool, screenShare bool) {
		wailsrt.EventsEmit(a.ctx, "video:state", map[string]any{
			"server_addr":  serverAddr,
			"id":           int(userID),
			"video_active": active,
			"screen_share": screenShare,
		})
	})
	tr.SetOnReactionAdded(func(msgID uint64, emoji string, userID uint16) {
		wailsrt.EventsEmit(a.ctx, "chat:reaction_added", map[string]any{
			"server_addr": serverAddr,
			"msg_id":      msgID,
			"emoji":       emoji,
			"id":          int(userID),
		})
	})
	tr.SetOnReactionRemoved(func(msgID uint64, emoji string, userID uint16) {
		wailsrt.EventsEmit(a.ctx, "chat:reaction_removed", map[string]any{
			"server_addr": serverAddr,
			"msg_id":      msgID,
			"emoji":       emoji,
			"id":          int(userID),
		})
	})
	tr.SetOnUserTyping(func(userID uint16, username string, channelID int64) {
		wailsrt.EventsEmit(a.ctx, "chat:user_typing", map[string]any{
			"server_addr": serverAddr,
			"id":          int(userID),
			"username":    username,
			"channel_id":  channelID,
		})
	})
	tr.SetOnMessagePinned(func(msgID uint64, channelID int64, userID uint16) {
		wailsrt.EventsEmit(a.ctx, "chat:message_pinned", map[string]any{
			"server_addr": serverAddr,
			"msg_id":      msgID,
			"channel_id":  channelID,
			"id":          int(userID),
		})
	})
	tr.SetOnMessageUnpinned(func(msgID uint64) {
		wailsrt.EventsEmit(a.ctx, "chat:message_unpinned", map[string]any{
			"server_addr": serverAddr,
			"msg_id":      msgID,
		})
	})
	tr.SetOnRecordingState(func(channelID int64, recording bool, startedBy string) {
		wailsrt.EventsEmit(a.ctx, "recording:state", map[string]any{
			"server_addr": serverAddr,
			"channel_id":  channelID,
			"recording":   recording,
			"started_by":  startedBy,
		})
	})
	tr.SetOnVideoLayers(func(userID uint16, layers []VideoLayer) {
		wailsrt.EventsEmit(a.ctx, "video:layers", map[string]any{
			"server_addr": serverAddr,
			"id":          int(userID),
			"layers":      layers,
		})
	})
	tr.SetOnVideoQualityRequest(func(fromUserID uint16, quality string) {
		wailsrt.EventsEmit(a.ctx, "video:quality_request", map[string]any{
			"server_addr": serverAddr,
			"id":          int(fromUserID),
			"quality":     quality,
		})
	})
	a.audio.OnSpeaking = func() {
		voiceAddr, voiceTr := a.voiceSession()
		if voiceTr == nil {
			return
		}
		wailsrt.EventsEmit(a.ctx, "audio:speaking", map[string]any{
			"server_addr": voiceAddr,
			"id":          int(voiceTr.MyID()),
		})
	}
}

// Disconnect tears down all voice/control sessions.
func (a *App) Disconnect() {
	if a.connected.Load() {
		_ = a.DisconnectVoice()
	}

	a.sessionsMu.Lock()
	transports := make([]Transporter, 0, len(a.sessions))
	for _, tr := range a.sessions {
		transports = append(transports, tr)
	}
	a.sessions = make(map[string]Transporter)
	a.activeServer = ""
	a.voiceServer = ""
	a.voiceTransport = nil
	a.sessionsMu.Unlock()

	for _, tr := range transports {
		tr.Disconnect()
	}
	if a.transport != nil && len(transports) == 0 {
		a.transport.Disconnect()
	}

	a.connected.Store(false)
	a.metricsMu.Lock()
	a.cachedMetrics = Metrics{}
	a.metricsMu.Unlock()
	log.Println("[app] disconnected all sessions")
}

// DisconnectServer closes only one server control session (text + signaling).
// Voice is also disconnected if it currently uses this server.
func (a *App) DisconnectServer(addr string) string {
	normalizedAddr, err := a.normalizedAddr(addr)
	if err != nil {
		return err.Error()
	}

	a.sessionsMu.RLock()
	tr := a.sessions[normalizedAddr]
	isVoiceServer := a.voiceServer == normalizedAddr
	a.sessionsMu.RUnlock()
	if tr == nil {
		return ""
	}

	if isVoiceServer {
		if err := a.DisconnectVoice(); err != "" {
			return err
		}
	}

	tr.Disconnect()

	a.sessionsMu.Lock()
	delete(a.sessions, normalizedAddr)
	if a.activeServer == normalizedAddr {
		a.activeServer = ""
		for nextAddr := range a.sessions {
			a.activeServer = nextAddr
			break
		}
	}
	if a.transport == tr {
		a.transport = nil
		for _, nextTr := range a.sessions {
			a.transport = nextTr
			break
		}
	}
	a.sessionsMu.Unlock()

	wailsrt.EventsEmit(a.ctx, "server:disconnected", map[string]any{"server_addr": normalizedAddr})
	return ""
}

// SetActiveServer selects the text/control context used by actions like
// SendChannelChat, RenameServer, MoveUserToChannel, etc.
func (a *App) SetActiveServer(addr string) string {
	normalizedAddr, err := a.normalizedAddr(addr)
	if err != nil {
		return err.Error()
	}
	if a.getSession(normalizedAddr) == nil {
		return "server not connected"
	}
	a.setActiveServer(normalizedAddr)
	return ""
}

// DisconnectVoice stops audio capture/playback and moves the user to the
// lobby (channel 0) but keeps the signaling session alive so chat and
// control messages continue to flow.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) DisconnectVoice() string {
	voiceAddr, voiceTr := a.voiceSession()
	if voiceTr == nil {
		a.connected.Store(false)
		return ""
	}

	a.audio.Stop()
	if err := voiceTr.JoinChannel(0); err != nil {
		if !strings.Contains(err.Error(), "control websocket not connected") {
			return err.Error()
		}
	}
	a.connected.Store(false)
	a.setVoiceSession("", nil)
	a.audio.PlayNotification(SoundUserLeft)
	log.Printf("[app] disconnected voice from %s (control session still active)", voiceAddr)
	return ""
}

// ConnectVoice restarts audio capture/playback and joins the given channel.
// Call this after DisconnectVoice to rejoin voice in a channel.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) ConnectVoice(channelID int) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	targetAddr := a.getActiveServer()
	if targetAddr == "" {
		return "no active server selected"
	}

	prevAddr, prevVoiceTr := a.voiceSession()
	if prevVoiceTr != nil && prevVoiceTr != tr {
		_ = prevVoiceTr.JoinChannel(0)
	}

	startedAudio := false
	if !a.connected.Load() {
		if err := a.audio.Start(); err != nil {
			return err.Error()
		}
		startedAudio = true
	}

	tr.StartReceiving(context.Background(), a.audio.PlaybackIn)
	if startedAudio {
		go a.sendLoop()
		go a.adaptBitrateLoop(a.audio.Done())
	}
	if err := tr.JoinChannel(int64(channelID)); err != nil {
		if startedAudio {
			a.audio.Stop()
		}
		return err.Error()
	}
	a.setVoiceSession(targetAddr, tr)
	a.connected.Store(true)
	a.audio.PlayNotification(SoundConnect)
	if prevAddr != "" && prevAddr != targetAddr {
		log.Printf("[app] switched voice %s -> %s channel %d", prevAddr, targetAddr, channelID)
	} else {
		log.Printf("[app] connected voice on %s channel %d", targetAddr, channelID)
	}
	return ""
}

// GetMetrics returns the most recently cached connection quality metrics.
// The cache is refreshed every 5 s by adaptBitrateLoop while connected.
func (a *App) GetMetrics() Metrics {
	a.metricsMu.Lock()
	defer a.metricsMu.Unlock()
	return a.cachedMetrics
}

// adaptInterval is the metrics refresh interval.
const adaptInterval = 5 * time.Second

// adaptBitrateLoop caches quality metrics for the frontend. Adaptive bitrate
// and custom jitter-depth control were removed in favor of WebRTC defaults.
func (a *App) adaptBitrateLoop(done <-chan struct{}) {
	ticker := time.NewTicker(adaptInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			_, voiceTr := a.voiceSession()
			if voiceTr == nil {
				continue
			}
			m := voiceTr.GetMetrics()

			// Collect local frame drops since the last tick.
			captureDrops, playbackDropsLocal := a.audio.DroppedFrames()
			m.CaptureDropped = captureDrops
			m.PlaybackDropped += playbackDropsLocal

			// Compute quality level including local drops.
			totalDrops := captureDrops + m.PlaybackDropped
			dropRate := float64(totalDrops) / adaptInterval.Seconds()

			m.OpusTargetKbps = a.audio.CurrentBitrate()
			m.QualityLevel = qualityLevel(m.PacketLoss, m.RTTMs, m.JitterMs, dropRate)
			a.metricsMu.Lock()
			a.cachedMetrics = m
			a.metricsMu.Unlock()

			wailsrt.EventsEmit(a.ctx, "voice:quality", m)
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
	if cfg.AudioBitrate > 0 {
		a.audio.SetBitrate(cfg.AudioBitrate)
	}
	a.audio.SetAEC(cfg.AECEnabled)
	a.audio.SetAGC(cfg.AGCEnabled)
	a.audio.SetPTTMode(cfg.PTTEnabled)
	a.SetNoiseSuppression(cfg.NoiseEnabled)
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
	tr := a.preferredAudioTransport()
	if tr == nil {
		return
	}
	tr.MuteUser(uint16(id))
}

// UnmuteUser re-enables incoming audio from the given remote user.
func (a *App) UnmuteUser(id int) {
	tr := a.preferredAudioTransport()
	if tr == nil {
		return
	}
	tr.UnmuteUser(uint16(id))
}

// GetMutedUsers returns the IDs of all currently muted remote users.
func (a *App) GetMutedUsers() []int {
	tr := a.preferredAudioTransport()
	if tr == nil {
		return nil
	}
	ids := tr.MutedUsers()
	out := make([]int, len(ids))
	for i, id := range ids {
		out[i] = int(id)
	}
	return out
}

// SetUserVolume sets the local playback volume for a specific remote user.
// volume is a float64 in [0.0, 2.0] representing 0%-200%.
func (a *App) SetUserVolume(userID int, volume float64) {
	tr := a.preferredAudioTransport()
	if tr == nil {
		return
	}
	tr.SetUserVolume(uint16(userID), volume)
}

// GetUserVolume returns the current local playback volume for a specific remote user.
func (a *App) GetUserVolume(userID int) float64 {
	tr := a.preferredAudioTransport()
	if tr == nil {
		return 1.0
	}
	return tr.GetUserVolume(uint16(userID))
}

// RenameUser updates the current user's display name on the server so that
// future chat messages use the new name. Other clients are notified via a
// user_renamed broadcast.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) RenameUser(name string) string {
	a.sessionsMu.RLock()
	sessions := make([]Transporter, 0, len(a.sessions))
	for _, tr := range a.sessions {
		sessions = append(sessions, tr)
	}
	a.sessionsMu.RUnlock()
	if len(sessions) == 0 {
		// Backward-compatible fallback for single-session/test setups where
		// sessions map is not populated.
		if tr := a.activeTransport(); tr != nil {
			sessions = append(sessions, tr)
		}
	}
	for _, tr := range sessions {
		if err := tr.RenameUser(name); err != nil {
			return err.Error()
		}
	}
	return ""
}

// RenameServer updates the server name. Only succeeds if the caller is the
// room owner; the server enforces the check and broadcasts the update.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) RenameServer(name string) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.RenameServer(name); err != nil {
		return err.Error()
	}
	return ""
}

// KickUser removes the given user from the server. Only succeeds if the
// caller is the room owner; the server enforces the check.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) KickUser(id int) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.KickUser(uint16(id)); err != nil {
		return err.Error()
	}
	return ""
}

// JoinChannel sends a join_channel request for the given channel ID.
// Pass id=0 to leave all channels (return to lobby).
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) JoinChannel(id int) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.JoinChannel(int64(id)); err != nil {
		return err.Error()
	}
	return ""
}

// SendChannelChat sends a channel-scoped chat message to all users in that channel.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) SendChannelChat(channelID int, message string) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.SendChannelChat(int64(channelID), message); err != nil {
		return err.Error()
	}
	return ""
}

// EditMessage asks the server to update a chat message's text.
// Only the original sender is allowed to edit; the server enforces the check.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) EditMessage(msgID int, message string) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.EditMessage(uint64(msgID), message); err != nil {
		return err.Error()
	}
	return ""
}

// DeleteMessage asks the server to delete a chat message.
// The original sender and the room owner are allowed to delete.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) DeleteMessage(msgID int) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.DeleteMessage(uint64(msgID)); err != nil {
		return err.Error()
	}
	return ""
}

// AddReaction adds an emoji reaction to a message.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) AddReaction(msgID int, emoji string) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.AddReaction(uint64(msgID), emoji); err != nil {
		return err.Error()
	}
	return ""
}

// RemoveReaction removes an emoji reaction from a message.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) RemoveReaction(msgID int, emoji string) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.RemoveReaction(uint64(msgID), emoji); err != nil {
		return err.Error()
	}
	return ""
}

// SendTyping notifies the server that the user is typing in a channel.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) SendTyping(channelID int) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.SendTyping(int64(channelID)); err != nil {
		return err.Error()
	}
	return ""
}

// SendChat sends a chat message to the server for fan-out to all participants.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) SendChat(message string) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.SendChat(message); err != nil {
		return err.Error()
	}
	return ""
}

// fileURL constructs a download URL for the given file ID using the API base URL.
func fileURLForTransport(tr Transporter, fileID int64) string {
	if tr == nil {
		return ""
	}
	base := tr.APIBaseURL()
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s/api/files/%d", base, fileID)
}

// fileURL constructs a download URL for the given file ID using the active server API base URL.
func (a *App) fileURL(fileID int64) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return ""
	}
	return fileURLForTransport(tr, fileID)
}

// UploadFile opens a native file dialog and uploads the selected file to the
// current server, then sends a chat message with the file attachment.
// channelID determines which channel the file message is scoped to.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) UploadFile(channelID int) string {
	path, err := wailsrt.OpenFileDialog(a.ctx, wailsrt.OpenDialogOptions{
		Title: "Upload File",
	})
	if err != nil {
		return err.Error()
	}
	if path == "" {
		return "" // user cancelled
	}
	return a.uploadFilePath(int64(channelID), path)
}

// UploadFileFromPath uploads a file at the given path and sends a chat message.
// Used for drag-and-drop where the frontend provides the file path.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) UploadFileFromPath(channelID int, path string) string {
	if path == "" {
		return "no file path"
	}
	return a.uploadFilePath(int64(channelID), path)
}

// uploadResponse mirrors the server's UploadResponse JSON.
type uploadResponse struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

const maxFileSize = 10 * 1024 * 1024 // 10 MB

func (a *App) uploadFilePath(channelID int64, path string) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	base := tr.APIBaseURL()
	if base == "" {
		return "server API not available"
	}

	// Validate file size before uploading.
	info, err := os.Stat(path)
	if err != nil {
		return err.Error()
	}
	if info.Size() > maxFileSize {
		return fmt.Sprintf("file exceeds %d MB limit", maxFileSize/(1024*1024))
	}

	f, err := os.Open(path)
	if err != nil {
		return err.Error()
	}
	defer f.Close()

	// Build multipart form.
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return err.Error()
	}
	if _, err := io.Copy(fw, f); err != nil {
		return err.Error()
	}
	w.Close()

	resp, err := http.Post(base+"/api/upload", w.FormDataContentType(), &buf) //nolint:gosec — LAN server, not arbitrary URL
	if err != nil {
		return err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Sprintf("upload failed (%d): %s", resp.StatusCode, string(body))
	}

	var ur uploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&ur); err != nil {
		return "failed to parse upload response"
	}

	// Send a chat message with the file metadata.
	if err := tr.SendFileChat(channelID, ur.ID, ur.Size, ur.Name, ""); err != nil {
		return err.Error()
	}
	return ""
}

// CreateChannel asks the server to create a new channel.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) CreateChannel(name string) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.CreateChannel(name); err != nil {
		return err.Error()
	}
	return ""
}

// RenameChannel asks the server to rename a channel.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) RenameChannel(id int, name string) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.RenameChannel(int64(id), name); err != nil {
		return err.Error()
	}
	return ""
}

// DeleteChannel asks the server to delete a channel.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) DeleteChannel(id int) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.DeleteChannel(int64(id)); err != nil {
		return err.Error()
	}
	return ""
}

// StartVideo notifies all peers that this user has started video.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) StartVideo() string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.SendVideoState(true, false); err != nil {
		return err.Error()
	}
	return ""
}

// StopVideo notifies all peers that this user has stopped video.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) StopVideo() string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.SendVideoState(false, false); err != nil {
		return err.Error()
	}
	return ""
}

// StartScreenShare notifies all peers that this user has started screen sharing.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) StartScreenShare() string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.SendVideoState(true, true); err != nil {
		return err.Error()
	}
	return ""
}

// StopScreenShare notifies all peers that this user has stopped screen sharing.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) StopScreenShare() string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.SendVideoState(false, false); err != nil {
		return err.Error()
	}
	return ""
}

// MoveUserToChannel asks the server to move a user to a different channel.
// Only succeeds if the caller is the room owner; the server enforces the check.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) MoveUserToChannel(userID int, channelID int) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.MoveUser(uint16(userID), int64(channelID)); err != nil {
		return err.Error()
	}
	return ""
}

// sendFailureThreshold is the number of consecutive SendAudio errors before
// the send loop gives up and disconnects. 50 errors ≈ 1 s of voice at 50 fps.
// Mirrors the server-side circuit breaker threshold for symmetry.
const sendFailureThreshold = 50

// sendLoop reads encoded audio from the capture channel and forwards it via
// transport. Exits when the audio engine stops or after sustained send failures.
//
// Transient errors (e.g. QUIC buffer full) are tolerated: the failure counter
// resets on every successful send. Only after sendFailureThreshold consecutive
// errors does the loop give up and trigger a disconnect.
func (a *App) sendLoop() {
	done := a.audio.Done()
	var consecutiveErrors int
	for {
		select {
		case <-done:
			return
		case data, ok := <-a.audio.CaptureOut:
			if !ok {
				return
			}
			_, voiceTr := a.voiceSession()
			if voiceTr == nil {
				continue
			}
			if err := voiceTr.SendAudio(data); err != nil {
				consecutiveErrors++
				if consecutiveErrors == 1 {
					log.Printf("[app] send audio error: %v", err)
				} else if consecutiveErrors%10 == 0 {
					log.Printf("[app] send audio: %d consecutive errors", consecutiveErrors)
				}
				if consecutiveErrors >= sendFailureThreshold {
					log.Printf("[app] send audio: %d consecutive errors, disconnecting voice", consecutiveErrors)
					_ = a.DisconnectVoice()
					return
				}
				continue
			}
			consecutiveErrors = 0
		}
	}
}

// StartRecording asks the server to start recording voice in a channel.
// Only the room owner can start recording; the server enforces the check.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) StartRecording(channelID int) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.StartRecording(int64(channelID)); err != nil {
		return err.Error()
	}
	return ""
}

// StopRecording asks the server to stop recording voice in a channel.
// Only the room owner can stop recording; the server enforces the check.
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) StopRecording(channelID int) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.StopRecording(int64(channelID)); err != nil {
		return err.Error()
	}
	return ""
}

// RequestVideoQuality asks a remote video sender to switch to a different
// simulcast quality layer. quality must be "high", "medium", or "low".
// Returns an error message string or "" on success (Wails JS binding convention).
func (a *App) RequestVideoQuality(targetUserID int, quality string) string {
	tr, err := a.requireActiveTransport()
	if err != nil {
		return err.Error()
	}
	if err := tr.RequestVideoQuality(uint16(targetUserID), quality); err != nil {
		return err.Error()
	}
	return ""
}
