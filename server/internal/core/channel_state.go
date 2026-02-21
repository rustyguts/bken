package core

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bken/server/internal/protocol"
)

// SendTimeout bounds how long a write to one subscriber may block.
const SendTimeout = 50 * time.Millisecond

// Session represents one connected websocket session.
type Session struct {
	UserID string
	Send   chan protocol.Message
}

type userState struct {
	id        string
	username  string
	connected map[string]struct{}
	voice     *protocol.VoiceState
	send      chan protocol.Message
	muted     bool
	deafened  bool
}

// ChannelState is the global in-memory presence state.
// Users may connect to multiple servers simultaneously, but each user has at
// most one global voice connection at any time.
type ChannelState struct {
	mu         sync.RWMutex
	users      map[string]*userState
	nextID     atomic.Uint64
	channels   map[string][]protocol.Channel // serverID → channels
	nextChID   atomic.Int64
	serverName string
}

// NewChannelState returns an empty channel state with the given server name.
func NewChannelState(serverName string) *ChannelState {
	if serverName == "" {
		serverName = "bken server"
	}
	return &ChannelState{
		users:      make(map[string]*userState),
		channels:   make(map[string][]protocol.Channel),
		serverName: serverName,
	}
}

// ServerName returns the configured server display name.
func (r *ChannelState) ServerName() string {
	return r.serverName
}

// Add registers a new user session and returns the session plus full snapshot.
func (r *ChannelState) Add(username string, sendBuf int) (*Session, []protocol.User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil, fmt.Errorf("username is required")
	}
	if sendBuf <= 0 {
		sendBuf = 64
	}

	id := fmt.Sprintf("u%d", r.nextID.Add(1))
	u := &userState{
		id:        id,
		username:  username,
		connected: make(map[string]struct{}),
		send:      make(chan protocol.Message, sendBuf),
	}

	r.mu.Lock()
	r.users[id] = u
	snapshot := r.snapshotLocked()
	count := len(r.users)
	r.mu.Unlock()

	slog.Info("user added", "user_id", id, "username", username, "total_users", count)
	return &Session{UserID: id, Send: u.send}, snapshot, nil
}

// Remove unregisters a user session.
func (r *ChannelState) Remove(userID string) (protocol.User, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	u, ok := r.users[userID]
	if !ok {
		return protocol.User{}, false
	}
	hadVoice := u.voice != nil
	delete(r.users, userID)
	close(u.send)

	slog.Info("user removed", "user_id", userID, "username", u.username, "had_voice", hadVoice, "remaining_users", len(r.users))
	return toProtocolUser(u), true
}

// ClientCount returns active websocket session count.
func (r *ChannelState) ClientCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.users)
}

// User returns one user's authoritative state.
func (r *ChannelState) User(userID string) (protocol.User, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	u, ok := r.users[userID]
	if !ok {
		return protocol.User{}, false
	}
	return toProtocolUser(u), true
}

// Users returns a stable ordered snapshot of all users.
func (r *ChannelState) Users() []protocol.User {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshotLocked()
}

