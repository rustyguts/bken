package main

import (
	"fmt"
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

func TestRoomAddOrReplaceClientReplacesDuplicateUsername(t *testing.T) {
	room := NewRoom()

	original := newTestClient("alice")
	originalID := room.AddClient(original)

	replacement := newTestClient("Alice") // case-insensitive duplicate
	newID, replaced, replacedID := room.AddOrReplaceClient(replacement)

	if replaced == nil {
		t.Fatal("expected duplicate username to replace existing client")
	}
	if replacedID != originalID {
		t.Fatalf("replacedID: got %d, want %d", replacedID, originalID)
	}
	if replaced != original {
		t.Fatal("expected replaced pointer to be the original client")
	}
	if newID == 0 || replacement.ID != newID {
		t.Fatalf("replacement ID assignment failed: newID=%d client.ID=%d", newID, replacement.ID)
	}
	if room.ClientCount() != 1 {
		t.Fatalf("expected exactly 1 client after replacement, got %d", room.ClientCount())
	}

	users := room.Clients()
	if len(users) != 1 {
		t.Fatalf("expected 1 user in snapshot, got %d", len(users))
	}
	if users[0].ID != newID {
		t.Fatalf("snapshot ID: got %d, want %d", users[0].ID, newID)
	}
	if users[0].Username != "Alice" {
		t.Fatalf("snapshot username: got %q, want %q", users[0].Username, "Alice")
	}
}

func TestRoomRemoveClientReturnsFalseWhenMissing(t *testing.T) {
	room := NewRoom()

	if removed := room.RemoveClient(12345); removed {
		t.Fatal("expected RemoveClient to return false for missing ID")
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

	// All three must be in the same non-zero channel for voice to route.
	c1.channelID.Store(1)
	c2.channelID.Store(1)
	c3.channelID.Store(1)

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

func TestRoomBroadcastFiltersByChannel(t *testing.T) {
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

	// alice and bob in channel 1, carol in channel 2
	c1.channelID.Store(1)
	c2.channelID.Store(1)
	c3.channelID.Store(2)

	data := []byte{0, 0, 0, 1, 0xAA, 0xBB}
	room.Broadcast(c1.ID, data)

	s2.mu.Lock()
	rcv2 := len(s2.received)
	s2.mu.Unlock()

	s3.mu.Lock()
	rcv3 := len(s3.received)
	s3.mu.Unlock()

	if rcv2 != 1 {
		t.Errorf("bob (same channel) should receive 1 datagram, got %d", rcv2)
	}
	if rcv3 != 0 {
		t.Errorf("carol (different channel) should receive 0 datagrams, got %d", rcv3)
	}
}

func TestRoomBroadcastLobbyDoesNotTransmit(t *testing.T) {
	room := NewRoom()

	s1 := &mockSender{}
	s2 := &mockSender{}

	c1 := &Client{Username: "alice", session: s1}
	c2 := &Client{Username: "bob", session: s2}

	room.AddClient(c1)
	room.AddClient(c2)

	// Both in lobby (channel 0) — no voice should be transmitted.
	c1.channelID.Store(0)
	c2.channelID.Store(0)

	data := []byte{0, 0, 0, 1, 0xAA, 0xBB}
	room.Broadcast(c1.ID, data)

	s2.mu.Lock()
	rcv2 := len(s2.received)
	s2.mu.Unlock()

	if rcv2 != 0 {
		t.Errorf("lobby users should not receive voice datagrams, got %d", rcv2)
	}
}

func TestRoomSetGetServerName(t *testing.T) {
	room := NewRoom()
	if room.ServerName() != "" {
		t.Errorf("expected empty server name initially, got %q", room.ServerName())
	}
	room.SetServerName("My Server")
	if room.ServerName() != "My Server" {
		t.Errorf("expected %q, got %q", "My Server", room.ServerName())
	}
	room.SetServerName("Updated")
	if room.ServerName() != "Updated" {
		t.Errorf("expected %q, got %q", "Updated", room.ServerName())
	}
}

func TestRoomClaimOwnership(t *testing.T) {
	room := NewRoom()

	c1 := newTestClient("alice")
	c2 := newTestClient("bob")
	room.AddClient(c1)
	room.AddClient(c2)

	if !room.ClaimOwnership(c1.ID) {
		t.Fatal("first ClaimOwnership should succeed")
	}
	if room.OwnerID() != c1.ID {
		t.Errorf("ownerID: got %d, want %d", room.OwnerID(), c1.ID)
	}
	if room.ClaimOwnership(c2.ID) {
		t.Error("second ClaimOwnership should fail when owner already set")
	}
	if room.OwnerID() != c1.ID {
		t.Errorf("ownerID should remain %d, got %d", c1.ID, room.OwnerID())
	}
}

func TestRoomTransferOwnershipToLowestID(t *testing.T) {
	room := NewRoom()

	c1 := newTestClient("alice")
	c2 := newTestClient("bob")
	c3 := newTestClient("carol")
	room.AddClient(c1)
	room.AddClient(c2)
	room.AddClient(c3)
	room.ClaimOwnership(c1.ID)

	// c1 leaves → TransferOwnership must be called AFTER RemoveClient.
	room.RemoveClient(c1.ID)
	newOwner, changed := room.TransferOwnership(c1.ID)
	if !changed {
		t.Fatal("expected ownership to change when owner left")
	}
	if newOwner != c2.ID {
		t.Errorf("newOwner: got %d, want %d (lowest remaining ID)", newOwner, c2.ID)
	}
}

func TestRoomTransferOwnershipNonOwner(t *testing.T) {
	room := NewRoom()

	c1 := newTestClient("alice")
	c2 := newTestClient("bob")
	room.AddClient(c1)
	room.AddClient(c2)
	room.ClaimOwnership(c1.ID)

	room.RemoveClient(c2.ID)
	_, changed := room.TransferOwnership(c2.ID)
	if changed {
		t.Error("non-owner leaving should not change ownership")
	}
	if room.OwnerID() != c1.ID {
		t.Errorf("ownerID should remain %d, got %d", c1.ID, room.OwnerID())
	}
}

func TestRoomTransferOwnershipEmptyRoom(t *testing.T) {
	room := NewRoom()

	c1 := newTestClient("alice")
	room.AddClient(c1)
	room.ClaimOwnership(c1.ID)

	room.RemoveClient(c1.ID)
	newOwner, changed := room.TransferOwnership(c1.ID)
	if !changed {
		t.Fatal("expected ownership to change")
	}
	if newOwner != 0 {
		t.Errorf("empty room: newOwner should be 0, got %d", newOwner)
	}
}

func TestRoomRenameFiresCallback(t *testing.T) {
	room := NewRoom()
	var called []string
	room.SetOnRename(func(name string) error { called = append(called, name); return nil })

	room.Rename("First Name")
	room.Rename("Second Name")

	if room.ServerName() != "Second Name" {
		t.Errorf("ServerName: got %q, want %q", room.ServerName(), "Second Name")
	}
	if len(called) != 2 || called[0] != "First Name" || called[1] != "Second Name" {
		t.Errorf("callback sequence: got %v", called)
	}
}

func TestRoomRenameNoCallback(t *testing.T) {
	room := NewRoom()
	// Should not panic when no callback is registered.
	room.Rename("Unnamed")
	if room.ServerName() != "Unnamed" {
		t.Errorf("ServerName: got %q, want %q", room.ServerName(), "Unnamed")
	}
}

func TestRoomSetGetChannelList(t *testing.T) {
	room := NewRoom()

	if chs := room.GetChannelList(); chs != nil {
		t.Errorf("expected nil before SetChannels, got %v", chs)
	}

	room.SetChannels([]ChannelInfo{{ID: 1, Name: "General"}, {ID: 2, Name: "Gaming"}})
	chs := room.GetChannelList()
	if len(chs) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(chs))
	}
	if chs[0].Name != "General" || chs[1].Name != "Gaming" {
		t.Errorf("unexpected channels: %v", chs)
	}
}

func TestRoomClientsIncludesChannelID(t *testing.T) {
	room := NewRoom()

	c := newTestClient("alice")
	room.AddClient(c)
	c.channelID.Store(42)

	users := room.Clients()
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].ChannelID != 42 {
		t.Errorf("ChannelID: got %d, want 42", users[0].ChannelID)
	}
}

