package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// mutedSet is a concurrent set of uint16 user IDs.
type mutedSet struct{ m sync.Map }

func (ms *mutedSet) Add(id uint16)    { ms.m.Store(id, struct{}{}) }
func (ms *mutedSet) Remove(id uint16) { ms.m.Delete(id) }
func (ms *mutedSet) Has(id uint16) bool {
	_, ok := ms.m.Load(id)
	return ok
}
func (ms *mutedSet) Clear() {
	ms.m.Range(func(k, _ any) bool { ms.m.Delete(k); return true })
}
func (ms *mutedSet) Slice() []uint16 {
	var out []uint16
	ms.m.Range(func(k, _ any) bool { out = append(out, k.(uint16)); return true })
	return out
}

// ICEServerInfo describes a STUN or TURN server for WebRTC peer connections.
type ICEServerInfo struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// ControlMsg mirrors the server's control message format.
type ControlMsg struct {
	Type          string          `json:"type"`
	Username      string          `json:"username,omitempty"`
	ID            uint16          `json:"id,omitempty"`
	SelfID        uint16          `json:"self_id,omitempty"`
	TargetID      uint16          `json:"target_id,omitempty"`
	Users         []UserInfo      `json:"users,omitempty"`
	Ts            int64           `json:"ts,omitempty"`              // ping/pong timestamp (Unix ms)
	Message       string          `json:"message,omitempty"`         // chat: body text
	ServerName    string          `json:"server_name,omitempty"`     // user_list: human-readable server name
	OwnerID       uint16          `json:"owner_id,omitempty"`        // user_list/owner_changed: current channel owner
	ChannelID     int64           `json:"channel_id,omitempty"`      // join_channel/user_channel: target channel
	Channels      []ChannelInfo   `json:"channels,omitempty"`        // channel_list: full list of channels
	APIPort       int             `json:"api_port,omitempty"`        // user_list: HTTP API port for file uploads
	ICEServers    []ICEServerInfo `json:"ice_servers,omitempty"`     // user_list: ICE servers for WebRTC
	FileID        int64           `json:"file_id,omitempty"`         // chat: uploaded file DB id
	FileName      string          `json:"file_name,omitempty"`       // chat: original filename
	FileSize      int64           `json:"file_size,omitempty"`       // chat: file size in bytes
	MsgID         uint64          `json:"msg_id,omitempty"`          // chat/link_preview: server-assigned message ID
	LinkURL       string          `json:"link_url,omitempty"`        // link_preview: the URL that was fetched
	LinkTitle     string          `json:"link_title,omitempty"`      // link_preview: page title
	LinkDesc      string          `json:"link_desc,omitempty"`       // link_preview: page description
	LinkImage     string          `json:"link_image,omitempty"`      // link_preview: preview image URL
	LinkSite      string          `json:"link_site,omitempty"`       // link_preview: site name
	SDP           string          `json:"sdp,omitempty"`             // webrtc_offer/webrtc_answer
	Candidate     string          `json:"candidate,omitempty"`       // webrtc_ice
	SDPMid        string          `json:"sdp_mid,omitempty"`         // webrtc_ice
	SDPMLineIndex *uint16         `json:"sdp_mline_index,omitempty"` // webrtc_ice
	VideoActive   *bool           `json:"video_active,omitempty"`    // video_state: whether user has video on
	ScreenShare   *bool           `json:"screen_share,omitempty"`    // video_state: whether this is a screen share
	Mentions      []uint16        `json:"mentions,omitempty"`        // chat: user IDs mentioned
	Emoji         string          `json:"emoji,omitempty"`           // add_reaction/remove_reaction: emoji character
	ReplyTo       uint64          `json:"reply_to,omitempty"`        // chat: message ID being replied to
	ReplyPreview  *ReplyPreview   `json:"reply_preview,omitempty"`   // chat: preview of replied-to message
	Pinned        *bool           `json:"pinned,omitempty"`          // message_pinned/message_unpinned
	Recording     *bool           `json:"recording,omitempty"`       // recording_started/stopped
	VideoLayers   []VideoLayer    `json:"video_layers,omitempty"`    // video_state: simulcast layers
	VideoQuality  string          `json:"video_quality,omitempty"`   // set_video_quality: requested layer
}

// ReplyPreview is a compact preview of the original message in a reply.
type ReplyPreview struct {
	MsgID    uint64 `json:"msg_id"`
	Username string `json:"username"`
	Message  string `json:"message"`
	Deleted  bool   `json:"deleted,omitempty"`
}

// UserInfo describes a connected peer.
type UserInfo struct {
	ID        uint16 `json:"id"`
	Username  string `json:"username"`
	ChannelID int64  `json:"channel_id,omitempty"` // 0 = not in any channel
	Role      string `json:"role,omitempty"`       // OWNER/ADMIN/MODERATOR/USER
}

// ChannelInfo describes a voice channel.
type ChannelInfo struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	MaxUsers int    `json:"max_users,omitempty"` // 0 = unlimited
}

// ChatHistoryMessage is a single message in a channel's message history.
type ChatHistoryMessage struct {
	MsgID    int64  `json:"msg_id"`
	Username string `json:"username"`
	Message  string `json:"message"`
	TS       int64  `json:"ts"`
}

// VideoLayer describes a simulcast video layer available from a sender.
type VideoLayer struct {
	Quality string `json:"quality"` // "high", "medium", or "low"
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Bitrate int    `json:"bitrate"` // kbps
}

type backendUser struct {
	ID       string             `json:"id"`
	Username string             `json:"username"`
	Voice    *backendVoiceState `json:"voice,omitempty"`
}

type backendVoiceState struct {
	ServerID  string `json:"server_id"`
	ChannelID string `json:"channel_id"`
	Muted     bool   `json:"muted,omitempty"`
	Deafened  bool   `json:"deafened,omitempty"`
}

type backendSnapshotMsg struct {
	Type   string        `json:"type"`
	SelfID string        `json:"self_id"`
	Users  []backendUser `json:"users"`
}

type backendUserMsg struct {
	Type      string       `json:"type"`
	User      *backendUser `json:"user,omitempty"`
	ServerID  string       `json:"server_id,omitempty"`
	ChannelID string       `json:"channel_id,omitempty"`
	Message   string       `json:"message,omitempty"`
	MsgID     int64        `json:"msg_id,omitempty"`
	Ts        int64        `json:"ts,omitempty"`
	Error     string       `json:"error,omitempty"`
}

// Metrics holds connection quality metrics shown in the UI.
type Metrics struct {
	RTTMs           float64 `json:"rtt_ms"`
	PacketLoss      float64 `json:"packet_loss"`      // 0.0–1.0
	JitterMs        float64 `json:"jitter_ms"`        // inter-arrival jitter (smoothed)
	BitrateKbps     float64 `json:"bitrate_kbps"`     // measured outgoing audio
	OpusTargetKbps  int     `json:"opus_target_kbps"` // current encoder target
	QualityLevel    string  `json:"quality_level"`    // "good", "moderate", or "poor"
	CaptureDropped  uint64  `json:"capture_dropped"`  // frames dropped on send side since last tick
	PlaybackDropped uint64  `json:"playback_dropped"` // frames dropped on recv side since last tick
}

// qualityLevel classifies connection quality from metrics.
// Thresholds: good (loss<2%, RTT<100ms, jitter<20ms, drops<1/s),
// moderate (loss<10%, RTT<300ms, jitter<50ms, drops<5/s), poor (everything else).
// dropRate is the combined capture+playback drops per second.
func qualityLevel(loss, rttMs, jitterMs, dropRate float64) string {
	if loss >= 0.10 || rttMs >= 300 || jitterMs >= 50 || dropRate >= 5 {
		return "poor"
	}
	if loss >= 0.02 || rttMs >= 100 || jitterMs >= 20 || dropRate >= 1 {
		return "moderate"
	}
	return "good"
}

