package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
)

// DatagramSender is the minimal interface needed to send a datagram to a session.
// Using an interface here lets tests inject a mock instead of a real webtransport.Session.
type DatagramSender interface {
	SendDatagram([]byte) error
}

// Room holds all connected clients and handles voice datagram fan-out.
type Room struct {
	mu         sync.RWMutex
	clients    map[uint16]*Client
	serverName string              // protected by mu
	ownerID    uint16              // ID of the current room owner; 0 = no owner; protected by mu
	onRename   func(string) error // optional persistence callback, fired after Rename; protected by mu
	channels   []ChannelInfo // cached channel list sent to newly-connecting clients; protected by mu
	apiPort    int           // HTTP API port communicated to clients in user_list; protected by mu
	nextID     atomic.Uint32
	nextMsgID  atomic.Uint64

	// Channel CRUD persistence callbacks — set via setters; called outside the mutex.
	onCreateChannel func(name string) (int64, error)
	onRenameChannel func(id int64, name string) error
	onDeleteChannel func(id int64) error
	onRefreshChannels func() ([]ChannelInfo, error)

	// Metrics (reset on each Stats call).
	totalDatagrams atomic.Uint64
	totalBytes     atomic.Uint64
}

func NewRoom() *Room {
	return &Room{
		clients: make(map[uint16]*Client),
	}
}

// SetServerName updates the human-readable server name sent to connecting clients.
func (r *Room) SetServerName(name string) {
	r.mu.Lock()
	r.serverName = name
	r.mu.Unlock()
}

// ServerName returns the current server name.
func (r *Room) ServerName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.serverName
}

// SetAPIPort sets the HTTP API port communicated to clients in the welcome message.
func (r *Room) SetAPIPort(port int) {
	r.mu.Lock()
	r.apiPort = port
	r.mu.Unlock()
}

// APIPort returns the configured HTTP API port.
func (r *Room) APIPort() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.apiPort
}

// SetOnRename registers a callback invoked after a successful Rename.
// Intended for persisting the name to a store; called outside the mutex.
func (r *Room) SetOnRename(fn func(string) error) {
	r.mu.Lock()
	r.onRename = fn
	r.mu.Unlock()
}

// Rename updates the server name and fires the onRename callback if set.
// Returns an error if the persistence callback fails; in that case the
// in-memory name is still updated.
func (r *Room) Rename(name string) error {
	r.mu.Lock()
	r.serverName = name
	cb := r.onRename
	r.mu.Unlock()
	if cb != nil {
		return cb(name)
	}
	return nil
}

// ClaimOwnership sets id as the room owner if no owner is currently set.
// Returns true if this call made the client the owner.
func (r *Room) ClaimOwnership(id uint16) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ownerID == 0 {
		r.ownerID = id
		return true
	}
	return false
}

// OwnerID returns the ID of the current room owner (0 if none).
func (r *Room) OwnerID() uint16 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ownerID
}

// TransferOwnership removes ownership from leavingID and assigns it to the
// remaining client with the lowest ID. Returns the new owner ID and whether
// ownership actually changed. Call after RemoveClient so the leaving client
// is no longer in the clients map.
func (r *Room) TransferOwnership(leavingID uint16) (newOwnerID uint16, changed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ownerID != leavingID {
		return r.ownerID, false // leaving client was not the owner
	}
	r.ownerID = 0
	for id := range r.clients {
		if r.ownerID == 0 || id < r.ownerID {
			r.ownerID = id
		}
	}
	return r.ownerID, true
}

// GetClient returns the client with the given ID, or nil if not found.
func (r *Room) GetClient(id uint16) *Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.clients[id]
}

// AddClient registers a client, assigns it a unique ID, and returns that ID.
func (r *Room) AddClient(c *Client) uint16 {
	id := uint16(r.nextID.Add(1))
	c.ID = id

	r.mu.Lock()
	r.clients[id] = c
	r.mu.Unlock()

	log.Printf("[room] client %d (%s) joined, total=%d", id, c.Username, r.ClientCount())
	return id
}