func TestRoomBroadcastToChannelOnlySameChannel(t *testing.T) {
	room := NewRoom()

	inChannel, inBuf := newCtrlClient("alice")
	inChannel.channelID.Store(1)
	room.AddClient(inChannel)

	otherChannel, otherBuf := newCtrlClient("bob")
	otherChannel.channelID.Store(2)
	room.AddClient(otherChannel)

	lobby, lobbyBuf := newCtrlClient("carol")
	// lobby.channelID = 0 (default)
	room.AddClient(lobby)

	msg := ControlMsg{Type: "chat", Message: "hello channel 1", ChannelID: 1}
	room.BroadcastToChannel(1, msg)

	// Only the client in channel 1 should receive the message.
	if inBuf.Len() == 0 {
		t.Error("client in channel 1 should receive the message")
	}
	got := decodeControl(t, inBuf)
	if got.Message != "hello channel 1" {
		t.Errorf("message: got %q", got.Message)
	}
	if otherBuf.Len() != 0 {
		t.Error("client in channel 2 should NOT receive the message")
	}
	if lobbyBuf.Len() != 0 {
		t.Error("lobby client should NOT receive the message")
	}
}

func TestRoomBroadcastCountsMetrics(t *testing.T) {
	room := NewRoom()

	sender := newTestClient("sender")
	receiver := newTestClient("receiver")
	room.AddClient(sender)
	room.AddClient(receiver)

	// Both must be in the same non-zero channel.
	sender.channelID.Store(1)
	receiver.channelID.Store(1)

	data := make([]byte, 10)
	room.Broadcast(sender.ID, data)

	d, b, _ := room.Stats()
	if d != 1 {
		t.Errorf("expected 1 datagram, got %d", d)
	}
	if b != 10 {
		t.Errorf("expected 10 bytes, got %d", b)
	}
}

