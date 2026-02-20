package main

import (
	"sync"
	"testing"
)

func TestRoomAddRemoveClient(t *testing.T) {
	room := NewRoom()

	c1 := &Client{Username: "alice"}
	c2 := &Client{Username: "bob"}

	id1 := room.AddClient(c1)
	id2 := room.AddClient(c2)

	if id1 == id2 {
		t.Fatalf("expected different IDs, got %d and %d", id1, id2)
	}
	if id1 == 0 || id2 == 0 {
		t.Fatalf("IDs should be non-zero, got %d and %d", id1, id2)
	}

	if room.ClientCount() != 2 {
		t.Fatalf("expected 2 clients, got %d", room.ClientCount())
	}

	room.RemoveClient(id1)
	if room.ClientCount() != 1 {
		t.Fatalf("expected 1 client after remove, got %d", room.ClientCount())
	}

	room.RemoveClient(id2)
	if room.ClientCount() != 0 {
		t.Fatalf("expected 0 clients after remove, got %d", room.ClientCount())
	}
}

func TestRoomStatsResetAfterRead(t *testing.T) {
	room := NewRoom()

	room.totalDatagrams.Store(100)
	room.totalBytes.Store(5000)

	d, b, _ := room.Stats()
	if d != 100 || b != 5000 {
		t.Errorf("expected 100/5000, got %d/%d", d, b)
	}

	// Stats should reset after read.
	d, b, _ = room.Stats()
	if d != 0 || b != 0 {
		t.Errorf("expected 0/0 after reset, got %d/%d", d, b)
	}
}

func TestRoomClientsSnapshot(t *testing.T) {
	room := NewRoom()

	c := &Client{Username: "test"}
	room.AddClient(c)

	clients := room.Clients()
	if len(clients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(clients))
	}
	if clients[0].Username != "test" {
		t.Errorf("expected username 'test', got %q", clients[0].Username)
	}
}

func TestRoomIDsAreUnique(t *testing.T) {
	room := NewRoom()
	seen := make(map[uint16]bool)

	for i := 0; i < 100; i++ {
		c := &Client{Username: "user"}
		id := room.AddClient(c)
		if seen[id] {
			t.Fatalf("duplicate ID: %d", id)
		}
		seen[id] = true
	}
}

func TestRoomConcurrentAccess(t *testing.T) {
	room := NewRoom()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := &Client{Username: "user"}
			room.AddClient(c)
		}()
	}

	wg.Wait()

	if room.ClientCount() != 50 {
		t.Errorf("expected 50 clients, got %d", room.ClientCount())
	}
}
