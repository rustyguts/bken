package main

import (
	"fmt"
	"sync"
	"testing"
)

func TestRoomStress500Clients(t *testing.T) {
	room := NewRoom()
	const n = 500

	var wg sync.WaitGroup
	wg.Add(n)

	ids := make([]uint16, n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			c := newTestClient(fmt.Sprintf("user-%d", i))
			ids[i] = room.AddClient(c)
		}(i)
	}
	wg.Wait()

	if room.ClientCount() != n {
		t.Fatalf("expected %d clients, got %d", n, room.ClientCount())
	}

	// All IDs should be unique.
	seen := make(map[uint16]bool, n)
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate ID: %d", id)
		}
		seen[id] = true
	}

	// Broadcast a datagram — should reach n-1 clients.
	data := make([]byte, 10)
	room.Broadcast(ids[0], data)

	d, b, clients := room.Stats()
	if clients != n {
		t.Errorf("stats clients: got %d, want %d", clients, n)
	}
	if d != 1 {
		t.Errorf("expected 1 datagram, got %d", d)
	}
	if b != 10 {
		t.Errorf("expected 10 bytes, got %d", b)
	}

	// Remove all clients concurrently.
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			room.RemoveClient(ids[i])
		}(i)
	}
	wg.Wait()

	if room.ClientCount() != 0 {
		t.Errorf("expected 0 after removal, got %d", room.ClientCount())
	}
}