type peerState struct {
	id      uint16
	pc      *webrtc.PeerConnection
	track   *webrtc.TrackLocalStaticSample
	trackID string

	mu         sync.Mutex
	pendingICE []webrtc.ICECandidateInit
}

// Transport manages the websocket signaling channel and WebRTC media peers.
// It implements the Transporter interface.
type Transport struct {
	mu        sync.Mutex
	ws        *websocket.Conn
	cancel    context.CancelFunc
	myID      uint16
	myChannel atomic.Int64

	// Control write serialization.
	ctrlMu sync.Mutex

	// RTT: smoothed via EWMA (RFC 6298), stored as float64 bits for atomic access.
	smoothedRTT atomic.Uint64
	lastPingTs  atomic.Int64 // Unix ms of the last ping sent

	// lastPongTime records when the most recent pong was received (Unix nanoseconds).
	// Initialised to the connection start time; 0 means never received.
	lastPongTime atomic.Int64

	// Bytes sent since the last GetMetrics call (for bitrate calculation).
	bytesSent atomic.Uint64

	// Packet loss accounting via incoming sequence-gap detection.
	lostPackets     atomic.Uint64
	expectedPackets atomic.Uint64

	// Inter-arrival jitter: EWMA of |actual_gap - 20ms| across all senders,
	// stored as float64 bits for atomic access. Units: milliseconds.
	smoothedJitter atomic.Uint64

	// Dropped frame counters: incremented when the playback channel is full
	// and a received frame cannot be delivered.
	playbackDropped atomic.Uint64

	// muted holds the set of remote user IDs whose audio is suppressed locally.
	muted mutedSet

	// userVolume stores per-user volume multipliers (uint16 -> float64).
	// Default (absent) means 1.0. Range is [0.0, 2.0] (0%-200%).
	userVolume sync.Map

	// recvCancel cancels the current StartReceiving lifecycle (if any).
	recvCancel context.CancelFunc

	// disconnectReason is set before Disconnect is called to communicate the
	// cause to the onDisconnected callback. Protected by mu.
	disconnectReason string

	// lastMetricsTime is the timestamp of the previous GetMetrics call.
	metricsMu       sync.Mutex
	lastMetricsTime time.Time

	// serverAddr is the normalized host:port passed to Connect.
	serverAddr string // protected by mu
	serverID   string // protected by mu; backend server_id routing key

	// apiBaseURL is the HTTP base URL for the server's REST API (e.g. "http://host:8080").
	// Set from the api_port field in user_list.
	apiBaseURL string // protected by mu

	// playbackCh receives decoded Opus payloads from remote tracks.
	playbackCh chan<- TaggedAudio

	// userChannels tracks the latest channel for each connected user.
	userChannels sync.Map // map[uint16]int64

	// ID/channel mapping for backend protocol compatibility.
	userIDByWire    map[string]uint16 // protected by mu
	wireIDByUser    map[uint16]string // protected by mu
	channelIDByWire map[string]int64  // protected by mu
	wireChannelByID map[int64]string  // protected by mu

	// iceServers holds ICE configuration received from the server in user_list.
	iceServers []ICEServerInfo // protected by mu

	// peers holds one RTCPeerConnection per remote user.
	peers map[uint16]*peerState

	// stats maps for sequence and jitter tracking per sender.
	statsMu      sync.Mutex
	lastSeq      map[uint16]uint16
	hasSeq       map[uint16]bool
	lastSeen     map[uint16]time.Time
	lastArrival  map[uint16]time.Time
	lastSpeaking map[uint16]time.Time
	pruneCounter int

	// Callbacks — set via setters before calling Connect.
	cbMu                 sync.RWMutex
	onUserList           func([]UserInfo)
	onUserJoined         func(uint16, string)
	onUserLeft           func(uint16)
	onAudioReceived      func(uint16)
	onDisconnected       func(reason string)
	onChatMessage        func(msgID uint64, senderID uint16, username, message string, ts int64, fileID int64, fileName string, fileSize int64, mentions []uint16, replyTo uint64, replyPreview *ReplyPreview)
	onChannelChatMessage func(msgID uint64, senderID uint16, channelID int64, username, message string, ts int64, fileID int64, fileName string, fileSize int64, mentions []uint16, replyTo uint64, replyPreview *ReplyPreview)
	onServerInfo         func(name string)
	onKicked             func()
	onOwnerChanged       func(ownerID uint16)
	onChannelList        func([]ChannelInfo)
	onUserChannel        func(userID uint16, channelID int64)
	onLinkPreview        func(msgID uint64, channelID int64, url, title, desc, image, siteName string)
	onUserRenamed        func(userID uint16, username string)
	onMessageEdited      func(msgID uint64, message string, ts int64)
	onMessageDeleted     func(msgID uint64)
	onVideoState         func(userID uint16, active bool, screenShare bool)
	onReactionAdded      func(msgID uint64, emoji string, userID uint16)
	onReactionRemoved    func(msgID uint64, emoji string, userID uint16)
	onUserTyping         func(userID uint16, username string, channelID int64)
	onMessagePinned      func(msgID uint64, channelID int64, userID uint16)
	onMessageUnpinned    func(msgID uint64)
	onRecordingState     func(channelID int64, recording bool, startedBy string)
	onVideoLayers        func(userID uint16, layers []VideoLayer)
	onVideoQualityReq    func(fromUserID uint16, quality string)
	onMessageHistory     func(channelID int64, messages []ChatHistoryMessage)
	onUserVoiceFlags     func(userID uint16, muted, deafened bool)
}

// Verify Transport satisfies the Transporter interface at compile time.
var _ Transporter = (*Transport)(nil)

// NewTransport creates a ready-to-use Transport.
func NewTransport() *Transport {
	return &Transport{
		lastMetricsTime: time.Now(),
		peers:           make(map[uint16]*peerState),
		lastSeq:         make(map[uint16]uint16),
		hasSeq:          make(map[uint16]bool),
		lastSeen:        make(map[uint16]time.Time),
		lastArrival:     make(map[uint16]time.Time),
		lastSpeaking:    make(map[uint16]time.Time),
		userIDByWire:    make(map[string]uint16),
		wireIDByUser:    make(map[uint16]string),
		channelIDByWire: make(map[string]int64),
		wireChannelByID: make(map[int64]string),
	}
}

// --- Callback setters (satisfy Transporter interface) ---