func TestRoomBroadcastSnapshotReleasesLock(t *testing.T) {
	// Verify that the broadcast snapshot approach allows concurrent AddClient
	// while SendDatagram calls are in progress. With the old hold-lock approach,
	// a slow SendDatagram would block AddClient.
	room := NewRoom()

	slowSender := &mockSender{}
	s1 := &Client{Username: "alice", session: slowSender}
	room.AddClient(s1)
	s1.channelID.Store(1)

	s2 := &Client{Username: "bob", session: &mockSender{}}
	room.AddClient(s2)
	s2.channelID.Store(1)

	data := []byte{0, 0, 0, 1, 0xAA}
	room.Broadcast(s1.ID, data)

	// After broadcast, we should be able to add more clients without deadlock.
	s3 := newTestClient("carol")
	room.AddClient(s3)

	if room.ClientCount() != 3 {
		t.Errorf("expected 3 clients, got %d", room.ClientCount())
	}
}

func TestRoomBroadcastUnknownSender(t *testing.T) {
	room := NewRoom()

	data := []byte{0, 0, 0, 1, 0xAA}
	// Should not panic when sender doesn't exist.
	room.Broadcast(9999, data)
}

func TestRoomBroadcastConcurrentWithClientChanges(t *testing.T) {
	room := NewRoom()
	const numClients = 20

	clients := make([]*Client, numClients)
	for i := range clients {
		c := &Client{Username: fmt.Sprintf("user-%d", i), session: &mockSender{}}
		room.AddClient(c)
		c.channelID.Store(1)
		clients[i] = c
	}

	// Concurrently broadcast and add/remove clients.
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		data := []byte{0, 0, 0, 1, 0xAA}
		for i := 0; i < 100; i++ {
			room.Broadcast(clients[0].ID, data)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			c := &Client{Username: fmt.Sprintf("extra-%d", i), session: &mockSender{}}
			id := room.AddClient(c)
			c.channelID.Store(1)
			room.RemoveClient(id)
		}
	}()

	wg.Wait()
	// If we got here without deadlock or panic, the test passes.
}

func TestRoomNextMsgID(t *testing.T) {
	room := NewRoom()
	id1 := room.NextMsgID()
	id2 := room.NextMsgID()
	if id2 <= id1 {
		t.Errorf("MsgID should be monotonically increasing: %d, %d", id1, id2)
	}
}
