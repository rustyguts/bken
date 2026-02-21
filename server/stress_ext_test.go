package main

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Concurrent WebSocket-like message sends
// ---------------------------------------------------------------------------

func TestStressConcurrentChatMessages(t *testing.T) {
	room := NewRoom()
	const numClients = 20
	const msgsPerClient = 50

	clients := make([]*Client, numClients)
	buffers := make([]*bytes.Buffer, numClients)
	for i := range clients {
		c, buf := newCtrlClient(fmt.Sprintf("user-%d", i))
		room.AddClient(c)
		clients[i] = c
		buffers[i] = buf
	}

	var wg sync.WaitGroup
	for i := range clients {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < msgsPerClient; j++ {
				processControl(ControlMsg{
					Type:    "chat",
					Message: fmt.Sprintf("msg-%d-%d", idx, j),
				}, clients[idx], room)
			}
		}(i)
	}
	wg.Wait()

	// If we got here without panics or deadlocks, the test passes.
	// Verify every client received some messages.
	for i, buf := range buffers {
		if buf.Len() == 0 {
			t.Errorf("client %d received no messages", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Race condition: concurrent channel create/delete with users joining
// ---------------------------------------------------------------------------

func TestStressConcurrentChannelOpsWithJoins(t *testing.T) {
	room := NewRoom()
	room.SetOnCreateChannel(func(name string) (int64, error) { return 1, nil })
	room.SetOnDeleteChannel(func(id int64) error { return nil })
	room.SetOnRefreshChannels(func() ([]ChannelInfo, error) {
		return []ChannelInfo{{ID: 1, Name: "General"}}, nil
	})
	room.SetChannels([]ChannelInfo{{ID: 1, Name: "General"}, {ID: 2, Name: "Extra"}})

	owner, _ := newCtrlClient("owner")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	const numWorkers = 10
	var wg sync.WaitGroup

	// Concurrent channel create/delete.
	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				processControl(ControlMsg{Type: "create_channel", Message: fmt.Sprintf("ch-%d-%d", idx, j)}, owner, room)
			}
		}(i)
	}

	// Concurrent user joins.
	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func(idx int) {
			defer wg.Done()
			c, _ := newCtrlClient(fmt.Sprintf("joiner-%d", idx))
			room.AddClient(c)
			for j := 0; j < 20; j++ {
				processControl(ControlMsg{Type: "join_channel", ChannelID: int64(j%3 + 1)}, c, room)
			}
		}(i)
	}

	wg.Wait()
	// No panics or deadlocks = pass.
}

// ---------------------------------------------------------------------------
// Race condition: concurrent room operations (add/remove/broadcast)
// ---------------------------------------------------------------------------

func TestStressConcurrentRoomOperations(t *testing.T) {
	room := NewRoom()
	const numClients = 30
	const iterations = 100

	var wg sync.WaitGroup

	// Concurrent add/remove.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			c := &Client{Username: fmt.Sprintf("temp-%d", i), session: &mockSender{}}
			id := room.AddClient(c)
			c.channelID.Store(1)
			room.RemoveClient(id)
		}
	}()

	// Concurrent broadcasts.
	sender := &Client{Username: "sender", session: &mockSender{}}
	room.AddClient(sender)
	sender.channelID.Store(1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		data := []byte{0, 0, 0, 1, 0xAA}
		for i := 0; i < iterations; i++ {
			room.Broadcast(sender.ID, data)
		}
	}()

	// Concurrent Clients() snapshot reads.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = room.Clients()
		}
	}()

	// Concurrent chat messages.
	wg.Add(1)
	go func() {
		defer wg.Done()
		chatSender, _ := newCtrlClient("chat-sender")
		room.AddClient(chatSender)
		for i := 0; i < iterations; i++ {
			processControl(ControlMsg{Type: "chat", Message: fmt.Sprintf("stress-%d", i)}, chatSender, room)
		}
	}()

	wg.Wait()
}

// ---------------------------------------------------------------------------
// Race condition: concurrent message reactions
// ---------------------------------------------------------------------------