func (t *Transport) SetOnUserList(fn func([]UserInfo)) {
	t.cbMu.Lock()
	t.onUserList = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnUserJoined(fn func(uint16, string)) {
	t.cbMu.Lock()
	t.onUserJoined = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnUserLeft(fn func(uint16)) {
	t.cbMu.Lock()
	t.onUserLeft = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnAudioReceived(fn func(uint16)) {
	t.cbMu.Lock()
	t.onAudioReceived = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnDisconnected(fn func(reason string)) {
	t.cbMu.Lock()
	t.onDisconnected = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnChatMessage(fn func(msgID uint64, senderID uint16, username, message string, ts int64, fileID int64, fileName string, fileSize int64, mentions []uint16, replyTo uint64, replyPreview *ReplyPreview)) {
	t.cbMu.Lock()
	t.onChatMessage = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnChannelChatMessage(fn func(msgID uint64, senderID uint16, channelID int64, username, message string, ts int64, fileID int64, fileName string, fileSize int64, mentions []uint16, replyTo uint64, replyPreview *ReplyPreview)) {
	t.cbMu.Lock()
	t.onChannelChatMessage = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnServerInfo(fn func(name string)) {
	t.cbMu.Lock()
	t.onServerInfo = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnKicked(fn func()) {
	t.cbMu.Lock()
	t.onKicked = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnOwnerChanged(fn func(ownerID uint16)) {
	t.cbMu.Lock()
	t.onOwnerChanged = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnChannelList(fn func([]ChannelInfo)) {
	t.cbMu.Lock()
	t.onChannelList = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnUserChannel(fn func(userID uint16, channelID int64)) {
	t.cbMu.Lock()
	t.onUserChannel = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnLinkPreview(fn func(msgID uint64, channelID int64, url, title, desc, image, siteName string)) {
	t.cbMu.Lock()
	t.onLinkPreview = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnUserRenamed(fn func(userID uint16, username string)) {
	t.cbMu.Lock()
	t.onUserRenamed = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnMessageEdited(fn func(msgID uint64, message string, ts int64)) {
	t.cbMu.Lock()
	t.onMessageEdited = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnMessageDeleted(fn func(msgID uint64)) {
	t.cbMu.Lock()
	t.onMessageDeleted = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnVideoState(fn func(userID uint16, active bool, screenShare bool)) {
	t.cbMu.Lock()
	t.onVideoState = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnReactionAdded(fn func(msgID uint64, emoji string, userID uint16)) {
	t.cbMu.Lock()
	t.onReactionAdded = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnReactionRemoved(fn func(msgID uint64, emoji string, userID uint16)) {
	t.cbMu.Lock()
	t.onReactionRemoved = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnUserTyping(fn func(userID uint16, username string, channelID int64)) {
	t.cbMu.Lock()
	t.onUserTyping = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnMessagePinned(fn func(msgID uint64, channelID int64, userID uint16)) {
	t.cbMu.Lock()
	t.onMessagePinned = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnMessageUnpinned(fn func(msgID uint64)) {
	t.cbMu.Lock()
	t.onMessageUnpinned = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnRecordingState(fn func(channelID int64, recording bool, startedBy string)) {
	t.cbMu.Lock()
	t.onRecordingState = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnVideoLayers(fn func(userID uint16, layers []VideoLayer)) {
	t.cbMu.Lock()
	t.onVideoLayers = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnVideoQualityRequest(fn func(fromUserID uint16, quality string)) {
	t.cbMu.Lock()
	t.onVideoQualityReq = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnMessageHistory(fn func(channelID int64, messages []ChatHistoryMessage)) {
	t.cbMu.Lock()
	t.onMessageHistory = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnUserVoiceFlags(fn func(userID uint16, muted, deafened bool)) {
	t.cbMu.Lock()
	t.onUserVoiceFlags = fn
	t.cbMu.Unlock()
}

// SendVoiceFlags sends a set_voice_state message to the server.
func (t *Transport) SendVoiceFlags(muted, deafened bool) error {
	return t.writeJSON(map[string]any{
		"type":     "set_voice_state",
		"muted":    muted,
		"deafened": deafened,
	})
}

// --- Per-user local muting ---

// MuteUser suppresses incoming audio from the given remote user ID.
func (t *Transport) MuteUser(id uint16) { t.muted.Add(id) }

// UnmuteUser re-enables incoming audio from the given remote user ID.
func (t *Transport) UnmuteUser(id uint16) { t.muted.Remove(id) }

// IsUserMuted reports whether audio from id is currently suppressed.
func (t *Transport) IsUserMuted(id uint16) bool { return t.muted.Has(id) }

// MutedUsers returns the IDs of all currently muted remote users.
func (t *Transport) MutedUsers() []uint16 { return t.muted.Slice() }

// SetUserVolume sets the local playback volume multiplier for a remote user.
// volume is in [0.0, 2.0] representing 0%-200%. Default (unset) is 1.0.
func (t *Transport) SetUserVolume(id uint16, volume float64) {
	if volume < 0 {
		volume = 0
	}
	if volume > 2.0 {
		volume = 2.0
	}
	t.userVolume.Store(id, volume)
}

// GetUserVolume returns the local playback volume multiplier for a remote user.
// Returns 1.0 if not explicitly set.
func (t *Transport) GetUserVolume(id uint16) float64 {
	v, ok := t.userVolume.Load(id)
	if !ok {
		return 1.0
	}
	return v.(float64)
}

// KickUser sends a kick request to the server. Only succeeds if the caller is
// the channel owner; the server enforces the authorisation check.
func (t *Transport) KickUser(id uint16) error {
	return t.writeCtrl(ControlMsg{Type: "kick", ID: id})
}

// RenameServer sends a rename request to the server. Only succeeds if the
// caller is the channel owner; the server enforces the authorisation check.
func (t *Transport) RenameServer(name string) error {
	return t.writeCtrl(ControlMsg{Type: "rename", ServerName: name})
}

// JoinChannel sends a join_channel request to the server.
// Pass channelID=0 to leave all channels (return to lobby).
func (t *Transport) JoinChannel(id int64) error {
	if id == 0 {
		return t.writeJSON(map[string]any{"type": "DisconnectVoice"})
	}
	return t.writeJSON(map[string]any{
		"type":       "join_voice",
		"server_id":  t.backendServerID(),
		"channel_id": t.wireChannelID(id),
	})
}

// CreateChannel asks the server to create a new channel with the given name.
// Only succeeds if the caller is the channel owner; the server enforces the check.
func (t *Transport) CreateChannel(name string) error {
	return t.writeCtrl(ControlMsg{Type: "create_channel", Message: name})
}

// RenameChannel asks the server to rename a channel.
// Only succeeds if the caller is the channel owner; the server enforces the check.
func (t *Transport) RenameChannel(id int64, name string) error {
	return t.writeCtrl(ControlMsg{Type: "rename_channel", ChannelID: id, Message: name})
}

// DeleteChannel asks the server to delete a channel.
// Only succeeds if the caller is the channel owner; the server enforces the check.
func (t *Transport) DeleteChannel(id int64) error {
	return t.writeCtrl(ControlMsg{Type: "delete_channel", ChannelID: id})
}

// MoveUser asks the server to move a user to a different channel.
// Only succeeds if the caller is the channel owner; the server enforces the check.
func (t *Transport) MoveUser(userID uint16, channelID int64) error {
	return t.writeCtrl(ControlMsg{Type: "move_user", ID: userID, ChannelID: channelID})
}

// RenameUser sends a rename_user request so the server updates our username
// for future chat messages and notifies other clients.
func (t *Transport) RenameUser(name string) error {
	return t.writeCtrl(ControlMsg{Type: "rename_user", Username: name})
}

// SendVideoState tells the server (and thus all peers) whether we have video
// active and whether it's a screen share. The server broadcasts a video_state
// message with our authoritative ID.
func (t *Transport) SendVideoState(active bool, screenShare bool) error {
	return t.writeCtrl(ControlMsg{
		Type:        "video_state",
		VideoActive: &active,
		ScreenShare: &screenShare,
	})
}

// RequestVideoQuality asks the server to relay a quality request to a video
// sender. The sender can then adjust their encoder or select a simulcast layer.
// quality must be "high", "medium", or "low".
func (t *Transport) RequestVideoQuality(targetID uint16, quality string) error {
	return t.writeCtrl(ControlMsg{Type: "set_video_quality", TargetID: targetID, VideoQuality: quality})
}

// StartRecording asks the server to start recording voice in a channel.
// Only succeeds if the caller is the channel owner; the server enforces the check.
func (t *Transport) StartRecording(channelID int64) error {
	return t.writeCtrl(ControlMsg{Type: "start_recording", ChannelID: channelID})
}

// StopRecording asks the server to stop recording voice in a channel.
// Only succeeds if the caller is the channel owner; the server enforces the check.
func (t *Transport) StopRecording(channelID int64) error {
	return t.writeCtrl(ControlMsg{Type: "stop_recording", ChannelID: channelID})
}

// RequestChannels asks the server to send the channel list for the connected server.
func (t *Transport) RequestChannels() error {
	return t.writeJSON(map[string]any{"type": "get_channels"})
}

// RequestMessages asks the server to send message history for a channel.
func (t *Transport) RequestMessages(channelID int64) error {
	return t.writeJSON(map[string]any{
		"type":       "get_messages",
		"channel_id": t.wireChannelID(channelID),
	})
}

// RequestServerInfo asks the server to send its name and metadata.
func (t *Transport) RequestServerInfo() error {
	return t.writeJSON(map[string]any{"type": "get_server_info"})
}

// EditMessage asks the server to update a message's text. Only the original
// sender is allowed to edit; the server enforces the authorisation check.
func (t *Transport) EditMessage(msgID uint64, message string) error {
	if err := validateChat(message); err != nil {
		return err
	}
	return t.writeCtrl(ControlMsg{Type: "edit_message", MsgID: msgID, Message: message})
}

// DeleteMessage asks the server to delete a message. The original sender
// and the channel owner are allowed to delete; the server enforces the check.
func (t *Transport) DeleteMessage(msgID uint64) error {
	return t.writeCtrl(ControlMsg{Type: "delete_message", MsgID: msgID})
}

// APIBaseURL returns the HTTP base URL for the server's REST API, or "" if not yet known.
func (t *Transport) APIBaseURL() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.apiBaseURL
}

// SendFileChat sends a chat message with a file attachment.
// The file metadata must come from a prior upload to the server's API.
func (t *Transport) SendFileChat(channelID, fileID, fileSize int64, fileName, message string) error {
	_ = channelID
	_ = fileID
	_ = fileSize
	_ = fileName
	_ = message
	return fmt.Errorf("file chat is not supported by this backend")
}

// SendChannelChat sends a channel-scoped chat message.
func (t *Transport) SendChannelChat(channelID int64, message string) error {
	if err := validateChat(message); err != nil {
		return err
	}
	return t.writeJSON(map[string]any{
		"type":       "send_text",
		"server_id":  t.backendServerID(),
		"channel_id": t.wireChannelID(channelID),
		"message":    message,
	})
}

// SendChat sends a chat message to the server for fan-out to all participants.
func (t *Transport) SendChat(message string) error {
	if err := validateChat(message); err != nil {
		return err
	}
	return t.writeJSON(map[string]any{
		"type":       "send_text",
		"server_id":  t.backendServerID(),
		"channel_id": t.wireChannelID(1),
		"message":    message,
	})
}

// AddReaction adds an emoji reaction to a message.
func (t *Transport) AddReaction(msgID uint64, emoji string) error {
	if emoji == "" {
		return fmt.Errorf("emoji must not be empty")
	}
	return t.writeCtrl(ControlMsg{Type: "add_reaction", MsgID: msgID, Emoji: emoji})
}

// RemoveReaction removes an emoji reaction from a message.
func (t *Transport) RemoveReaction(msgID uint64, emoji string) error {
	if emoji == "" {
		return fmt.Errorf("emoji must not be empty")
	}
	return t.writeCtrl(ControlMsg{Type: "remove_reaction", MsgID: msgID, Emoji: emoji})
}

// SendTyping notifies the server that the user is typing in a channel.
func (t *Transport) SendTyping(channelID int64) error {
	if channelID == 0 {
		return fmt.Errorf("channel_id must not be zero")
	}
	return nil
}

// validateChat returns an error if the message is empty or too long.
func validateChat(message string) error {
	if message == "" {
		return fmt.Errorf("message must not be empty")
	}
	if len(message) > 500 {
		return fmt.Errorf("message must not exceed 500 characters")
	}
	return nil
}

func (t *Transport) writeJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	t.ctrlMu.Lock()
	defer t.ctrlMu.Unlock()
	if t.ws == nil {
		return fmt.Errorf("control websocket not connected")
	}
	_ = t.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := t.ws.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("websocket write: %w", err)
	}
	return nil
}

// writeCtrl serialises a control message write; safe for concurrent callers.
func (t *Transport) writeCtrl(msg ControlMsg) error {
	return t.writeJSON(msg)
}

// writeCtrlBestEffort sends a control message without returning errors.
// Used for non-critical messages where failure is handled elsewhere.
func (t *Transport) writeCtrlBestEffort(msg ControlMsg) {
	if err := t.writeCtrl(msg); err != nil {
		slog.Debug("best-effort write failed", "type", msg.Type, "err", err)
	}
}

func (t *Transport) backendServerID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.serverID == "" {
		t.serverID = "default"
	}
	return t.serverID
}

func (t *Transport) wireChannelID(channelID int64) string {
	if channelID == 0 {
		return "0"
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if ch, ok := t.wireChannelByID[channelID]; ok {
		return ch
	}
	ch := strconv.FormatInt(channelID, 10)
	t.wireChannelByID[channelID] = ch
	t.channelIDByWire[ch] = channelID
	return ch
}

func (t *Transport) localChannelID(wire string) int64 {
	wire = strings.TrimSpace(wire)
	if wire == "" || wire == "0" {
		return 0
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if id, ok := t.channelIDByWire[wire]; ok {
		return id
	}

	if n, err := strconv.ParseInt(wire, 10, 64); err == nil && n != 0 {
		t.channelIDByWire[wire] = n
		t.wireChannelByID[n] = wire
		return n
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(wire))
	id := int64(h.Sum32())
	if id == 0 {
		id = 1
	}
	if id < 0 {
		id = -id
	}
	for {
		if _, used := t.wireChannelByID[id]; !used {
			break
		}
		id++
	}
	t.channelIDByWire[wire] = id
	t.wireChannelByID[id] = wire
	return id
}

func (t *Transport) localUserID(wire string) uint16 {
	wire = strings.TrimSpace(wire)
	if wire == "" {
		return 0
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if id, ok := t.userIDByWire[wire]; ok {
		return id
	}

	var candidate uint16
	raw := wire
	if strings.HasPrefix(raw, "u") {
		raw = strings.TrimPrefix(raw, "u")
	}
	if n, err := strconv.ParseUint(raw, 10, 16); err == nil && n > 0 {
		candidate = uint16(n)
		if existing, used := t.wireIDByUser[candidate]; used && existing != wire {
			candidate = 0
		}
	}
	if candidate == 0 {
		for i := uint16(1); i != 0; i++ {
			if _, used := t.wireIDByUser[i]; !used {
				candidate = i
				break
			}
		}
		if candidate == 0 {
			return 0
		}
	}

	t.userIDByWire[wire] = candidate
	t.wireIDByUser[candidate] = wire
	return candidate
}

// connectTimeout is the maximum time allowed for the initial websocket dial + hello handshake.
const connectTimeout = 10 * time.Second

// dialAddrsForWebsocket returns connection attempts for addr, adding IPv4/IPv6
// loopback fallbacks to avoid localhost resolution mismatches.
func dialAddrsForWebsocket(addr string) []string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return []string{addr}
	}

	out := []string{net.JoinHostPort(host, port)}
	switch host {
	case "localhost":
		out = append(out, net.JoinHostPort("127.0.0.1", port), net.JoinHostPort("::1", port))
	case "127.0.0.1":
		out = append(out, net.JoinHostPort("::1", port))
	case "::1":
		out = append(out, net.JoinHostPort("127.0.0.1", port))
	}

	seen := make(map[string]struct{}, len(out))
	unique := make([]string, 0, len(out))
	for _, a := range out {
		if _, ok := seen[a]; ok {
			continue
		}
		seen[a] = struct{}{}
		unique = append(unique, a)
	}
	return unique
}

// Connect establishes the websocket control/signaling channel and sends hello.
// Callbacks must be registered via Set* methods before calling Connect.
func (t *Transport) Connect(ctx context.Context, addr, username string) error {
	slog.Debug("connecting", "addr", addr, "username", username)
	normalizedAddr, err := normalizeServerAddr(addr)
	if err != nil {
		return err
	}

	// Defensive cleanup in case a stale session exists.
	t.Disconnect()

	// Reset per-session state.
	t.muted.Clear()
	t.clearUserChannels()
	t.resetPeerStats()

	t.mu.Lock()
	t.disconnectReason = ""
	t.serverAddr = normalizedAddr
	t.serverID = normalizedAddr
	t.apiBaseURL = "http://" + normalizedAddr
	t.myID = 0
	t.myChannel.Store(0)
	t.userIDByWire = make(map[string]uint16)
	t.wireIDByUser = make(map[uint16]string)
	t.channelIDByWire = make(map[string]int64)
	t.wireChannelByID = make(map[int64]string)
	t.mu.Unlock()

	dialCtx, dialCancel := context.WithTimeout(ctx, connectTimeout)
	defer dialCancel()

	sessionCtx, cancel := context.WithCancel(ctx)

	d := websocket.Dialer{HandshakeTimeout: connectTimeout}

	var conn *websocket.Conn
	for _, dialAddr := range dialAddrsForWebsocket(normalizedAddr) {
		slog.Debug("dialing websocket", "addr", dialAddr)
		conn, _, err = d.DialContext(dialCtx, "ws://"+dialAddr+"/ws", nil)
		if err == nil {
			break
		}
	}
	if err != nil {
		cancel()
		return err
	}

	t.mu.Lock()
	t.ws = conn
	slog.Debug("websocket connected", "addr", normalizedAddr)
	t.cancel = cancel
	t.mu.Unlock()

	// Reset per-session metrics.
	t.smoothedRTT.Store(0)
	t.smoothedJitter.Store(0)
	t.bytesSent.Store(0)
	t.lostPackets.Store(0)
	t.expectedPackets.Store(0)
	t.lastPongTime.Store(time.Now().UnixNano())
	t.metricsMu.Lock()
	t.lastMetricsTime = time.Now()
	t.metricsMu.Unlock()

	if err := t.writeJSON(map[string]any{
		"type":     "hello",
		"username": username,
	}); err != nil {
		t.Disconnect()
		return fmt.Errorf("send hello: %w", err)
	}
	slog.Debug("hello sent", "username", username)
	if err := t.writeJSON(map[string]any{
		"type":      "connect_server",
		"server_id": t.backendServerID(),
	}); err != nil {
		t.Disconnect()
		return fmt.Errorf("connect server: %w", err)
	}

	go t.readControl(sessionCtx, conn)
	go t.pingLoop(sessionCtx)

	return nil
}

// Disconnect closes the websocket and all peer connections.
func (t *Transport) Disconnect() {
	slog.Debug("disconnecting")
	t.ctrlMu.Lock()
	ws := t.ws
	t.ws = nil
	t.ctrlMu.Unlock()

	if ws != nil {
		_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "disconnect"), time.Now().Add(250*time.Millisecond))
		_ = ws.Close()
	}

	var peers []*peerState

	t.mu.Lock()
	if t.recvCancel != nil {
		t.recvCancel()
		t.recvCancel = nil
	}
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	for _, p := range t.peers {
		peers = append(peers, p)
	}
	t.peers = make(map[uint16]*peerState)
	t.myID = 0
	t.myChannel.Store(0)
	t.playbackCh = nil
	t.mu.Unlock()

	for _, p := range peers {
		_ = p.pc.Close()
	}
	slog.Debug("peers closed", "count", len(peers))

	t.clearUserChannels()
	t.resetPeerStats()
}

func (t *Transport) clearUserChannels() {
	t.userChannels.Range(func(k, _ any) bool {
		t.userChannels.Delete(k)
		return true
	})
}

func (t *Transport) resetPeerStats() {
	t.statsMu.Lock()
	t.lastSeq = make(map[uint16]uint16)
	t.hasSeq = make(map[uint16]bool)
	t.lastSeen = make(map[uint16]time.Time)
	t.lastArrival = make(map[uint16]time.Time)
	t.lastSpeaking = make(map[uint16]time.Time)
	t.pruneCounter = 0
	t.statsMu.Unlock()
}

// SendAudio writes an Opus frame to every active WebRTC peer in the same voice channel.
func (t *Transport) SendAudio(opusData []byte) error {
	if len(opusData) == 0 {
		return nil
	}

	myChannel := t.myChannel.Load()
	if myChannel == 0 {
		return nil
	}

	t.mu.Lock()
	if len(t.peers) == 0 {
		t.mu.Unlock()
		return nil
	}
	peers := make([]*peerState, 0, len(t.peers))
	for _, p := range t.peers {
		peers = append(peers, p)
	}
	t.mu.Unlock()

	var firstErr error
	for _, p := range peers {
		if !t.peerInMyChannel(p.id, myChannel) {
			continue
		}
		sample := media.Sample{
			Data:     append([]byte(nil), opusData...),
			Duration: 20 * time.Millisecond,
		}
		if err := p.track.WriteSample(sample); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		t.bytesSent.Add(uint64(len(opusData)))
	}
	return firstErr
}

func (t *Transport) peerInMyChannel(peerID uint16, myChannel int64) bool {
	if myChannel == 0 {
		return false
	}
	v, ok := t.userChannels.Load(peerID)
	if !ok {
		return false
	}
	peerChannel, ok := v.(int64)
	if !ok {
		return false
	}
	if peerChannel == 0 {
		return false
	}
	return peerChannel == myChannel
}

// MyID returns the local client's server-assigned user ID (0 before join ack).
func (t *Transport) MyID() uint16 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.myID
}

// StartReceiving stores the playback channel used by incoming WebRTC tracks.
func (t *Transport) StartReceiving(ctx context.Context, playbackCh chan<- TaggedAudio) {
	slog.Debug("start receiving")
	t.mu.Lock()
	if t.ws == nil {
		t.mu.Unlock()
		return
	}
	if t.recvCancel != nil {
		t.recvCancel()
	}
	_, cancel := context.WithCancel(ctx)
	t.recvCancel = cancel
	t.playbackCh = playbackCh
	t.mu.Unlock()
}

// buildICEServers converts ICEServerInfo from the server into pion's
// webrtc.ICEServer slice. Falls back to Google STUN if none were provided.
// Caller must hold t.mu.
func (t *Transport) buildICEServers() []webrtc.ICEServer {
	if len(t.iceServers) == 0 {
		return []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		}
	}
	servers := make([]webrtc.ICEServer, 0, len(t.iceServers))
	for _, s := range t.iceServers {
		ice := webrtc.ICEServer{URLs: s.URLs}
		if s.Username != "" {
			ice.Username = s.Username
		}
		if s.Credential != "" {
			ice.Credential = s.Credential
		}
		servers = append(servers, ice)
	}
	return servers
}

func (t *Transport) ensurePeer(remoteID uint16) (*peerState, bool) {
	if remoteID == 0 {
		return nil, false
	}

	t.mu.Lock()
	if existing, ok := t.peers[remoteID]; ok {
		t.mu.Unlock()
		return existing, false
	}
	myID := t.myID
	iceServers := t.buildICEServers()
	t.mu.Unlock()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: iceServers,
	})
	if err != nil {
		slog.Error("create peer", "remote_id", remoteID, "err", err)
		return nil, false
	}

	trackID := fmt.Sprintf("audio-%d-to-%d", myID, remoteID)
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 1},
		trackID,
		"bken",
	)
	if err != nil {
		_ = pc.Close()
		slog.Error("create local track", "remote_id", remoteID, "err", err)
		return nil, false
	}

	sender, err := pc.AddTrack(track)
	if err != nil {
		_ = pc.Close()
		slog.Error("add track", "remote_id", remoteID, "err", err)
		return nil, false
	}

	// Drain RTCP so interceptors do not back up.
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, err := sender.Read(rtcpBuf); err != nil {
				return
			}
		}
	}()

	peer := &peerState{
		id:      remoteID,
		pc:      pc,
		track:   track,
		trackID: trackID,
	}

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		ice := c.ToJSON()
		msg := ControlMsg{Type: "webrtc_ice", TargetID: remoteID, Candidate: ice.Candidate}
		if ice.SDPMid != nil {
			msg.SDPMid = *ice.SDPMid
		}
		if ice.SDPMLineIndex != nil {
			idx := uint16(*ice.SDPMLineIndex)
			msg.SDPMLineIndex = &idx
		}
		t.writeCtrlBestEffort(msg)
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			t.closePeer(remoteID)
		}
	})

	pc.OnTrack(func(remoteTrack *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if remoteTrack.Codec().MimeType != webrtc.MimeTypeOpus {
			return
		}
		go t.readRemoteTrack(remoteID, remoteTrack)
	})

	t.mu.Lock()
	if existing, ok := t.peers[remoteID]; ok {
		t.mu.Unlock()
		_ = pc.Close()
		return existing, false
	}
	t.peers[remoteID] = peer
	t.mu.Unlock()

	return peer, true
}

func (t *Transport) closePeer(remoteID uint16) {
	var peer *peerState
	t.mu.Lock()
	if p, ok := t.peers[remoteID]; ok {
		peer = p
		delete(t.peers, remoteID)
	}
	t.mu.Unlock()

	if peer != nil {
		_ = peer.pc.Close()
	}

	t.statsMu.Lock()
	delete(t.lastSeq, remoteID)
	delete(t.hasSeq, remoteID)
	delete(t.lastSeen, remoteID)
	delete(t.lastArrival, remoteID)
	delete(t.lastSpeaking, remoteID)
	t.statsMu.Unlock()
}

func (t *Transport) createAndSendOffer(remoteID uint16) {
	peer, _ := t.ensurePeer(remoteID)
	if peer == nil {
		return
	}

	offer, err := peer.pc.CreateOffer(nil)
	if err != nil {
		slog.Error("create offer", "remote_id", remoteID, "err", err)
		return
	}
	if err := peer.pc.SetLocalDescription(offer); err != nil {
		slog.Error("set local offer", "remote_id", remoteID, "err", err)
		return
	}
	t.writeCtrlBestEffort(ControlMsg{Type: "webrtc_offer", TargetID: remoteID, SDP: offer.SDP})
}

func (t *Transport) handleOffer(senderID uint16, sdp string) {
	peer, _ := t.ensurePeer(senderID)
	if peer == nil {
		return
	}

	offer := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp}
	if err := peer.pc.SetRemoteDescription(offer); err != nil {
		slog.Error("set remote offer", "sender_id", senderID, "err", err)
		return
	}
	t.flushPendingICE(peer)

	answer, err := peer.pc.CreateAnswer(nil)
	if err != nil {
		slog.Error("create answer", "sender_id", senderID, "err", err)
		return
	}
	if err := peer.pc.SetLocalDescription(answer); err != nil {
		slog.Error("set local answer", "sender_id", senderID, "err", err)
		return
	}
	t.writeCtrlBestEffort(ControlMsg{Type: "webrtc_answer", TargetID: senderID, SDP: answer.SDP})
}

func (t *Transport) handleAnswer(senderID uint16, sdp string) {
	peer, _ := t.ensurePeer(senderID)
	if peer == nil {
		return
	}

	answer := webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp}
	if err := peer.pc.SetRemoteDescription(answer); err != nil {
		slog.Error("set remote answer", "sender_id", senderID, "err", err)
		return
	}
	t.flushPendingICE(peer)
}

func (t *Transport) handleICE(senderID uint16, cand ControlMsg) {
	if cand.Candidate == "" {
		return
	}
	peer, _ := t.ensurePeer(senderID)
	if peer == nil {
		return
	}

	init := webrtc.ICECandidateInit{Candidate: cand.Candidate}
	if cand.SDPMid != "" {
		mid := cand.SDPMid
		init.SDPMid = &mid
	}
	if cand.SDPMLineIndex != nil {
		idx := *cand.SDPMLineIndex
		init.SDPMLineIndex = &idx
	}

	if peer.pc.RemoteDescription() == nil {
		peer.mu.Lock()
		peer.pendingICE = append(peer.pendingICE, init)
		peer.mu.Unlock()
		return
	}

	if err := peer.pc.AddICECandidate(init); err != nil {
		slog.Warn("add ICE candidate failed", "sender_id", senderID, "err", err)
	}
}

func (t *Transport) flushPendingICE(peer *peerState) {
	peer.mu.Lock()
	pending := peer.pendingICE
	peer.pendingICE = nil
	peer.mu.Unlock()

	for _, c := range pending {
		if err := peer.pc.AddICECandidate(c); err != nil {
			slog.Warn("add pending ICE candidate failed", "peer_id", peer.id, "err", err)
		}
	}
}

func (t *Transport) readRemoteTrack(senderID uint16, tr *webrtc.TrackRemote) {
	for {
		pkt, _, err := tr.ReadRTP()
		if err != nil {
			return
		}
		if len(pkt.Payload) == 0 {
			continue
		}
		t.handleIncomingAudio(senderID, pkt.SequenceNumber, pkt.Payload)
	}
}

func (t *Transport) handleIncomingAudio(senderID uint16, seq uint16, payload []byte) {
	if t.muted.Has(senderID) {
		return
	}
	if !t.canHear(senderID) {
		return
	}

	now := time.Now()
	shouldNotifySpeaking := false

	const expectedGapMs = 20.0
	const jitterAlpha = 1.0 / 16.0

	t.statsMu.Lock()
	t.lastSeen[senderID] = now

	forwardProgress := false
	if prev, has := t.lastSeq[senderID]; has && t.hasSeq[senderID] {
		diff := int(seq) - int(prev)
		if diff < 0 {
			diff += 65536
		}
		if diff > 0 && diff < 1000 {
			forwardProgress = true
			t.lastSeq[senderID] = seq
			t.expectedPackets.Add(uint64(diff))
			if diff > 1 {
				t.lostPackets.Add(uint64(diff - 1))
			}
		}
	} else {
		forwardProgress = true
		t.lastSeq[senderID] = seq
		t.hasSeq[senderID] = true
	}

	if forwardProgress {
		if prevArrival, ok := t.lastArrival[senderID]; ok {
			gapMs := float64(now.Sub(prevArrival).Microseconds()) / 1000.0
			if gapMs < 100.0 {
				d := gapMs - expectedGapMs
				if d < 0 {
					d = -d
				}
				old := math.Float64frombits(t.smoothedJitter.Load())
				next := old + jitterAlpha*(d-old)
				t.smoothedJitter.Store(math.Float64bits(next))
			}
		}
		t.lastArrival[senderID] = now
	}

	if last, ok := t.lastSpeaking[senderID]; !ok || now.Sub(last) > 80*time.Millisecond {
		t.lastSpeaking[senderID] = now
		shouldNotifySpeaking = true
	}

	t.pruneCounter++
	if t.pruneCounter >= 500 {
		t.pruneCounter = 0
		for id, seen := range t.lastSeen {
			if now.Sub(seen) > 30*time.Second {
				delete(t.lastSeen, id)
				delete(t.lastSeq, id)
				delete(t.hasSeq, id)
				delete(t.lastArrival, id)
				delete(t.lastSpeaking, id)
			}
		}
	}

	t.statsMu.Unlock()

	if shouldNotifySpeaking {
		t.cbMu.RLock()
		onAudio := t.onAudioReceived
		t.cbMu.RUnlock()
		if onAudio != nil {
			onAudio(senderID)
		}
	}

	frame := append([]byte(nil), payload...)
	t.mu.Lock()
	playbackCh := t.playbackCh
	t.mu.Unlock()
	if playbackCh == nil {
		return
	}

	select {
	case playbackCh <- TaggedAudio{SenderID: senderID, Seq: seq, OpusData: frame}:
	default:
		t.playbackDropped.Add(1)
	}
}

func (t *Transport) canHear(peerID uint16) bool {
	myChannel := t.myChannel.Load()
	if myChannel == 0 {
		return false
	}
	v, ok := t.userChannels.Load(peerID)
	if !ok {
		return false
	}
	peerChannel, ok := v.(int64)
	if !ok {
		return false
	}
	if peerChannel == 0 {
		return false
	}
	return peerChannel == myChannel
}

func (t *Transport) ensurePeersFromUserList(users []UserInfo) {
	myID := t.MyID()
	if myID == 0 {
		return
	}
	for _, u := range users {
		if u.ID == 0 || u.ID == myID {
			continue
		}
		_, created := t.ensurePeer(u.ID)
		if created && myID < u.ID {
			go t.createAndSendOffer(u.ID)
		}
	}
}

// GetMetrics returns current connection quality metrics and resets interval counters.
func (t *Transport) GetMetrics() Metrics {
	now := time.Now()

	t.metricsMu.Lock()
	elapsed := now.Sub(t.lastMetricsTime).Seconds()
	if elapsed <= 0 {
		elapsed = 2
	}
	t.lastMetricsTime = now
	t.metricsMu.Unlock()

	bytes := t.bytesSent.Swap(0)
	bitrate := float64(bytes*8) / elapsed / 1000 // kbps

	lost := t.lostPackets.Swap(0)
	expected := t.expectedPackets.Swap(0)
	var loss float64
	if expected > 0 {
		loss = float64(lost) / float64(expected)
		if loss > 1 {
			loss = 1
		}
	}

	rtt := math.Float64frombits(t.smoothedRTT.Load())
	jitterMs := math.Float64frombits(t.smoothedJitter.Load())
	playbackDrops := t.playbackDropped.Swap(0)

	return Metrics{
		RTTMs:           rtt,
		PacketLoss:      loss,
		JitterMs:        jitterMs,
		BitrateKbps:     bitrate,
		PlaybackDropped: playbackDrops,
		QualityLevel:    qualityLevel(loss, rtt, jitterMs, 0),
	}
}

// pongTimeout is the maximum time allowed between pongs before the connection
// is considered dead and the client disconnects. 3 missed pings at 2 s each.
const pongTimeout = 6 * time.Second

// pingLoop sends a ping every 2 s for RTT measurement and enforces a pong deadline.
func (t *Transport) pingLoop(ctx context.Context) {
	slog.Debug("ping loop started")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ts := time.Now().UnixMilli()
			t.lastPingTs.Store(ts)
			t.writeCtrlBestEffort(ControlMsg{Type: "ping", Ts: ts})

			lastPong := t.lastPongTime.Load()
			if lastPong > 0 && time.Since(time.Unix(0, lastPong)) > pongTimeout {
				slog.Warn("pong timeout, disconnecting")
				t.mu.Lock()
				t.disconnectReason = "Server unreachable (ping timeout)"
				t.mu.Unlock()
				t.Disconnect()
				return
			}
		}
	}
}

// readControl reads JSON control messages from the server websocket.
func (t *Transport) readControl(ctx context.Context, conn *websocket.Conn) {
	_ = ctx
	slog.Debug("read control loop started")

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}

		t.cbMu.RLock()
		onUserList := t.onUserList
		onUserJoined := t.onUserJoined
		onUserLeft := t.onUserLeft
		onChat := t.onChatMessage
		onChannelChat := t.onChannelChatMessage
		onServerInfo := t.onServerInfo
		onKicked := t.onKicked
		onOwnerChanged := t.onOwnerChanged
		onChannelList := t.onChannelList
		onUserChannel := t.onUserChannel
		onLinkPreview := t.onLinkPreview
		onUserRenamed := t.onUserRenamed
		onMessageEdited := t.onMessageEdited
		onMessageDeleted := t.onMessageDeleted
		onVideoState := t.onVideoState
		onReactionAdded := t.onReactionAdded
		onReactionRemoved := t.onReactionRemoved
		onUserTyping := t.onUserTyping
		onMessagePinned := t.onMessagePinned
		onMessageUnpinned := t.onMessageUnpinned
		onRecordingState := t.onRecordingState
		onVideoLayers := t.onVideoLayers
		onVideoQualityReq := t.onVideoQualityReq
		onMessageHistory := t.onMessageHistory
		onUserVoiceFlags := t.onUserVoiceFlags
		t.cbMu.RUnlock()

		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			slog.Error("invalid control message", "err", err)
			continue
		}

		switch header.Type {
		case "snapshot":
			var msg backendSnapshotMsg
			if err := json.Unmarshal(data, &msg); err != nil {
				slog.Error("invalid snapshot message", "err", err)
				continue
			}

			slog.Debug("snapshot received", "self_id", msg.SelfID, "users", len(msg.Users))
			selfID := t.localUserID(msg.SelfID)
			t.mu.Lock()
			t.myID = selfID
			t.mu.Unlock()

			users := make([]UserInfo, 0, len(msg.Users))
			t.clearUserChannels()
			for _, u := range msg.Users {
				id := t.localUserID(u.ID)
				channelID := int64(0)
				if u.Voice != nil {
					channelID = t.localChannelID(u.Voice.ChannelID)
				}
				t.userChannels.Store(id, channelID)
				if id == selfID {
					t.myChannel.Store(channelID)
				}
				users = append(users, UserInfo{ID: id, Username: u.Username, ChannelID: channelID})
			}

			if onUserList != nil {
				onUserList(users)
			}
			if onUserVoiceFlags != nil {
				for _, u := range msg.Users {
					if u.Voice != nil {
						id := t.localUserID(u.ID)
						onUserVoiceFlags(id, u.Voice.Muted, u.Voice.Deafened)
					}
				}
			}
		case "user_joined":
			var msg backendUserMsg
			if err := json.Unmarshal(data, &msg); err != nil {
				slog.Error("invalid user_joined message", "err", err)
				continue
			}
			if msg.User == nil {
				continue
			}
			id := t.localUserID(msg.User.ID)
			channelID := int64(0)
			if msg.User.Voice != nil {
				channelID = t.localChannelID(msg.User.Voice.ChannelID)
			}
			t.userChannels.Store(id, channelID)
			if onUserJoined != nil {
				onUserJoined(id, msg.User.Username)
			}
			if onUserChannel != nil {
				onUserChannel(id, channelID)
			}
		case "user_left":
			var msg backendUserMsg
			if err := json.Unmarshal(data, &msg); err != nil {
				slog.Error("invalid user_left message", "err", err)
				continue
			}
			if msg.User == nil {
				continue
			}
			id := t.localUserID(msg.User.ID)
			t.userChannels.Delete(id)
			t.closePeer(id)
			if onUserLeft != nil {
				onUserLeft(id)
			}
		case "user_state":
			var msg backendUserMsg
			if err := json.Unmarshal(data, &msg); err != nil {
				slog.Error("invalid user_state message", "err", err)
				continue
			}
			if msg.User == nil {
				continue
			}
			id := t.localUserID(msg.User.ID)
			channelID := int64(0)
			if msg.User.Voice != nil {
				channelID = t.localChannelID(msg.User.Voice.ChannelID)
			}
			t.userChannels.Store(id, channelID)
			if id == t.MyID() {
				t.myChannel.Store(channelID)
			}
			if onUserChannel != nil {
				onUserChannel(id, channelID)
			}
			if onUserVoiceFlags != nil && msg.User.Voice != nil {
				onUserVoiceFlags(id, msg.User.Voice.Muted, msg.User.Voice.Deafened)
			}
		case "text_message":
			var msg backendUserMsg
			if err := json.Unmarshal(data, &msg); err != nil {
				slog.Error("invalid text_message message", "err", err)
				continue
			}
			if msg.User == nil {
				continue
			}
			id := t.localUserID(msg.User.ID)
			channelID := t.localChannelID(msg.ChannelID)
			if msg.Ts == 0 {
				msg.Ts = time.Now().UnixMilli()
			}
			msgID := uint64(msg.MsgID)
			if channelID != 0 {
				if onChannelChat != nil {
					onChannelChat(msgID, id, channelID, msg.User.Username, msg.Message, msg.Ts, 0, "", 0, nil, 0, nil)
				}
			} else if onChat != nil {
				onChat(msgID, id, msg.User.Username, msg.Message, msg.Ts, 0, "", 0, nil, 0, nil)
			}
		case "message_history":
			var msg struct {
				ChannelID string `json:"channel_id"`
				Messages  []struct {
					MsgID    int64  `json:"msg_id"`
					Username string `json:"username"`
					Message  string `json:"message"`
					TS       int64  `json:"ts"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				slog.Error("invalid message_history message", "err", err)
				continue
			}
			channelID := t.localChannelID(msg.ChannelID)
			msgs := make([]ChatHistoryMessage, len(msg.Messages))
			for i, m := range msg.Messages {
				msgs[i] = ChatHistoryMessage{
					MsgID:    m.MsgID,
					Username: m.Username,
					Message:  m.Message,
					TS:       m.TS,
				}
			}
			if onMessageHistory != nil {
				onMessageHistory(channelID, msgs)
			}
		case "channel_list":
			var msg struct {
				Channels []ChannelInfo `json:"channels"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				slog.Error("invalid channel_list message", "err", err)
				continue
			}
			if onChannelList != nil {
				onChannelList(msg.Channels)
			}
		case "server_info":
			var msg struct {
				ServerName string `json:"server_name"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				slog.Error("invalid server_info message", "err", err)
				continue
			}
			if msg.ServerName != "" && onServerInfo != nil {
				onServerInfo(msg.ServerName)
			}
		case "pong":
			t.lastPongTime.Store(time.Now().UnixNano())
			sent := t.lastPingTs.Load()
			if sent != 0 {
				sample := float64(time.Now().UnixMilli() - sent)
				old := math.Float64frombits(t.smoothedRTT.Load())
				var next float64
				if old == 0 {
					next = sample
				} else {
					next = 0.125*sample + 0.875*old
				}
				t.smoothedRTT.Store(math.Float64bits(next))
			}
		case "error":
			var msg backendUserMsg
			if err := json.Unmarshal(data, &msg); err == nil && msg.Error != "" {
				slog.Warn("server error", "error", msg.Error)
			}
		default:
			var msg ControlMsg
			if err := json.Unmarshal(data, &msg); err != nil {
				slog.Error("invalid legacy control message", "err", err)
				continue
			}

			switch msg.Type {
			case "user_list":
				selfID := msg.SelfID
				if selfID == 0 && len(msg.Users) > 0 {
					selfID = msg.Users[len(msg.Users)-1].ID
				}

				t.mu.Lock()
				t.myID = selfID
				t.mu.Unlock()

				t.clearUserChannels()
				for _, u := range msg.Users {
					t.userChannels.Store(u.ID, u.ChannelID)
					if u.ID == selfID {
						t.myChannel.Store(u.ChannelID)
					}
				}

				if msg.APIPort != 0 {
					t.mu.Lock()
					host, _, err := net.SplitHostPort(t.serverAddr)
					if err != nil {
						host = t.serverAddr
					}
					t.apiBaseURL = fmt.Sprintf("http://%s:%d", host, msg.APIPort)
					t.mu.Unlock()
				}

				if len(msg.ICEServers) > 0 {
					t.mu.Lock()
					t.iceServers = msg.ICEServers
					t.mu.Unlock()
				}

				if onUserList != nil {
					onUserList(msg.Users)
				}
				if msg.ServerName != "" && onServerInfo != nil {
					onServerInfo(msg.ServerName)
				}
				if onOwnerChanged != nil {
					onOwnerChanged(msg.OwnerID)
				}
				t.ensurePeersFromUserList(msg.Users)
			case "user_joined":
				t.userChannels.Store(msg.ID, int64(0))
				if onUserJoined != nil {
					onUserJoined(msg.ID, msg.Username)
				}
				myID := t.MyID()
				if myID != 0 && msg.ID != 0 && msg.ID != myID {
					_, created := t.ensurePeer(msg.ID)
					if created && myID < msg.ID {
						go t.createAndSendOffer(msg.ID)
					}
				}
			case "user_left":
				t.userChannels.Delete(msg.ID)
				t.closePeer(msg.ID)
				if onUserLeft != nil {
					onUserLeft(msg.ID)
				}
			case "chat":
				if msg.ChannelID != 0 {
					if onChannelChat != nil {
						onChannelChat(msg.MsgID, msg.ID, msg.ChannelID, msg.Username, msg.Message, msg.Ts, msg.FileID, msg.FileName, msg.FileSize, msg.Mentions, msg.ReplyTo, msg.ReplyPreview)
					}
				} else {
					if onChat != nil {
						onChat(msg.MsgID, msg.ID, msg.Username, msg.Message, msg.Ts, msg.FileID, msg.FileName, msg.FileSize, msg.Mentions, msg.ReplyTo, msg.ReplyPreview)
					}
				}
			case "link_preview":
				if onLinkPreview != nil {
					onLinkPreview(msg.MsgID, msg.ChannelID, msg.LinkURL, msg.LinkTitle, msg.LinkDesc, msg.LinkImage, msg.LinkSite)
				}
			case "server_info":
				if msg.ServerName != "" && onServerInfo != nil {
					onServerInfo(msg.ServerName)
				}
			case "owner_changed":
				if onOwnerChanged != nil {
					onOwnerChanged(msg.OwnerID)
				}
			case "kicked":
				if onKicked != nil {
					onKicked()
				}
			case "channel_list":
				if onChannelList != nil {
					onChannelList(msg.Channels)
				}
			case "user_channel":
				t.userChannels.Store(msg.ID, msg.ChannelID)
				if msg.ID == t.MyID() {
					t.myChannel.Store(msg.ChannelID)
				}
				if onUserChannel != nil {
					onUserChannel(msg.ID, msg.ChannelID)
				}
			case "user_renamed":
				if onUserRenamed != nil {
					onUserRenamed(msg.ID, msg.Username)
				}
			case "message_edited":
				if onMessageEdited != nil {
					onMessageEdited(msg.MsgID, msg.Message, msg.Ts)
				}
			case "message_deleted":
				if onMessageDeleted != nil {
					onMessageDeleted(msg.MsgID)
				}
			case "reaction_added":
				if onReactionAdded != nil {
					onReactionAdded(msg.MsgID, msg.Emoji, msg.ID)
				}
			case "reaction_removed":
				if onReactionRemoved != nil {
					onReactionRemoved(msg.MsgID, msg.Emoji, msg.ID)
				}
			case "user_typing":
				if onUserTyping != nil {
					onUserTyping(msg.ID, msg.Username, msg.ChannelID)
				}
			case "message_pinned":
				if onMessagePinned != nil {
					onMessagePinned(msg.MsgID, msg.ChannelID, msg.ID)
				}
			case "message_unpinned":
				if onMessageUnpinned != nil {
					onMessageUnpinned(msg.MsgID)
				}
			case "video_state":
				if onVideoState != nil {
					active := msg.VideoActive != nil && *msg.VideoActive
					screen := msg.ScreenShare != nil && *msg.ScreenShare
					onVideoState(msg.ID, active, screen)
				}
				if onVideoLayers != nil && len(msg.VideoLayers) > 0 {
					onVideoLayers(msg.ID, msg.VideoLayers)
				}
			case "set_video_quality":
				if onVideoQualityReq != nil && msg.VideoQuality != "" {
					onVideoQualityReq(msg.ID, msg.VideoQuality)
				}
			case "recording_started":
				if onRecordingState != nil {
					onRecordingState(msg.ChannelID, true, msg.Username)
				}
			case "recording_stopped":
				if onRecordingState != nil {
					onRecordingState(msg.ChannelID, false, "")
				}
			case "webrtc_offer":
				t.handleOffer(msg.ID, msg.SDP)
			case "webrtc_answer":
				t.handleAnswer(msg.ID, msg.SDP)
			case "webrtc_ice":
				t.handleICE(msg.ID, msg)
			}
		}
	}

	slog.Debug("read control loop exiting")

	t.mu.Lock()
	reason := t.disconnectReason
	t.disconnectReason = ""
	t.mu.Unlock()
	if reason == "" {
		reason = "Connection closed by server"
	}

	t.Disconnect()

	t.cbMu.RLock()
	onDisconnected := t.onDisconnected
	t.cbMu.RUnlock()
	if onDisconnected != nil {
		onDisconnected(reason)
	}
}

// TaggedAudio is a voice frame tagged with the sender's ID and sequence number.
// Used to feed the playback mixer in the audio engine.
type TaggedAudio struct {
	SenderID uint16
	Seq      uint16
	OpusData []byte
}