// AddOrReplaceClient registers c and atomically removes any existing client
// with the same username (case-insensitive). It always assigns a fresh ID to c.
// Returns the new ID, plus the replaced client and ID if a replacement occurred.
func (r *Room) AddOrReplaceClient(c *Client) (id uint16, replaced *Client, replacedID uint16) {
	id = uint16(r.nextID.Add(1))
	c.ID = id

	r.mu.Lock()
	for existingID, existing := range r.clients {
		if strings.EqualFold(existing.Username, c.Username) {
			replaced = existing
			replacedID = existingID
			delete(r.clients, existingID)
			break
		}
	}
	r.clients[id] = c
	total := len(r.clients)
	r.mu.Unlock()

	if replaced != nil {
		log.Printf("[room] replaced client %d (%s) with %d (%s), total=%d",
			replacedID, replaced.Username, id, c.Username, total)
	} else {
		log.Printf("[room] client %d (%s) joined, total=%d", id, c.Username, total)
	}

	return id, replaced, replacedID
}

// RemoveClient unregisters a client by ID.
func (r *Room) RemoveClient(id uint16) bool {
	r.mu.Lock()
	_, existed := r.clients[id]
	if existed {
		delete(r.clients, id)
	}
	total := len(r.clients)
	r.mu.Unlock()

	if existed {
		log.Printf("[room] client %d left, total=%d", id, total)
	}
	return existed
}

// Broadcast sends a datagram to every client in the same channel as the sender,
// excluding the sender itself. Clients in channel 0 (lobby) never send or
// receive voice datagrams.
// It overwrites bytes [0:2] with senderID before fan-out (anti-spoofing is done
// by the caller in readDatagrams, so the slice is already stamped here).
func (r *Room) Broadcast(senderID uint16, data []byte) {
	r.totalDatagrams.Add(1)
	r.totalBytes.Add(uint64(len(data)))

	r.mu.RLock()
	defer r.mu.RUnlock()

	sender := r.clients[senderID]
	if sender == nil {
		return
	}
	senderChannel := sender.channelID.Load()
	if senderChannel == 0 {
		return // lobby users don't transmit voice
	}

	for id, c := range r.clients {
		if id == senderID {
			continue
		}
		if c.channelID.Load() != senderChannel {
			continue
		}
		if err := c.session.SendDatagram(data); err != nil {
			// UDP-like semantics: log and continue, never block the hot path.
			log.Printf("[room] datagram to client %d dropped: %v", id, err)
		}
	}
}

// BroadcastControl sends a control message to all clients except the one with excludeID.
// Pass excludeID=0 to send to all clients.
// JSON is marshaled once before acquiring the lock to minimise lock hold time.
func (r *Room) BroadcastControl(msg ControlMsg, excludeID uint16) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[room] BroadcastControl marshal error: %v", err)
		return
	}
	data = append(data, '\n')

	r.mu.RLock()
	defer r.mu.RUnlock()

	for id, c := range r.clients {
		if id == excludeID {
			continue
		}
		c.sendRaw(data)
	}
}

// BroadcastToChannel sends a control message to all clients currently in channelID.
// Sends to every matching client including the sender (excludeID=0 semantics).
// JSON is marshaled once before acquiring the lock to minimise lock hold time.
func (r *Room) BroadcastToChannel(channelID int64, msg ControlMsg) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[room] BroadcastToChannel marshal error: %v", err)
		return
	}
	data = append(data, '\n')

	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, c := range r.clients {
		if c.channelID.Load() == channelID {
			c.sendRaw(data)
		}
	}
}

// Clients returns a snapshot of all connected clients (safe to use after releasing the lock).
func (r *Room) Clients() []UserInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]UserInfo, 0, len(r.clients))
	for _, c := range r.clients {
		out = append(out, UserInfo{ID: c.ID, Username: c.Username, ChannelID: c.channelID.Load()})
	}
	return out
}

// SetChannels replaces the cached channel list and broadcasts a channel_list
// message to all currently-connected clients.
func (r *Room) SetChannels(channels []ChannelInfo) {
	r.mu.Lock()
	r.channels = channels
	r.mu.Unlock()
	r.BroadcastControl(ControlMsg{Type: "channel_list", Channels: channels}, 0)
}