func TestStressConcurrentReactions(t *testing.T) {
	room := NewRoom()
	const numUsers = 20
	emojis := []string{"👍", "😂", "🔥", "❤️", "👀"}

	// Create messages to react to.
	for i := uint64(1); i <= 10; i++ {
		room.RecordMsg(i, uint16(i), "user", "msg", 1)
	}

	var wg sync.WaitGroup
	for i := 0; i < numUsers; i++ {
		wg.Add(1)
		go func(uid int) {
			defer wg.Done()
			for msgID := uint64(1); msgID <= 10; msgID++ {
				for _, emoji := range emojis {
					room.AddReaction(msgID, uint16(uid), emoji)
				}
			}
			// Remove some.
			for msgID := uint64(1); msgID <= 5; msgID++ {
				room.RemoveReaction(msgID, uint16(uid), emojis[0])
			}
		}(i)
	}
	wg.Wait()

	// Verify GetReactions doesn't crash.
	for msgID := uint64(1); msgID <= 10; msgID++ {
		reactions := room.GetReactions(msgID)
		if reactions == nil && msgID > 5 {
			// Messages 6-10 should still have all emojis.
			t.Errorf("msg %d should have reactions", msgID)
		}
	}
}

// ---------------------------------------------------------------------------
// Race condition: concurrent search and record
// ---------------------------------------------------------------------------

func TestStressConcurrentSearchAndRecord(t *testing.T) {
	room := NewRoom()
	var wg sync.WaitGroup

	// Writer goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := uint64(1); i <= 200; i++ {
			room.RecordMsg(i, 1, "alice", fmt.Sprintf("searchable msg %d", i), 1)
		}
	}()

	// Reader goroutines.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = room.SearchMessages(1, "searchable", 0, 20)
			}
		}()
	}

	wg.Wait()
}

// ---------------------------------------------------------------------------
// Race condition: concurrent pin/unpin
// ---------------------------------------------------------------------------

func TestStressConcurrentPinUnpin(t *testing.T) {
	room := NewRoom()
	for i := uint64(1); i <= 30; i++ {
		room.RecordMsg(i, 1, "alice", "msg", 1)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(uid int) {
			defer wg.Done()
			for msgID := uint64(1); msgID <= 25; msgID++ {
				room.PinMessage(msgID, 1, uint16(uid))
			}
			for msgID := uint64(1); msgID <= 15; msgID++ {
				room.UnpinMessage(msgID)
			}
		}(i)
	}
	wg.Wait()

	// GetPinnedMessages should not panic.
	_ = room.GetPinnedMessages(1)
}

// ---------------------------------------------------------------------------
// Rate limiting under concurrent access
// ---------------------------------------------------------------------------

func TestStressConcurrentRateLimiting(t *testing.T) {
	room := NewRoom()
	room.SetControlRateLimit(100)

	const numClients = 10
	clients := make([]*Client, numClients)
	for i := range clients {
		c := newTestClient(fmt.Sprintf("user-%d", i))
		room.AddClient(c)
		clients[i] = c
	}

	var wg sync.WaitGroup
	for i := range clients {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = room.CheckControlRate(clients[idx].ID)
			}
		}(i)
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Connection tracking under concurrent access
// ---------------------------------------------------------------------------

func TestStressConcurrentIPTracking(t *testing.T) {
	room := NewRoom()
	room.SetPerIPLimit(100)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ip := fmt.Sprintf("10.0.0.%d", idx)
			for j := 0; j < 50; j++ {
				room.TrackIPConnect(ip)
				_ = room.CanConnect(ip)
				room.TrackIPDisconnect(ip)
			}
		}(i)
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Buffer messages under concurrent access
// ---------------------------------------------------------------------------

func TestStressConcurrentBufferMessages(t *testing.T) {
	room := NewRoom()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(ch int64) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				room.BufferMessage(ch, ControlMsg{Type: "chat", Message: fmt.Sprintf("msg-%d", j)})
			}
		}(int64(i + 1))
	}

	// Concurrent reads.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = room.GetMessagesSince(1, 0)
				_ = room.GetChannelSeq(1)
			}
		}()
	}

	wg.Wait()
}
