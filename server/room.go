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
	serverName string // protected by mu
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
		out = append(out, UserInfo{ID: c.ID, Username: c.Username})
	}
	return out
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