func (r *ChannelState) snapshotLocked() []protocol.User {
	out := make([]protocol.User, 0, len(r.users))
	for _, u := range r.users {
		out = append(out, toProtocolUser(u))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ConnectServer marks user as connected to a logical server.
func (r *ChannelState) ConnectServer(userID, serverID string) (protocol.User, bool, error) {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return protocol.User{}, false, fmt.Errorf("server_id is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	u, ok := r.users[userID]
	if !ok {
		return protocol.User{}, false, fmt.Errorf("user not found")
	}
	_, existed := u.connected[serverID]
	u.connected[serverID] = struct{}{}

	slog.Debug("server connected", "user_id", userID, "server_id", serverID, "new", !existed)
	return toProtocolUser(u), !existed, nil
}

// DisconnectServer removes one logical server membership.
// If the user was voice-connected in that server, voice is disconnected too.
func (r *ChannelState) DisconnectServer(userID, serverID string) (protocol.User, bool, *protocol.VoiceState, error) {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return protocol.User{}, false, nil, fmt.Errorf("server_id is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	u, ok := r.users[userID]
	if !ok {
		return protocol.User{}, false, nil, fmt.Errorf("user not found")
	}
	if _, exists := u.connected[serverID]; !exists {
		return toProtocolUser(u), false, nil, nil
	}
	delete(u.connected, serverID)

	var oldVoice *protocol.VoiceState
	if u.voice != nil && u.voice.ServerID == serverID {
		v := *u.voice
		oldVoice = &v
		u.voice = nil
		u.muted = false
		u.deafened = false
	}

	slog.Debug("server disconnected", "user_id", userID, "server_id", serverID, "voice_cleared", oldVoice != nil)
	return toProtocolUser(u), true, oldVoice, nil
}

// JoinVoice sets the global voice state.
// Users can only be in one voice channel globally.
func (r *ChannelState) JoinVoice(userID, serverID, channelID string) (protocol.User, *protocol.VoiceState, error) {
	serverID = strings.TrimSpace(serverID)
	channelID = strings.TrimSpace(channelID)
	if serverID == "" || channelID == "" {
		return protocol.User{}, nil, fmt.Errorf("server_id and channel_id are required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	u, ok := r.users[userID]
	if !ok {
		return protocol.User{}, nil, fmt.Errorf("user not found")
	}
	if _, connected := u.connected[serverID]; !connected {
		return protocol.User{}, nil, fmt.Errorf("user is not connected to server")
	}

	var oldVoice *protocol.VoiceState
	if u.voice != nil {
		v := *u.voice
		oldVoice = &v
	}
	u.voice = &protocol.VoiceState{ServerID: serverID, ChannelID: channelID}

	slog.Info("voice joined", "user_id", userID, "server_id", serverID, "channel_id", channelID, "prev_server", oldVoice)
	return toProtocolUser(u), oldVoice, nil
}

// DisconnectVoice clears the global voice state.
func (r *ChannelState) DisconnectVoice(userID string) (protocol.User, *protocol.VoiceState, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	u, ok := r.users[userID]
	if !ok {
		return protocol.User{}, nil, false
	}
	if u.voice == nil {
		return toProtocolUser(u), nil, false
	}
	v := *u.voice
	u.voice = nil
	u.muted = false
	u.deafened = false

	slog.Info("voice disconnected", "user_id", userID, "was_server", v.ServerID, "was_channel", v.ChannelID)
	return toProtocolUser(u), &v, true
}

// SetVoiceFlags updates the muted/deafened flags for a user in voice.
// Returns the updated User and whether any flag actually changed.
func (r *ChannelState) SetVoiceFlags(userID string, muted, deafened bool) (protocol.User, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	u, ok := r.users[userID]
	if !ok || u.voice == nil {
		return protocol.User{}, false
	}
	if u.muted == muted && u.deafened == deafened {
		return toProtocolUser(u), false
	}
	u.muted = muted
	u.deafened = deafened

	slog.Debug("voice flags updated", "user_id", userID, "muted", muted, "deafened", deafened)
	return toProtocolUser(u), true
}

// CanSendText reports whether a user is connected to the target server.
func (r *ChannelState) CanSendText(userID, serverID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	u, ok := r.users[userID]
	if !ok {
		return false
	}
	_, connected := u.connected[serverID]
	return connected
}

// CreateChannel adds a named channel to a server and returns the updated list.
func (r *ChannelState) CreateChannel(serverID, name string) ([]protocol.Channel, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("channel name is required")
	}
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return nil, fmt.Errorf("server_id is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	id := r.nextChID.Add(1)
	r.channels[serverID] = append(r.channels[serverID], protocol.Channel{ID: id, Name: name})
	out := make([]protocol.Channel, len(r.channels[serverID]))
	copy(out, r.channels[serverID])

	slog.Info("channel created", "server_id", serverID, "channel_id", id, "name", name, "total_channels", len(out))
	return out, nil
}

// RenameChannel renames a channel and returns the updated list.
func (r *ChannelState) RenameChannel(serverID string, channelID int64, name string) ([]protocol.Channel, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("channel name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	chs := r.channels[serverID]
	for i := range chs {
		if chs[i].ID == channelID {
			chs[i].Name = name
			out := make([]protocol.Channel, len(chs))
			copy(out, chs)
			slog.Debug("channel renamed", "server_id", serverID, "channel_id", channelID, "new_name", name)
			return out, nil
		}
	}
	return nil, fmt.Errorf("channel not found")
}

// DeleteChannel removes a channel and returns the updated list.
func (r *ChannelState) DeleteChannel(serverID string, channelID int64) ([]protocol.Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	chs := r.channels[serverID]
	for i := range chs {
		if chs[i].ID == channelID {
			r.channels[serverID] = append(chs[:i], chs[i+1:]...)
			out := make([]protocol.Channel, len(r.channels[serverID]))
			copy(out, r.channels[serverID])
			slog.Info("channel deleted", "server_id", serverID, "channel_id", channelID, "remaining_channels", len(out))
			return out, nil
		}
	}
	return nil, fmt.Errorf("channel not found")
}

// Channels returns the channel list for a server.
func (r *ChannelState) Channels(serverID string) []protocol.Channel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]protocol.Channel, len(r.channels[serverID]))
	copy(out, r.channels[serverID])
	return out
}

// UserServer returns the single server a user is connected to, or an error
// if the user is connected to zero or multiple servers.
func (r *ChannelState) UserServer(userID string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	u, ok := r.users[userID]
	if !ok {
		return "", fmt.Errorf("user not found")
	}
	if len(u.connected) == 0 {
		return "", fmt.Errorf("user is not connected to any server")
	}
	if len(u.connected) > 1 {
		return "", fmt.Errorf("ambiguous: user is connected to multiple servers")
	}
	for sid := range u.connected {
		return sid, nil
	}
	return "", fmt.Errorf("unreachable")
}

// Broadcast sends a message to all connected users except exceptUserID.
func (r *ChannelState) Broadcast(msg protocol.Message, exceptUserID string) {
	r.mu.RLock()
	targets := make([]chan protocol.Message, 0, len(r.users))
	for id, u := range r.users {
		if exceptUserID != "" && id == exceptUserID {
			continue
		}
		targets = append(targets, u.send)
	}
	r.mu.RUnlock()

	sent := 0
	for _, ch := range targets {
		if trySend(ch, msg) {
			sent++
		}
	}
	slog.Debug("broadcast", "type", msg.Type, "recipients", sent, "total", len(targets))
}

// BroadcastToServer sends a message to users connected to serverID.
func (r *ChannelState) BroadcastToServer(serverID string, msg protocol.Message, exceptUserID string) {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return
	}

	r.mu.RLock()
	targets := make([]chan protocol.Message, 0, len(r.users))
	for id, u := range r.users {
		if exceptUserID != "" && id == exceptUserID {
			continue
		}
		if _, ok := u.connected[serverID]; !ok {
			continue
		}
		targets = append(targets, u.send)
	}
	r.mu.RUnlock()

	sent := 0
	for _, ch := range targets {
		if trySend(ch, msg) {
			sent++
		}
	}
	slog.Debug("broadcast_to_server", "type", msg.Type, "server_id", serverID, "recipients", sent, "total", len(targets))
}

// SendTo sends one message to one user.
func (r *ChannelState) SendTo(userID string, msg protocol.Message) bool {
	r.mu.RLock()
	u, ok := r.users[userID]
	r.mu.RUnlock()
	if !ok {
		return false
	}
	return trySend(u.send, msg)
}

func toProtocolUser(u *userState) protocol.User {
	servers := make([]string, 0, len(u.connected))
	for sid := range u.connected {
		servers = append(servers, sid)
	}
	sort.Strings(servers)

	out := protocol.User{
		ID:               u.id,
		Username:         u.username,
		ConnectedServers: servers,
	}
	if u.voice != nil {
		v := *u.voice
		v.Muted = u.muted
		v.Deafened = u.deafened
		out.Voice = &v
	}
	return out
}

func trySend(ch chan protocol.Message, msg protocol.Message) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	select {
	case ch <- msg:
		return true
	case <-time.After(SendTimeout):
		slog.Debug("trySend timeout", "type", msg.Type)
		return false
	}
}