// GetChannelList returns the cached channel list.
func (r *Room) GetChannelList() []ChannelInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.channels
}

// SetOnCreateChannel registers a callback for creating a channel in the store.
func (r *Room) SetOnCreateChannel(fn func(name string) (int64, error)) {
	r.mu.Lock()
	r.onCreateChannel = fn
	r.mu.Unlock()
}

// SetOnRenameChannel registers a callback for renaming a channel in the store.
func (r *Room) SetOnRenameChannel(fn func(id int64, name string) error) {
	r.mu.Lock()
	r.onRenameChannel = fn
	r.mu.Unlock()
}

// SetOnDeleteChannel registers a callback for deleting a channel from the store.
func (r *Room) SetOnDeleteChannel(fn func(id int64) error) {
	r.mu.Lock()
	r.onDeleteChannel = fn
	r.mu.Unlock()
}

// SetOnRefreshChannels registers a callback that reloads the channel list from the store.
func (r *Room) SetOnRefreshChannels(fn func() ([]ChannelInfo, error)) {
	r.mu.Lock()
	r.onRefreshChannels = fn
	r.mu.Unlock()
}

// CreateChannel creates a channel in the store and broadcasts the updated list.
func (r *Room) CreateChannel(name string) (int64, error) {
	r.mu.RLock()
	cb := r.onCreateChannel
	r.mu.RUnlock()
	if cb == nil {
		return 0, fmt.Errorf("channel creation not configured")
	}
	id, err := cb(name)
	if err != nil {
		return 0, err
	}
	r.refreshChannels()
	return id, nil
}

// RenameChannel renames a channel in the store and broadcasts the updated list.
func (r *Room) RenameChannel(id int64, name string) error {
	r.mu.RLock()
	cb := r.onRenameChannel
	r.mu.RUnlock()
	if cb == nil {
		return fmt.Errorf("channel rename not configured")
	}
	if err := cb(id, name); err != nil {
		return err
	}
	r.refreshChannels()
	return nil
}

// DeleteChannel deletes a channel from the store and broadcasts the updated list.
func (r *Room) DeleteChannel(id int64) error {
	r.mu.RLock()
	cb := r.onDeleteChannel
	r.mu.RUnlock()
	if cb == nil {
		return fmt.Errorf("channel deletion not configured")
	}
	if err := cb(id); err != nil {
		return err
	}
	r.refreshChannels()
	return nil
}

// refreshChannels reloads the channel list from the store and broadcasts it.
func (r *Room) refreshChannels() {
	r.mu.RLock()
	cb := r.onRefreshChannels
	r.mu.RUnlock()
	if cb == nil {
		return
	}
	channels, err := cb()
	if err != nil {
		log.Printf("[room] refresh channels: %v", err)
		return
	}
	r.SetChannels(channels)
}

// MoveChannelUsersToLobby moves all users currently in channelID to channel 0
// (lobby) and broadcasts a user_channel update for each moved user.
func (r *Room) MoveChannelUsersToLobby(channelID int64) {
	r.mu.RLock()
	var moved []uint16
	for _, c := range r.clients {
		if c.channelID.Load() == channelID {
			c.channelID.Store(0)
			moved = append(moved, c.ID)
		}
	}
	r.mu.RUnlock()
	for _, id := range moved {
		r.BroadcastControl(ControlMsg{Type: "user_channel", ID: id, ChannelID: 0}, 0)
	}
}

// NextMsgID returns a monotonically increasing message ID for chat messages.
func (r *Room) NextMsgID() uint64 {
	return r.nextMsgID.Add(1)
}

// ClientCount returns the current number of connected clients.
func (r *Room) ClientCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}

// Stats returns accumulated datagram/byte counts since the last call and resets them.
func (r *Room) Stats() (datagrams, bytes uint64, clients int) {
	datagrams = r.totalDatagrams.Swap(0)
	bytes = r.totalBytes.Swap(0)
	clients = r.ClientCount()
	return
}
