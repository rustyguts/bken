package core

import (
	"fmt"
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
}

// ChannelState is the global in-memory presence state.
// Users may connect to multiple servers simultaneously, but each user has at
// most one global voice connection at any time.
type ChannelState struct {
	mu     sync.RWMutex
	users  map[string]*userState
	nextID atomic.Uint64
}

// NewChannelState returns an empty channel state.
func NewChannelState() *ChannelState {
	return &ChannelState{users: make(map[string]*userState)}
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
	r.mu.Unlock()

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
	delete(r.users, userID)
	close(u.send)
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
	}

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
	return toProtocolUser(u), &v, true
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

	for _, ch := range targets {
		trySend(ch, msg)
	}
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

	for _, ch := range targets {
		trySend(ch, msg)
	}
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
		return false
	}
}
