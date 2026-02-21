package main

import (
	"fmt"
	"sync"
	"testing"
	"time"
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

	d, b, _, _ := room.Stats()
	if d != 100 || b != 5000 {
		t.Errorf("expected 100/5000, got %d/%d", d, b)
	}

	// Stats should reset after read.
	d, b, _, _ = room.Stats()
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

	d, b, _, _ := room.Stats()
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

func TestRoomBroadcastConcurrentSenders(t *testing.T) {
	// Regression test: multiple goroutines calling Broadcast simultaneously
	// must each get their own target buffer. The old code shared snapshotBuf
	// under RLock, causing a data race on the backing array.
	room := NewRoom()
	const numClients = 10

	clients := make([]*Client, numClients)
	for i := range clients {
		c := &Client{Username: fmt.Sprintf("user-%d", i), session: &mockSender{}}
		room.AddClient(c)
		c.channelID.Store(1)
		clients[i] = c
	}

	var wg sync.WaitGroup
	// Every client broadcasts concurrently — exercises concurrent RLock path.
	for _, c := range clients {
		wg.Add(1)
		go func(sender *Client) {
			defer wg.Done()
			data := []byte{0, 0, 0, 1, 0xAA}
			for i := 0; i < 200; i++ {
				room.Broadcast(sender.ID, data)
			}
		}(c)
	}

	wg.Wait()

	// Every client should have received datagrams from all other senders.
	for _, c := range clients {
		ms := c.session.(*mockSender)
		ms.mu.Lock()
		count := len(ms.received)
		ms.mu.Unlock()
		// Each of the other 9 senders sent 200 datagrams = 1800 expected.
		if count != (numClients-1)*200 {
			t.Errorf("client %d: received %d datagrams, want %d", c.ID, count, (numClients-1)*200)
		}
	}
}

func TestRoomNextMsgID(t *testing.T) {
	room := NewRoom()
	id1 := room.NextMsgID()
	id2 := room.NextMsgID()
	if id2 <= id1 {
		t.Errorf("MsgID should be monotonically increasing: %d, %d", id1, id2)
	}
}

// ---------------------------------------------------------------------------
// sendHealth (circuit breaker) unit tests
// ---------------------------------------------------------------------------

func TestSendHealthInitiallyHealthy(t *testing.T) {
	var h sendHealth
	if h.shouldSkip() {
		t.Error("fresh sendHealth should not skip")
	}
}

func TestSendHealthBelowThresholdNeverSkips(t *testing.T) {
	var h sendHealth
	for i := uint32(0); i < circuitBreakerThreshold-1; i++ {
		h.recordFailure()
	}
	if h.shouldSkip() {
		t.Error("should not skip when failures < threshold")
	}
}

func TestSendHealthTripsAtThreshold(t *testing.T) {
	var h sendHealth
	for i := uint32(0); i < circuitBreakerThreshold; i++ {
		h.recordFailure()
	}
	// After reaching threshold, most calls should skip (all except every probeInterval-th).
	skipped := 0
	for i := 0; i < 100; i++ {
		if h.shouldSkip() {
			skipped++
		}
	}
	// Expected: 100 - (100/probeInterval) = 100 - 4 = 96 skips.
	expectedProbes := 100 / int(circuitBreakerProbeInterval)
	expectedSkips := 100 - expectedProbes
	if skipped != expectedSkips {
		t.Errorf("skipped %d out of 100, want %d (probeInterval=%d)", skipped, expectedSkips, circuitBreakerProbeInterval)
	}
}

func TestSendHealthProbeAllowedPeriodically(t *testing.T) {
	var h sendHealth
	for i := uint32(0); i < circuitBreakerThreshold; i++ {
		h.recordFailure()
	}
	// The first probe should occur at skip count == probeInterval.
	probeCount := 0
	for i := uint32(0); i < circuitBreakerProbeInterval*2; i++ {
		if !h.shouldSkip() {
			probeCount++
		}
	}
	if probeCount != 2 {
		t.Errorf("expected 2 probes in %d calls, got %d", circuitBreakerProbeInterval*2, probeCount)
	}
}

func TestSendHealthRecoveryResetsState(t *testing.T) {
	var h sendHealth
	for i := uint32(0); i < circuitBreakerThreshold; i++ {
		h.recordFailure()
	}
	if !h.shouldSkip() {
		// consume the first non-probe skip — the probe is at probeInterval boundary
		// (this is expected to skip for the first call after threshold)
	}

	wasTripped := h.recordSuccess()
	if !wasTripped {
		t.Error("recordSuccess should report that breaker was tripped")
	}
	// After recovery, shouldSkip must return false.
	if h.shouldSkip() {
		t.Error("should not skip after recovery")
	}
	// failures and skips should be zero.
	if h.failures.Load() != 0 {
		t.Errorf("failures should be 0, got %d", h.failures.Load())
	}
	if h.skips.Load() != 0 {
		t.Errorf("skips should be 0, got %d", h.skips.Load())
	}
}

func TestSendHealthRecordSuccessWhenHealthy(t *testing.T) {
	var h sendHealth
	h.recordFailure() // 1 failure, below threshold
	wasTripped := h.recordSuccess()
	if wasTripped {
		t.Error("recordSuccess should return false when breaker was not tripped")
	}
}

// ---------------------------------------------------------------------------
// Broadcast circuit breaker integration tests
// ---------------------------------------------------------------------------

func TestBroadcastCircuitBreakerSkipsFailingClient(t *testing.T) {
	room := NewRoom()

	sender := &Client{Username: "alice", session: &mockSender{}}
	healthy := &Client{Username: "bob", session: &mockSender{}}
	failing := &Client{Username: "carol", session: &mockSender{err: fmt.Errorf("connection dead")}}

	room.AddClient(sender)
	room.AddClient(healthy)
	room.AddClient(failing)
	sender.channelID.Store(1)
	healthy.channelID.Store(1)
	failing.channelID.Store(1)

	data := []byte{0, 0, 0, 1, 0xAA, 0xBB}

	// Send enough datagrams to trip the circuit breaker on the failing client.
	for i := 0; i < int(circuitBreakerThreshold)+10; i++ {
		room.Broadcast(sender.ID, data)
	}

	// The healthy client should have received all datagrams.
	healthySender := healthy.session.(*mockSender)
	healthySender.mu.Lock()
	healthyCount := len(healthySender.received)
	healthySender.mu.Unlock()
	total := int(circuitBreakerThreshold) + 10
	if healthyCount != total {
		t.Errorf("healthy client received %d, want %d", healthyCount, total)
	}

	// The failing client's circuit breaker should be open.
	if failing.health.failures.Load() < circuitBreakerThreshold {
		t.Errorf("failures=%d, want >= %d", failing.health.failures.Load(), circuitBreakerThreshold)
	}

	// Stats should show some skipped datagrams.
	_, _, skipped, _ := room.Stats()
	if skipped == 0 {
		t.Error("expected skipped datagrams > 0 after circuit breaker tripped")
	}
}

func TestBroadcastCircuitBreakerRecovery(t *testing.T) {
	room := NewRoom()

	sender := &Client{Username: "alice", session: &mockSender{}}
	flaky := &mockSender{err: fmt.Errorf("temporary failure")}
	receiver := &Client{Username: "bob", session: flaky}

	room.AddClient(sender)
	room.AddClient(receiver)
	sender.channelID.Store(1)
	receiver.channelID.Store(1)

	data := []byte{0, 0, 0, 1, 0xAA, 0xBB}

	// Trip the circuit breaker.
	for i := 0; i < int(circuitBreakerThreshold)+int(circuitBreakerProbeInterval); i++ {
		room.Broadcast(sender.ID, data)
	}
	if receiver.health.failures.Load() < circuitBreakerThreshold {
		t.Fatal("circuit breaker should be tripped")
	}

	// Fix the connection.
	flaky.mu.Lock()
	flaky.err = nil
	flaky.mu.Unlock()

	// Send enough datagrams for a probe to land and succeed.
	for i := 0; i < int(circuitBreakerProbeInterval)*2; i++ {
		room.Broadcast(sender.ID, data)
	}

	// The breaker should have recovered.
	if receiver.health.failures.Load() != 0 {
		t.Errorf("failures should be 0 after recovery, got %d", receiver.health.failures.Load())
	}

	// The receiver should have received at least the post-recovery datagrams.
	flaky.mu.Lock()
	received := len(flaky.received)
	flaky.mu.Unlock()
	if received == 0 {
		t.Error("receiver should have received datagrams after recovery")
	}
}

func TestBroadcastCircuitBreakerStatsSkippedCount(t *testing.T) {
	room := NewRoom()

	sender := &Client{Username: "alice", session: &mockSender{}}
	dead := &Client{Username: "dead", session: &mockSender{err: fmt.Errorf("gone")}}

	room.AddClient(sender)
	room.AddClient(dead)
	sender.channelID.Store(1)
	dead.channelID.Store(1)

	data := []byte{0, 0, 0, 1, 0xAA}

	// Trip the breaker, then send more to accumulate skips.
	totalSends := int(circuitBreakerThreshold) + 100
	for i := 0; i < totalSends; i++ {
		room.Broadcast(sender.ID, data)
	}

	_, _, skipped, _ := room.Stats()
	// After the threshold, ~100 more sends happen. Most should be skipped
	// (all except probes: 100/probeInterval).
	expectedProbes := 100 / int(circuitBreakerProbeInterval)
	expectedSkipped := uint64(100 - expectedProbes)
	if skipped < expectedSkipped-2 || skipped > expectedSkipped+2 {
		t.Errorf("skipped=%d, want ~%d (tolerance ±2)", skipped, expectedSkipped)
	}
}

// --- ChannelCount ---

func TestChannelCountEmpty(t *testing.T) {
	room := NewRoom()
	if room.ChannelCount() != 0 {
		t.Errorf("ChannelCount: got %d, want 0", room.ChannelCount())
	}
}

func TestChannelCountAfterSetChannels(t *testing.T) {
	room := NewRoom()
	room.SetChannels([]ChannelInfo{{ID: 1, Name: "General"}, {ID: 2, Name: "Music"}})
	if room.ChannelCount() != 2 {
		t.Errorf("ChannelCount: got %d, want 2", room.ChannelCount())
	}
}

// ---------------------------------------------------------------------------
// Phase 8: Administration tests
// ---------------------------------------------------------------------------

func TestSetClientRole(t *testing.T) {
	room := NewRoom()
	c := newTestClient("alice")
	room.AddClient(c)

	room.SetClientRole(c.ID, RoleAdmin)
	if got := room.GetClientRole(c.ID); got != RoleAdmin {
		t.Errorf("role: got %q, want %q", got, RoleAdmin)
	}

	// Non-existent client returns USER.
	if got := room.GetClientRole(9999); got != RoleUser {
		t.Errorf("missing client role: got %q, want %q", got, RoleUser)
	}
}

func TestSetClientMute(t *testing.T) {
	room := NewRoom()
	c := newTestClient("alice")
	room.AddClient(c)

	if room.IsClientMuted(c.ID) {
		t.Error("client should not be muted initially")
	}

	room.SetClientMute(c.ID, true, 0)
	if !room.IsClientMuted(c.ID) {
		t.Error("client should be muted after SetClientMute")
	}

	room.SetClientMute(c.ID, false, 0)
	if room.IsClientMuted(c.ID) {
		t.Error("client should be unmuted")
	}
}

func TestMuteExpiry(t *testing.T) {
	room := NewRoom()
	c := newTestClient("alice")
	room.AddClient(c)

	// Set mute that expires in the past.
	pastExpiry := time.Now().Add(-1 * time.Second).UnixMilli()
	room.SetClientMute(c.ID, true, pastExpiry)

	// Should not be muted because expiry has passed.
	if room.IsClientMuted(c.ID) {
		t.Error("client should not be muted after expiry passed")
	}
}

func TestCheckMuteExpiry(t *testing.T) {
	room := NewRoom()
	c := newTestClient("alice")
	room.AddClient(c)

	pastExpiry := time.Now().Add(-1 * time.Second).UnixMilli()
	room.SetClientMute(c.ID, true, pastExpiry)

	room.CheckMuteExpiry()

	// After CheckMuteExpiry, the mute flag should be cleared.
	room.mu.RLock()
	isMuted := c.muted
	room.mu.RUnlock()
	if isMuted {
		t.Error("CheckMuteExpiry should have cleared the mute flag")
	}
}

func TestSetAnnouncement(t *testing.T) {
	room := NewRoom()
	room.SetAnnouncement("Maintenance tonight", "admin")

	content, user := room.GetAnnouncement()
	if content != "Maintenance tonight" {
		t.Errorf("announcement content: got %q", content)
	}
	if user != "admin" {
		t.Errorf("announcement user: got %q", user)
	}
}

func TestSlowMode(t *testing.T) {
	room := NewRoom()
	room.SetSlowMode(1, 5)

	if room.GetSlowMode(1) != 5 {
		t.Errorf("slow mode: got %d, want 5", room.GetSlowMode(1))
	}

	// Channel without slow mode.
	if room.GetSlowMode(2) != 0 {
		t.Errorf("slow mode for unconfigured channel: got %d, want 0", room.GetSlowMode(2))
	}

	// Remove slow mode.
	room.SetSlowMode(1, 0)
	if room.GetSlowMode(1) != 0 {
		t.Errorf("slow mode after removal: got %d, want 0", room.GetSlowMode(1))
	}
}

func TestCheckSlowModeAllowsFirst(t *testing.T) {
	room := NewRoom()
	c := newTestClient("alice")
	room.AddClient(c)

	room.SetSlowMode(1, 60) // 60 second cooldown

	// First message should be allowed.
	if !room.CheckSlowMode(c.ID, 1) {
		t.Error("first message should be allowed")
	}
	// Second immediate message should be denied.
	if room.CheckSlowMode(c.ID, 1) {
		t.Error("second immediate message should be denied by slow mode")
	}
}

func TestCheckSlowModeExemptsAdmins(t *testing.T) {
	room := NewRoom()
	c := newTestClient("alice")
	room.AddClient(c)
	room.SetClientRole(c.ID, RoleAdmin)

	room.SetSlowMode(1, 60)

	// Admin should be exempt from slow mode.
	if !room.CheckSlowMode(c.ID, 1) {
		t.Error("first message should be allowed for admin")
	}
	if !room.CheckSlowMode(c.ID, 1) {
		t.Error("admin should be exempt from slow mode")
	}
}

func TestBroadcastBlocksMutedSender(t *testing.T) {
	room := NewRoom()

	sender := &Client{Username: "alice", session: &mockSender{}}
	receiver := &Client{Username: "bob", session: &mockSender{}}

	room.AddClient(sender)
	room.AddClient(receiver)
	sender.channelID.Store(1)
	receiver.channelID.Store(1)

	// Mute the sender.
	room.SetClientMute(sender.ID, true, 0)

	data := []byte{0, 0, 0, 1, 0xAA, 0xBB}
	room.Broadcast(sender.ID, data)

	// Receiver should not have received anything.
	ms := receiver.session.(*mockSender)
	ms.mu.Lock()
	count := len(ms.received)
	ms.mu.Unlock()
	if count != 0 {
		t.Errorf("muted sender's datagrams should be blocked, got %d", count)
	}
}

func TestAuditLogCallback(t *testing.T) {
	room := NewRoom()
	var logged []string
	room.SetOnAuditLog(func(actorID int, actorName, action, target, details string) {
		logged = append(logged, action)
	})

	room.AuditLog(1, "alice", "ban", "bob", "{}")
	if len(logged) != 1 || logged[0] != "ban" {
		t.Errorf("audit log callback: got %v", logged)
	}
}

func TestRecordBanCallsCallback(t *testing.T) {
	room := NewRoom()
	var banCalled bool
	room.SetOnBan(func(pubkey, ip, reason, bannedBy string, durationS int) {
		banCalled = true
	})
	room.SetOnAuditLog(func(actorID int, actorName, action, target, details string) {})

	room.RecordBan("alice", "1.2.3.4", "spam", "admin", 0)
	if !banCalled {
		t.Error("ban callback should have been called")
	}
}

func TestRemoveBanCallsCallback(t *testing.T) {
	room := NewRoom()
	var removedID int64
	room.SetOnUnban(func(banID int64) {
		removedID = banID
	})

	room.RemoveBan(42)
	if removedID != 42 {
		t.Errorf("unban callback: got %d, want 42", removedID)
	}
}

func TestClientsIncludesRoleAndMuted(t *testing.T) {
	room := NewRoom()
	c := newTestClient("alice")
	room.AddClient(c)
	room.SetClientRole(c.ID, RoleAdmin)
	room.SetClientMute(c.ID, true, 0)

	users := room.Clients()
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].Role != RoleAdmin {
		t.Errorf("role: got %q, want %q", users[0].Role, RoleAdmin)
	}
	if !users[0].Muted {
		t.Error("expected user to be reported as muted")
	}
}

