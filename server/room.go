package main

import (
	"log"
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
	serverName string        // protected by mu
	ownerID    uint16        // ID of the current room owner; 0 = no owner; protected by mu
	onRename   func(string)  // optional persistence callback, fired after Rename; protected by mu
	channels   []ChannelInfo // cached channel list sent to newly-connecting clients; protected by mu
	nextID     atomic.Uint32

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

// SetOnRename registers a callback invoked after a successful Rename.
// Intended for persisting the name to a store; called outside the mutex.
func (r *Room) SetOnRename(fn func(string)) {
	r.mu.Lock()
	r.onRename = fn
	r.mu.Unlock()
}

// Rename updates the server name and fires the onRename callback if set.
func (r *Room) Rename(name string) {
	r.mu.Lock()
	r.serverName = name
	cb := r.onRename
	r.mu.Unlock()
	if cb != nil {
		cb(name)
	}
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

// RemoveClient unregisters a client by ID.
func (r *Room) RemoveClient(id uint16) {
	r.mu.Lock()
	delete(r.clients, id)
	r.mu.Unlock()

	log.Printf("[room] client %d left, total=%d", id, r.ClientCount())
}

// Broadcast sends a datagram to every client except the sender.
// It overwrites bytes [0:2] with senderID before fan-out (anti-spoofing is done
// by the caller in readDatagrams, so the slice is already stamped here).
func (r *Room) Broadcast(senderID uint16, data []byte) {
	r.totalDatagrams.Add(1)
	r.totalBytes.Add(uint64(len(data)))

	r.mu.RLock()
	defer r.mu.RUnlock()

	for id, c := range r.clients {
		if id == senderID {
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
func (r *Room) BroadcastControl(msg ControlMsg, excludeID uint16) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for id, c := range r.clients {
		if id == excludeID {
			continue
		}
		c.SendControl(msg)
	}
}

// Clients returns a snapshot of all connected clients (safe to use after releasing the lock).
func (r *Room) Clients() []UserInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]UserInfo, 0, len(r.clients))
	for _, c := range r.clients {
		out = append(out, UserInfo{ID: c.ID, Username: c.Username, ChannelID: c.channelID})
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
