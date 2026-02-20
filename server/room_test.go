package main

import (
	"sync"
	"testing"
)

// mockSender implements DatagramSender for tests.
type mockSender struct {
	mu       sync.Mutex
	received [][]byte
	err      error
}

func (m *mockSender) SendDatagram(data []byte) error {
	if m.err != nil {
		return m.err
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	m.mu.Lock()
	m.received = append(m.received, cp)
	m.mu.Unlock()
	return nil
}

func newTestClient(username string) *Client {
	return &Client{
		Username: username,
		session:  &mockSender{},
	}
}

func TestRoomAddRemoveClient(t *testing.T) {
	room := NewRoom()

	c1 := newTestClient("alice")
	c2 := newTestClient("bob")

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

	c := newTestClient("test")
	room.AddClient(c)

	users := room.Clients()
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].Username != "test" {
		t.Errorf("expected username 'test', got %q", users[0].Username)
	}
}

func TestRoomIDsAreUnique(t *testing.T) {
	room := NewRoom()
	seen := make(map[uint16]bool)

	for i := 0; i < 100; i++ {
		c := newTestClient("user")
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
			c := newTestClient("user")
			room.AddClient(c)
		}()
	}

	wg.Wait()
	if room.ClientCount() != 50 {
		t.Errorf("expected 50 clients, got %d", room.ClientCount())
	}
}

func TestRoomBroadcastSkipsSender(t *testing.T) {
	room := NewRoom()

	s1 := &mockSender{}
	s2 := &mockSender{}
	s3 := &mockSender{}

	c1 := &Client{Username: "alice", session: s1}
	c2 := &Client{Username: "bob", session: s2}
	c3 := &Client{Username: "carol", session: s3}

	room.AddClient(c1)
	room.AddClient(c2)
	room.AddClient(c3)

	data := []byte{0, 0, 0, 1, 0xAA, 0xBB} // senderID placeholder + seq + payload
	room.Broadcast(c1.ID, data)

	s2.mu.Lock()
	rcv2 := len(s2.received)
	s2.mu.Unlock()

	s3.mu.Lock()
	rcv3 := len(s3.received)
	s3.mu.Unlock()

	s1.mu.Lock()
	rcv1 := len(s1.received)
	s1.mu.Unlock()

	if rcv1 != 0 {
		t.Errorf("sender should not receive their own datagram, got %d", rcv1)
	}
	if rcv2 != 1 {
		t.Errorf("bob should receive 1 datagram, got %d", rcv2)
	}
	if rcv3 != 1 {
		t.Errorf("carol should receive 1 datagram, got %d", rcv3)
	}
}

func TestRoomBroadcastCountsMetrics(t *testing.T) {
	room := NewRoom()

	c := newTestClient("user")
	room.AddClient(c)

	data := make([]byte, 10)
	room.Broadcast(999, data) // senderID=999 doesn't match any client, so all receive it

	d, b, _ := room.Stats()
	if d != 1 {
		t.Errorf("expected 1 datagram, got %d", d)
	}
	if b != 10 {
		t.Errorf("expected 10 bytes, got %d", b)
	}
}