// ---------------------------------------------------------------------------
// Phase 8: HasPermission tests
// ---------------------------------------------------------------------------

func TestHasPermission(t *testing.T) {
	tests := []struct {
		role   string
		action string
		want   bool
	}{
		{RoleOwner, "set_role", true},
		{RoleAdmin, "set_role", false},
		{RoleAdmin, "ban", true},
		{RoleModerator, "kick", true},
		{RoleModerator, "ban", false},
		{RoleUser, "kick", false},
		{RoleUser, "chat", true},
		{RoleOwner, "announce", true},
		{RoleAdmin, "announce", false},
	}

	for _, tt := range tests {
		got := HasPermission(tt.role, tt.action)
		if got != tt.want {
			t.Errorf("HasPermission(%q, %q) = %v, want %v", tt.role, tt.action, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Phase 10: Performance & Reliability tests
// ---------------------------------------------------------------------------

func TestCanConnect(t *testing.T) {
	room := NewRoom()
	room.SetMaxConnections(2)

	c1 := newTestClient("alice")
	c2 := newTestClient("bob")
	room.AddClient(c1)
	room.AddClient(c2)

	if room.CanConnect("1.2.3.4") {
		t.Error("should not allow connection when max reached")
	}

	room.RemoveClient(c1.ID)
	if !room.CanConnect("1.2.3.4") {
		t.Error("should allow connection after removal")
	}
}

func TestPerIPLimit(t *testing.T) {
	room := NewRoom()
	room.SetPerIPLimit(2)

	room.TrackIPConnect("1.2.3.4")
	room.TrackIPConnect("1.2.3.4")

	if room.CanConnect("1.2.3.4") {
		t.Error("should not allow connection from IP over limit")
	}
	if !room.CanConnect("5.6.7.8") {
		t.Error("should allow connection from different IP")
	}

	room.TrackIPDisconnect("1.2.3.4")
	if !room.CanConnect("1.2.3.4") {
		t.Error("should allow connection after disconnect")
	}
}

func TestTrackIPDisconnectCleansUp(t *testing.T) {
	room := NewRoom()
	room.TrackIPConnect("1.2.3.4")
	room.TrackIPDisconnect("1.2.3.4")

	room.mu.RLock()
	count := room.ipConnections["1.2.3.4"]
	room.mu.RUnlock()
	if count != 0 {
		t.Errorf("expected 0 connections for IP after disconnect, got %d", count)
	}
}

func TestControlRateLimit(t *testing.T) {
	room := NewRoom()
	room.SetControlRateLimit(3) // 3 per second

	c := newTestClient("alice")
	room.AddClient(c)

	// First 3 should be allowed.
	for i := 0; i < 3; i++ {
		if !room.CheckControlRate(c.ID) {
			t.Errorf("message %d should be allowed", i+1)
		}
	}
	// 4th should be denied.
	if room.CheckControlRate(c.ID) {
		t.Error("4th message should be denied by rate limit")
	}
}

func TestBufferAndGetMessages(t *testing.T) {
	room := NewRoom()

	msg1 := ControlMsg{Type: "chat", Message: "hello"}
	msg2 := ControlMsg{Type: "chat", Message: "world"}

	room.BufferMessage(1, msg1)
	room.BufferMessage(1, msg2)

	// Sequence numbers should be assigned.
	if room.GetChannelSeq(1) != 2 {
		t.Errorf("channel seq: got %d, want 2", room.GetChannelSeq(1))
	}

	// Get all messages.
	msgs := room.GetMessagesSince(1, 0)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Message != "hello" || msgs[1].Message != "world" {
		t.Errorf("unexpected messages: %v", msgs)
	}

	// Get messages since seq 1.
	msgs = room.GetMessagesSince(1, 1)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after seq 1, got %d", len(msgs))
	}
	if msgs[0].Message != "world" {
		t.Errorf("expected 'world', got %q", msgs[0].Message)
	}
}

func TestBufferMessageLimitsSize(t *testing.T) {
	room := NewRoom()

	for i := 0; i < maxMsgBuffer+100; i++ {
		room.BufferMessage(1, ControlMsg{Type: "chat", Message: fmt.Sprintf("msg-%d", i)})
	}

	msgs := room.GetMessagesSince(1, 0)
	if len(msgs) != maxMsgBuffer {
		t.Errorf("expected buffer capped at %d, got %d", maxMsgBuffer, len(msgs))
	}
}

func TestBufferMessageIgnoresChannelZero(t *testing.T) {
	room := NewRoom()
	room.BufferMessage(0, ControlMsg{Type: "chat", Message: "test"})

	msgs := room.GetMessagesSince(0, 0)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for channel 0, got %d", len(msgs))
	}
}
