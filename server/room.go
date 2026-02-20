package main

import (
	"log"
	"sync"
	"sync/atomic"
)

// Room holds all connected clients and handles voice datagram fan-out.
type Room struct {
	mu      sync.RWMutex
	clients map[uint16]*Client
	nextID  atomic.Uint32

	// Metrics
	totalDatagrams atomic.Uint64
	totalBytes     atomic.Uint64
}

func NewRoom() *Room {
	return &Room{
		clients: make(map[uint16]*Client),
	}
}

// AddClient registers a client and returns their assigned ID.
func (r *Room) AddClient(c *Client) uint16 {
	id := uint16(r.nextID.Add(1))
	c.ID = id

	r.mu.Lock()
	r.clients[id] = c
	r.mu.Unlock()

	log.Printf("[room] client %d (%s) joined, total=%d", id, c.Username, r.ClientCount())
	return id
}

// RemoveClient unregisters a client.
func (r *Room) RemoveClient(id uint16) {
	r.mu.Lock()
	delete(r.clients, id)
	r.mu.Unlock()

	log.Printf("[room] client %d left, total=%d", id, r.ClientCount())
}

// Broadcast sends a datagram to all clients except the sender.
func (r *Room) Broadcast(senderID uint16, data []byte) {
	r.totalDatagrams.Add(1)
	r.totalBytes.Add(uint64(len(data)))

	r.mu.RLock()
	defer r.mu.RUnlock()

	for id, c := range r.clients {
		if id == senderID {
			continue
		}
		if err := c.Session.SendDatagram(data); err != nil {
			// Non-fatal: UDP-like semantics, drop and move on
			log.Printf("[room] send to %d failed: %v", id, err)
		}
	}
}

// ClientCount returns the number of connected clients.
func (r *Room) ClientCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}

// Clients returns a snapshot of current clients for user list messages.
func (r *Room) Clients() []*Client {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*Client, 0, len(r.clients))
	for _, c := range r.clients {
		out = append(out, c)
	}
	return out
}

// Stats returns current room metrics and resets counters.
func (r *Room) Stats() (datagrams, bytes uint64, clients int) {
	datagrams = r.totalDatagrams.Swap(0)
	bytes = r.totalBytes.Swap(0)
	clients = r.ClientCount()
	return
}
