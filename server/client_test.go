package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// newCtrlClient returns a Client whose ctrl writer is a bytes.Buffer so tests
// can inspect what SendControl writes without a real WebTransport stream.
func newCtrlClient(username string) (*Client, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return &Client{
		Username: username,
		session:  &mockSender{},
		ctrl:     buf,
	}, buf
}

// decodeControl unmarshals the first newline-delimited JSON line from buf.
func decodeControl(t *testing.T, buf *bytes.Buffer) ControlMsg {
	t.Helper()
	line, err := buf.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read line: %v", err)
	}
	var msg ControlMsg
	if err := json.Unmarshal(bytes.TrimRight(line, "\n"), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return msg
}

// --- SendControl ---

func TestSendControl(t *testing.T) {
	c, buf := newCtrlClient("alice")

	c.SendControl(ControlMsg{Type: "pong", Timestamp: 9999})

	got := decodeControl(t, buf)
	if got.Type != "pong" {
		t.Errorf("type: got %q, want %q", got.Type, "pong")
	}
	if got.Timestamp != 9999 {
		t.Errorf("timestamp: got %d, want 9999", got.Timestamp)
	}
}

func TestSendControlNilCtrl(t *testing.T) {
	c := &Client{ID: 1} // ctrl is nil
	// Must not panic.
	c.SendControl(ControlMsg{Type: "pong"})
}

func TestSendControlAppendNewline(t *testing.T) {
	c, buf := newCtrlClient("alice")
	c.SendControl(ControlMsg{Type: "ping"})
	raw := buf.Bytes()
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		t.Errorf("expected trailing newline, got %q", raw)
	}
}

// --- processControl: ping ---

func TestProcessControlPingRepliesWithPong(t *testing.T) {
	room := NewRoom()
	client, buf := newCtrlClient("alice")
	room.AddClient(client)

	processControl(ControlMsg{Type: "ping", Timestamp: 1234}, client, room)

	got := decodeControl(t, buf)
	if got.Type != "pong" {
		t.Errorf("type: got %q, want %q", got.Type, "pong")
	}
	if got.Timestamp != 1234 {
		t.Errorf("timestamp echo: got %d, want 1234", got.Timestamp)
	}
}

// --- processControl: chat ---

func TestProcessControlChatBroadcastsToAll(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(sender)
	room.AddClient(receiver)

	processControl(ControlMsg{Type: "chat", Message: "hello"}, sender, room)

	for _, tc := range []struct {
		name string
		buf  *bytes.Buffer
	}{
		{"sender", senderBuf},
		{"receiver", receiverBuf},
	} {
		got := decodeControl(t, tc.buf)
		if got.Type != "chat" {
			t.Errorf("%s: type: got %q, want %q", tc.name, got.Type, "chat")
		}
		if got.Username != "alice" {
			t.Errorf("%s: username: got %q, want %q", tc.name, got.Username, "alice")
		}
		if got.Message != "hello" {
			t.Errorf("%s: message: got %q, want %q", tc.name, got.Message, "hello")
		}
		if got.ID != sender.ID {
			t.Errorf("%s: id: got %d, want %d", tc.name, got.ID, sender.ID)
		}
		if got.Timestamp == 0 {
			t.Errorf("%s: timestamp: got 0, want non-zero (server should stamp)", tc.name)
		}
	}
}

func TestProcessControlChatDropsEmptyMessage(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: ""}, sender, room)

	if senderBuf.Len() != 0 {
		t.Errorf("expected no broadcast for empty message, got %d bytes written", senderBuf.Len())
	}
}

func TestProcessControlChatDropsTooLong(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: strings.Repeat("a", 501)}, sender, room)

	if senderBuf.Len() != 0 {
		t.Errorf("expected no broadcast for 501-char message, got %d bytes written", senderBuf.Len())
	}
}

func TestProcessControlChatExactly500Chars(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: strings.Repeat("a", 500)}, sender, room)

	if senderBuf.Len() == 0 {
		t.Error("expected broadcast for exactly-500-char message, got nothing")
	}
	got := decodeControl(t, senderBuf)
	if got.Type != "chat" {
		t.Errorf("type: got %q, want %q", got.Type, "chat")
	}
}

func TestProcessControlChatStampsServerUsername(t *testing.T) {
	room := NewRoom()
	// Attacker tries to spoof a different username in the message.
	sender, buf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Username: "SPOOFED", Message: "hi"}, sender, room)

	got := decodeControl(t, buf)
	if got.Username != "alice" {
		t.Errorf("username should be server-stamped 'alice', got %q", got.Username)
	}
}

// --- processControl: channel-scoped chat ---

func TestProcessControlChatBroadcastsToAllClients(t *testing.T) {
	room := NewRoom()

	sender, senderBuf := newCtrlClient("alice")
	sender.channelID.Store(1)
	room.AddClient(sender)

	inChannel, inBuf := newCtrlClient("bob")
	inChannel.channelID.Store(1)
	room.AddClient(inChannel)

	otherChannel, otherBuf := newCtrlClient("carol")
	otherChannel.channelID.Store(2)
	room.AddClient(otherChannel)

	lobby, lobbyBuf := newCtrlClient("dave")
	// lobby.channelID = 0
	room.AddClient(lobby)

	// All chat is broadcast to every client with the client-supplied channelID.
	processControl(ControlMsg{Type: "chat", Message: "channel hello", ChannelID: 99}, sender, room)

	// Every client receives the message.
	if senderBuf.Len() == 0 {
		t.Error("sender should receive their own message")
	}
	got := decodeControl(t, senderBuf)
	if got.ChannelID != 99 {
		t.Errorf("channel_id should be the client-supplied value (99), got %d", got.ChannelID)
	}
	if inBuf.Len() == 0 {
		t.Error("peer in same channel should receive the message")
	}
	if otherBuf.Len() == 0 {
		t.Error("client in different channel should receive the message (cross-channel chat)")
	}
	if lobbyBuf.Len() == 0 {
		t.Error("lobby client should receive the message (cross-channel chat)")
	}
}

func TestProcessControlChatCrossChannelAllowed(t *testing.T) {
	room := NewRoom()

	// Client is in channel 1 and sends to channel 2 — this is allowed (cross-channel chat).
	sender, senderBuf := newCtrlClient("alice")
	sender.channelID.Store(1)
	room.AddClient(sender)

	target, targetBuf := newCtrlClient("bob")
	target.channelID.Store(2)
	room.AddClient(target)

	processControl(ControlMsg{Type: "chat", Message: "cross channel", ChannelID: 2}, sender, room)

	// Both clients receive the message with the client-supplied channelID.
	if senderBuf.Len() == 0 {
		t.Error("sender should receive their own message")
	}
	if targetBuf.Len() == 0 {
		t.Error("target should receive the cross-channel message")
	}
	got := decodeControl(t, targetBuf)
	if got.ChannelID != 2 {
		t.Errorf("channel_id should be preserved as client-supplied (2), got %d", got.ChannelID)
	}
}

func TestProcessControlChatFromLobbyPreservesChannelID(t *testing.T) {
	room := NewRoom()

	// Client is in the lobby (channelID=0) but sends to channel 1 — allowed.
	sender, senderBuf := newCtrlClient("alice")
	// sender.channelID = 0 (default)
	room.AddClient(sender)

	observer, observerBuf := newCtrlClient("bob")
	observer.channelID.Store(1)
	room.AddClient(observer)

	processControl(ControlMsg{Type: "chat", Message: "from lobby", ChannelID: 1}, sender, room)

	// Both receive it with the client-supplied channelID preserved.
	if senderBuf.Len() == 0 {
		t.Error("sender should receive the message")
	}
	got := decodeControl(t, senderBuf)
	if got.ChannelID != 1 {
		t.Errorf("channel_id should be preserved as client-supplied (1), got %d", got.ChannelID)
	}
	if observerBuf.Len() == 0 {
		t.Error("observer should receive the message")
	}
}

// --- processControl: kick ---

// mockCloser records whether Close was called.
type mockCloser struct{ closed bool }

func (m *mockCloser) Close() error { m.closed = true; return nil }

// newKickableClient returns a Client with a cancel func and closer, both observable in tests.
func newKickableClient(username string) (*Client, *bytes.Buffer, context.CancelFunc, *mockCloser) {
	buf := &bytes.Buffer{}
	ctx, cancel := context.WithCancel(context.Background())
	_ = ctx // cancel is what we observe
	mc := &mockCloser{}
	c := &Client{
		Username: username,
		session:  &mockSender{},
		ctrl:     buf,
		cancel:   cancel,
		closer:   mc,
	}
	return c, buf, cancel, mc
}

func TestProcessControlKickByOwner(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newCtrlClient("alice")
	target, targetBuf, _, targetCloser := newKickableClient("bob")
	room.AddClient(owner)
	room.AddClient(target)
	room.ClaimOwnership(owner.ID)

	_ = ownerBuf
	processControl(ControlMsg{Type: "kick", ID: target.ID}, owner, room)

	// Target should receive a "kicked" message.
	got := decodeControl(t, targetBuf)
	if got.Type != "kicked" {
		t.Errorf("target: type: got %q, want %q", got.Type, "kicked")
	}
	// Target's closer should have been called.
	if !targetCloser.closed {
		t.Error("target closer should have been called")
	}
}

func TestProcessControlKickByNonOwner(t *testing.T) {
	room := NewRoom()
	owner, _ := newCtrlClient("alice")
	attacker, _ := newCtrlClient("eve")
	target, targetBuf, _, targetCloser := newKickableClient("bob")
	room.AddClient(owner)
	room.AddClient(attacker)
	room.AddClient(target)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "kick", ID: target.ID}, attacker, room)

	if targetBuf.Len() != 0 {
		t.Error("non-owner kick should be ignored: target should receive nothing")
	}
	if targetCloser.closed {
		t.Error("non-owner kick should not close target connection")
	}
}

func TestProcessControlKickSelf(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf, _, ownerCloser := newKickableClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "kick", ID: owner.ID}, owner, room)

	if ownerBuf.Len() != 0 {
		t.Error("owner should not be able to kick themselves")
	}
	if ownerCloser.closed {
		t.Error("owner closer should not be called when kicking self")
	}
}

func TestProcessControlKickUnknownTarget(t *testing.T) {
	room := NewRoom()
	owner, _, _, ownerCloser := newKickableClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	// Should not panic — target ID 9999 doesn't exist.
	processControl(ControlMsg{Type: "kick", ID: 9999}, owner, room)

	if ownerCloser.closed {
		t.Error("kicking unknown target should not affect owner")
	}
}

// --- processControl: rename ---

func TestProcessControlRenameByOwner(t *testing.T) {
	room := NewRoom()
	var persisted string
	room.SetOnRename(func(name string) error { persisted = name; return nil })

	owner, _ := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "rename", ServerName: "  Cool Server  "}, owner, room)

	if room.ServerName() != "Cool Server" {
		t.Errorf("ServerName: got %q, want %q", room.ServerName(), "Cool Server")
	}
	if persisted != "Cool Server" {
		t.Errorf("onRename callback: got %q, want %q", persisted, "Cool Server")
	}
}

func TestProcessControlRenameByNonOwner(t *testing.T) {
	room := NewRoom()
	owner, _ := newCtrlClient("alice")
	attacker, _ := newCtrlClient("eve")
	room.AddClient(owner)
	room.AddClient(attacker)
	room.ClaimOwnership(owner.ID)
	room.Rename("Original")

	var renameCalled bool
	room.SetOnRename(func(_ string) error { renameCalled = true; return nil })

	processControl(ControlMsg{Type: "rename", ServerName: "Hacked"}, attacker, room)

	if room.ServerName() != "Original" {
		t.Errorf("non-owner rename should be ignored, got %q", room.ServerName())
	}
	if renameCalled {
		t.Error("onRename callback should not fire for non-owner")
	}
}

func TestProcessControlRenameEmpty(t *testing.T) {
	room := NewRoom()
	room.Rename("Original")
	owner, _ := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "rename", ServerName: "   "}, owner, room)

	if room.ServerName() != "Original" {
		t.Errorf("empty rename should be ignored, got %q", room.ServerName())
	}
}

func TestProcessControlRenameTooLong(t *testing.T) {
	room := NewRoom()
	room.Rename("Original")
	owner, _ := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "rename", ServerName: string(make([]byte, 51))}, owner, room)

	if room.ServerName() != "Original" {
		t.Errorf("51-char rename should be ignored, got %q", room.ServerName())
	}
}

func TestProcessControlRenameExactly50Chars(t *testing.T) {
	room := NewRoom()
	owner, _ := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	name50 := "12345678901234567890123456789012345678901234567890" // 50 chars
	processControl(ControlMsg{Type: "rename", ServerName: name50}, owner, room)

	if room.ServerName() != name50 {
		t.Errorf("50-char rename should succeed, got %q", room.ServerName())
	}
}

// --- processControl: join_channel ---

func TestProcessControlJoinChannel(t *testing.T) {
	room := NewRoom()
	client, _ := newCtrlClient("alice")
	observer, observerBuf := newCtrlClient("bob")
	room.AddClient(client)
	room.AddClient(observer)

	processControl(ControlMsg{Type: "join_channel", ChannelID: 7}, client, room)

	// Client's channelID should be updated.
	if client.channelID.Load() != 7 {
		t.Errorf("channelID: got %d, want 7", client.channelID.Load())
	}

	// All clients (including sender) should receive user_channel broadcast.
	got := decodeControl(t, observerBuf)
	if got.Type != "user_channel" {
		t.Errorf("type: got %q, want %q", got.Type, "user_channel")
	}
	if got.ID != client.ID {
		t.Errorf("ID: got %d, want %d", got.ID, client.ID)
	}
	if got.ChannelID != 7 {
		t.Errorf("ChannelID: got %d, want 7", got.ChannelID)
	}
}

func TestProcessControlJoinChannelZeroLeaves(t *testing.T) {
	room := NewRoom()
	client, _ := newCtrlClient("alice")
	room.AddClient(client)
	client.channelID.Store(5)

	// Sending channel_id=0 means "leave all channels".
	processControl(ControlMsg{Type: "join_channel", ChannelID: 0}, client, room)

	if client.channelID.Load() != 0 {
		t.Errorf("channelID after leave: got %d, want 0", client.channelID.Load())
	}
}

func TestProcessControlJoinChannelClientSnapshotUpdated(t *testing.T) {
	room := NewRoom()
	client, _ := newCtrlClient("alice")
	room.AddClient(client)

	processControl(ControlMsg{Type: "join_channel", ChannelID: 3}, client, room)

	users := room.Clients()
	if len(users) != 1 || users[0].ChannelID != 3 {
		t.Errorf("Clients() snapshot: expected ChannelID=3, got %+v", users)
	}
}

// --- processControl: create_channel ---

func TestProcessControlCreateChannelByOwner(t *testing.T) {
	room := NewRoom()
	var createdName string
	room.SetOnCreateChannel(func(name string) (int64, error) { createdName = name; return 42, nil })
	room.SetOnRefreshChannels(func() ([]ChannelInfo, error) {
		return []ChannelInfo{{ID: 42, Name: createdName}}, nil
	})

	owner, _ := newCtrlClient("alice")
	observer, observerBuf := newCtrlClient("bob")
	room.AddClient(owner)
	room.AddClient(observer)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "create_channel", Message: "  Gaming  "}, owner, room)

	if createdName != "Gaming" {
		t.Errorf("onCreateChannel: got %q, want %q", createdName, "Gaming")
	}
	// Observer should receive a channel_list broadcast.
	got := decodeControl(t, observerBuf)
	if got.Type != "channel_list" {
		t.Errorf("broadcast type: got %q, want %q", got.Type, "channel_list")
	}
	if len(got.Channels) != 1 || got.Channels[0].Name != "Gaming" {
		t.Errorf("channels: got %+v, want [{42 Gaming}]", got.Channels)
	}
}

func TestProcessControlCreateChannelByNonOwner(t *testing.T) {
	room := NewRoom()
	var called bool
	room.SetOnCreateChannel(func(_ string) (int64, error) { called = true; return 0, nil })

	owner, _ := newCtrlClient("alice")
	attacker, _ := newCtrlClient("eve")
	room.AddClient(owner)
	room.AddClient(attacker)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "create_channel", Message: "Hacked"}, attacker, room)

	if called {
		t.Error("non-owner should not be able to create channels")
	}
}

func TestProcessControlCreateChannelEmptyName(t *testing.T) {
	room := NewRoom()
	var called bool
	room.SetOnCreateChannel(func(_ string) (int64, error) { called = true; return 0, nil })

	owner, _ := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "create_channel", Message: "   "}, owner, room)

	if called {
		t.Error("empty channel name should be rejected")
	}
}

// --- processControl: rename_channel ---

func TestProcessControlRenameChannelByOwner(t *testing.T) {
	room := NewRoom()
	var renamedID int64
	var renamedName string
	room.SetOnRenameChannel(func(id int64, name string) error {
		renamedID = id
		renamedName = name
		return nil
	})
	room.SetOnRefreshChannels(func() ([]ChannelInfo, error) {
		return []ChannelInfo{{ID: renamedID, Name: renamedName}}, nil
	})

	owner, _ := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "rename_channel", ChannelID: 5, Message: "Music"}, owner, room)

	if renamedID != 5 || renamedName != "Music" {
		t.Errorf("onRenameChannel: got (%d, %q), want (5, %q)", renamedID, renamedName, "Music")
	}
}

func TestProcessControlRenameChannelByNonOwner(t *testing.T) {
	room := NewRoom()
	var called bool
	room.SetOnRenameChannel(func(_ int64, _ string) error { called = true; return nil })

	owner, _ := newCtrlClient("alice")
	attacker, _ := newCtrlClient("eve")
	room.AddClient(owner)
	room.AddClient(attacker)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "rename_channel", ChannelID: 5, Message: "Hacked"}, attacker, room)

	if called {
		t.Error("non-owner should not be able to rename channels")
	}
}

func TestProcessControlRenameChannelZeroID(t *testing.T) {
	room := NewRoom()
	var called bool
	room.SetOnRenameChannel(func(_ int64, _ string) error { called = true; return nil })

	owner, _ := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "rename_channel", ChannelID: 0, Message: "Lobby2"}, owner, room)

	if called {
		t.Error("renaming channel 0 (lobby) should be rejected")
	}
}

// --- processControl: delete_channel ---

func TestProcessControlDeleteChannelByOwner(t *testing.T) {
	room := NewRoom()
	var deletedID int64
	room.SetOnDeleteChannel(func(id int64) error { deletedID = id; return nil })
	room.SetOnRefreshChannels(func() ([]ChannelInfo, error) { return nil, nil })

	// Need at least 2 channels so last-channel protection doesn't block deletion.
	room.SetChannels([]ChannelInfo{{ID: 5, Name: "Gaming"}, {ID: 6, Name: "Music"}})

	owner, _ := newCtrlClient("alice")
	inChannel, _ := newCtrlClient("bob")
	room.AddClient(owner)
	room.AddClient(inChannel)
	room.ClaimOwnership(owner.ID)
	inChannel.channelID.Store(5)

	processControl(ControlMsg{Type: "delete_channel", ChannelID: 5}, owner, room)

	if deletedID != 5 {
		t.Errorf("onDeleteChannel: got %d, want 5", deletedID)
	}
	// User in deleted channel should be moved to lobby.
	if inChannel.channelID.Load() != 0 {
		t.Errorf("user should be moved to lobby, got channel %d", inChannel.channelID.Load())
	}
}

func TestProcessControlDeleteChannelByNonOwner(t *testing.T) {
	room := NewRoom()
	var called bool
	room.SetOnDeleteChannel(func(_ int64) error { called = true; return nil })

	owner, _ := newCtrlClient("alice")
	attacker, _ := newCtrlClient("eve")
	room.AddClient(owner)
	room.AddClient(attacker)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "delete_channel", ChannelID: 5}, attacker, room)

	if called {
		t.Error("non-owner should not be able to delete channels")
	}
}

func TestProcessControlDeleteChannelZeroID(t *testing.T) {
	room := NewRoom()
	var called bool
	room.SetOnDeleteChannel(func(_ int64) error { called = true; return nil })

	owner, _ := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "delete_channel", ChannelID: 0}, owner, room)

	if called {
		t.Error("deleting channel 0 (lobby) should be rejected")
	}
}

// --- processControl: move_user ---

func TestProcessControlMoveUserByOwner(t *testing.T) {
	room := NewRoom()
	owner, _ := newCtrlClient("alice")
	target, _ := newCtrlClient("bob")
	observer, observerBuf := newCtrlClient("carol")
	room.AddClient(owner)
	room.AddClient(target)
	room.AddClient(observer)
	room.ClaimOwnership(owner.ID)

	target.channelID.Store(1)

	processControl(ControlMsg{Type: "move_user", ID: target.ID, ChannelID: 5}, owner, room)

	// Target's channelID should be updated.
	if target.channelID.Load() != 5 {
		t.Errorf("channelID: got %d, want 5", target.channelID.Load())
	}

	// All clients should receive user_channel broadcast.
	got := decodeControl(t, observerBuf)
	if got.Type != "user_channel" {
		t.Errorf("type: got %q, want %q", got.Type, "user_channel")
	}
	if got.ID != target.ID {
		t.Errorf("ID: got %d, want %d", got.ID, target.ID)
	}
	if got.ChannelID != 5 {
		t.Errorf("ChannelID: got %d, want 5", got.ChannelID)
	}
}

func TestProcessControlMoveUserByNonOwner(t *testing.T) {
	room := NewRoom()
	owner, _ := newCtrlClient("alice")
	attacker, _ := newCtrlClient("eve")
	target, targetBuf := newCtrlClient("bob")
	room.AddClient(owner)
	room.AddClient(attacker)
	room.AddClient(target)
	room.ClaimOwnership(owner.ID)
	target.channelID.Store(1)

	processControl(ControlMsg{Type: "move_user", ID: target.ID, ChannelID: 5}, attacker, room)

	// Target should not be moved.
	if target.channelID.Load() != 1 {
		t.Errorf("channelID should remain 1, got %d", target.channelID.Load())
	}
	if targetBuf.Len() != 0 {
		t.Error("non-owner move should not produce any broadcast to target")
	}
}

func TestProcessControlMoveUserSelf(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)
	owner.channelID.Store(1)

	processControl(ControlMsg{Type: "move_user", ID: owner.ID, ChannelID: 5}, owner, room)

	// Owner should not be able to move themselves via move_user.
	if owner.channelID.Load() != 1 {
		t.Errorf("channelID should remain 1, got %d", owner.channelID.Load())
	}
	if ownerBuf.Len() != 0 {
		t.Error("owner moving self should be ignored")
	}
}

func TestProcessControlMoveUserUnknownTarget(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	// Target ID 9999 doesn't exist — should not panic.
	processControl(ControlMsg{Type: "move_user", ID: 9999, ChannelID: 5}, owner, room)

	if ownerBuf.Len() != 0 {
		t.Error("moving unknown target should produce no broadcast")
	}
}

func TestProcessControlMoveUserToLobby(t *testing.T) {
	room := NewRoom()
	owner, _ := newCtrlClient("alice")
	target, _ := newCtrlClient("bob")
	room.AddClient(owner)
	room.AddClient(target)
	room.ClaimOwnership(owner.ID)
	target.channelID.Store(3)

	processControl(ControlMsg{Type: "move_user", ID: target.ID, ChannelID: 0}, owner, room)

	if target.channelID.Load() != 0 {
		t.Errorf("channelID: got %d, want 0", target.channelID.Load())
	}
}

func TestProcessControlMoveUserZeroID(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	// ID 0 is invalid.
	processControl(ControlMsg{Type: "move_user", ID: 0, ChannelID: 5}, owner, room)

	if ownerBuf.Len() != 0 {
		t.Error("move_user with ID=0 should be ignored")
	}
}

// --- processControl: unknown type ---

func TestProcessControlUnknownTypeIsIgnored(t *testing.T) {
	room := NewRoom()
	client, buf := newCtrlClient("alice")
	room.AddClient(client)

	// Should not panic and should write nothing.
	processControl(ControlMsg{Type: "unknown_msg_type"}, client, room)

	if buf.Len() != 0 {
		t.Errorf("expected no output for unknown message type, got %d bytes", buf.Len())
	}
}

// --- processControl: chat with file attachment ---

func TestProcessControlChatWithFileRelaysFileFields(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{
		Type:     "chat",
		FileID:   42,
		FileName: "photo.jpg",
		FileSize: 123456,
	}, sender, room)

	got := decodeControl(t, senderBuf)
	if got.Type != "chat" {
		t.Errorf("type: got %q, want %q", got.Type, "chat")
	}
	if got.FileID != 42 {
		t.Errorf("file_id: got %d, want 42", got.FileID)
	}
	if got.FileName != "photo.jpg" {
		t.Errorf("file_name: got %q, want %q", got.FileName, "photo.jpg")
	}
	if got.FileSize != 123456 {
		t.Errorf("file_size: got %d, want 123456", got.FileSize)
	}
}

func TestProcessControlChatEmptyMessageWithFileAllowed(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	// Empty message body but with a file attachment — should be allowed.
	processControl(ControlMsg{
		Type:     "chat",
		Message:  "",
		FileID:   1,
		FileName: "doc.pdf",
		FileSize: 5000,
	}, sender, room)

	if senderBuf.Len() == 0 {
		t.Error("expected broadcast for file-only chat, got nothing")
	}
	got := decodeControl(t, senderBuf)
	if got.FileID != 1 {
		t.Errorf("file_id: got %d, want 1", got.FileID)
	}
}

func TestProcessControlChatEmptyMessageWithoutFileDropped(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	// Empty message without file — should be dropped (existing behavior).
	processControl(ControlMsg{Type: "chat", Message: ""}, sender, room)

	if senderBuf.Len() != 0 {
		t.Errorf("expected no broadcast for empty message without file, got %d bytes", senderBuf.Len())
	}
}

func TestProcessControlChatWithFileAndMessage(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{
		Type:     "chat",
		Message:  "check this out",
		FileID:   7,
		FileName: "screenshot.png",
		FileSize: 99999,
	}, sender, room)

	got := decodeControl(t, senderBuf)
	if got.Message != "check this out" {
		t.Errorf("message: got %q, want %q", got.Message, "check this out")
	}
	if got.FileID != 7 {
		t.Errorf("file_id: got %d, want 7", got.FileID)
	}
}

// --- processControl: rename_user ---

func TestProcessControlRenameUser(t *testing.T) {
	room := NewRoom()
	client, _ := newCtrlClient("alice")
	observer, observerBuf := newCtrlClient("bob")
	room.AddClient(client)
	room.AddClient(observer)

	processControl(ControlMsg{Type: "rename_user", Username: "  Alice2  "}, client, room)

	// Client's Username should be updated.
	if client.Username != "Alice2" {
		t.Errorf("Username: got %q, want %q", client.Username, "Alice2")
	}

	// All clients should receive user_renamed broadcast.
	got := decodeControl(t, observerBuf)
	if got.Type != "user_renamed" {
		t.Errorf("type: got %q, want %q", got.Type, "user_renamed")
	}
	if got.ID != client.ID {
		t.Errorf("ID: got %d, want %d", got.ID, client.ID)
	}
	if got.Username != "Alice2" {
		t.Errorf("Username: got %q, want %q", got.Username, "Alice2")
	}
}

func TestProcessControlRenameUserEmptyRejected(t *testing.T) {
	room := NewRoom()
	client, _ := newCtrlClient("alice")
	observer, observerBuf := newCtrlClient("bob")
	room.AddClient(client)
	room.AddClient(observer)

	processControl(ControlMsg{Type: "rename_user", Username: "   "}, client, room)

	if client.Username != "alice" {
		t.Errorf("Username should remain 'alice', got %q", client.Username)
	}
	if observerBuf.Len() != 0 {
		t.Error("empty rename should not broadcast")
	}
}

func TestProcessControlRenameUserTooLong(t *testing.T) {
	room := NewRoom()
	client, _ := newCtrlClient("alice")
	observer, observerBuf := newCtrlClient("bob")
	room.AddClient(client)
	room.AddClient(observer)

	processControl(ControlMsg{Type: "rename_user", Username: strings.Repeat("x", 51)}, client, room)

	if client.Username != "alice" {
		t.Errorf("Username should remain 'alice', got %q", client.Username)
	}
	if observerBuf.Len() != 0 {
		t.Error("too-long rename should not broadcast")
	}
}

func TestProcessControlRenameUserUpdatesChat(t *testing.T) {
	room := NewRoom()
	client, clientBuf := newCtrlClient("alice")
	room.AddClient(client)

	// Rename.
	processControl(ControlMsg{Type: "rename_user", Username: "NewName"}, client, room)
	// Drain the user_renamed broadcast.
	_ = decodeControl(t, clientBuf)

	// Send a chat message — it should use the new name.
	processControl(ControlMsg{Type: "chat", Message: "hello"}, client, room)
	got := decodeControl(t, clientBuf)
	if got.Username != "NewName" {
		t.Errorf("chat username after rename: got %q, want %q", got.Username, "NewName")
	}
}

// --- processControl: delete_channel last channel protection ---

func TestProcessControlDeleteLastChannelRejected(t *testing.T) {
	room := NewRoom()
	var called bool
	room.SetOnDeleteChannel(func(_ int64) error { called = true; return nil })
	room.SetOnRefreshChannels(func() ([]ChannelInfo, error) { return nil, nil })

	// Set a single channel in the cache.
	room.SetChannels([]ChannelInfo{{ID: 1, Name: "General"}})

	owner, _ := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "delete_channel", ChannelID: 1}, owner, room)

	if called {
		t.Error("deleting the last remaining channel should be rejected")
	}
}

func TestProcessControlDeleteChannelAllowedWithMultiple(t *testing.T) {
	room := NewRoom()
	var deletedID int64
	room.SetOnDeleteChannel(func(id int64) error { deletedID = id; return nil })
	room.SetOnRefreshChannels(func() ([]ChannelInfo, error) {
		return []ChannelInfo{{ID: 2, Name: "Music"}}, nil
	})

	// Set two channels in the cache.
	room.SetChannels([]ChannelInfo{{ID: 1, Name: "General"}, {ID: 2, Name: "Music"}})

	owner, _ := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "delete_channel", ChannelID: 1}, owner, room)

	if deletedID != 1 {
		t.Errorf("onDeleteChannel: got %d, want 1", deletedID)
	}
}

// --- processControl: edit_message ---

func TestProcessControlEditMessageBySender(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(sender)
	room.AddClient(receiver)

	// Send a chat message to establish ownership.
	processControl(ControlMsg{Type: "chat", Message: "original"}, sender, room)
	chatMsg := decodeControl(t, senderBuf)
	_ = decodeControl(t, receiverBuf) // drain receiver's copy

	// Edit the message.
	processControl(ControlMsg{Type: "edit_message", MsgID: chatMsg.MsgID, Message: "edited"}, sender, room)

	got := decodeControl(t, receiverBuf)
	if got.Type != "message_edited" {
		t.Errorf("type: got %q, want %q", got.Type, "message_edited")
	}
	if got.MsgID != chatMsg.MsgID {
		t.Errorf("msg_id: got %d, want %d", got.MsgID, chatMsg.MsgID)
	}
	if got.Message != "edited" {
		t.Errorf("message: got %q, want %q", got.Message, "edited")
	}
	if got.Timestamp == 0 {
		t.Error("timestamp should be set by server")
	}
}

func TestProcessControlEditMessageByNonSender(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	attacker, attackerBuf := newCtrlClient("eve")
	room.AddClient(sender)
	room.AddClient(attacker)

	// Send a message as alice.
	processControl(ControlMsg{Type: "chat", Message: "original"}, sender, room)
	chatMsg := decodeControl(t, senderBuf)
	_ = decodeControl(t, attackerBuf) // drain

	// Eve tries to edit alice's message — should be rejected.
	processControl(ControlMsg{Type: "edit_message", MsgID: chatMsg.MsgID, Message: "hacked"}, attacker, room)

	if attackerBuf.Len() != 0 {
		t.Error("non-sender should not be able to edit messages")
	}
}

func TestProcessControlEditMessageOwnerCannotEditOthers(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	owner, ownerBuf := newCtrlClient("admin")
	room.AddClient(sender)
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	// alice sends a message.
	processControl(ControlMsg{Type: "chat", Message: "original"}, sender, room)
	chatMsg := decodeControl(t, senderBuf)
	_ = decodeControl(t, ownerBuf) // drain

	// Owner tries to edit alice's message — should be rejected (only delete is allowed).
	processControl(ControlMsg{Type: "edit_message", MsgID: chatMsg.MsgID, Message: "owner-edit"}, owner, room)

	if ownerBuf.Len() != 0 {
		t.Error("owner should not be able to edit others' messages")
	}
}

func TestProcessControlEditMessageEmptyRejected(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "original"}, sender, room)
	chatMsg := decodeControl(t, senderBuf)

	processControl(ControlMsg{Type: "edit_message", MsgID: chatMsg.MsgID, Message: ""}, sender, room)

	if senderBuf.Len() != 0 {
		t.Error("empty edit message should be rejected")
	}
}

func TestProcessControlEditMessageTooLongRejected(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "original"}, sender, room)
	chatMsg := decodeControl(t, senderBuf)

	processControl(ControlMsg{Type: "edit_message", MsgID: chatMsg.MsgID, Message: strings.Repeat("x", 501)}, sender, room)

	if senderBuf.Len() != 0 {
		t.Error("501-char edit should be rejected")
	}
}

func TestProcessControlEditMessageUnknownMsgID(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	// Try to edit a message that was never sent.
	processControl(ControlMsg{Type: "edit_message", MsgID: 9999, Message: "edit"}, sender, room)

	if senderBuf.Len() != 0 {
		t.Error("editing unknown msg_id should be silently rejected")
	}
}

func TestProcessControlEditMessageZeroMsgID(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "edit_message", MsgID: 0, Message: "edit"}, sender, room)

	if senderBuf.Len() != 0 {
		t.Error("editing with msg_id=0 should be rejected")
	}
}

// --- processControl: delete_message ---

func TestProcessControlDeleteMessageBySender(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(sender)
	room.AddClient(receiver)

	processControl(ControlMsg{Type: "chat", Message: "to delete"}, sender, room)
	chatMsg := decodeControl(t, senderBuf)
	_ = decodeControl(t, receiverBuf)

	processControl(ControlMsg{Type: "delete_message", MsgID: chatMsg.MsgID}, sender, room)

	got := decodeControl(t, receiverBuf)
	if got.Type != "message_deleted" {
		t.Errorf("type: got %q, want %q", got.Type, "message_deleted")
	}
	if got.MsgID != chatMsg.MsgID {
		t.Errorf("msg_id: got %d, want %d", got.MsgID, chatMsg.MsgID)
	}
}

func TestProcessControlDeleteMessageByOwner(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	owner, ownerBuf := newCtrlClient("admin")
	room.AddClient(sender)
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "chat", Message: "alice says hi"}, sender, room)
	chatMsg := decodeControl(t, senderBuf)
	_ = decodeControl(t, ownerBuf) // drain

	// Owner deletes alice's message — should succeed.
	processControl(ControlMsg{Type: "delete_message", MsgID: chatMsg.MsgID}, owner, room)

	got := decodeControl(t, ownerBuf)
	if got.Type != "message_deleted" {
		t.Errorf("type: got %q, want %q", got.Type, "message_deleted")
	}
}

func TestProcessControlDeleteMessageByNonSenderNonOwner(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	owner, _ := newCtrlClient("admin")
	attacker, attackerBuf := newCtrlClient("eve")
	room.AddClient(sender)
	room.AddClient(owner)
	room.AddClient(attacker)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "chat", Message: "original"}, sender, room)
	chatMsg := decodeControl(t, senderBuf)
	_ = decodeControl(t, attackerBuf) // drain

	// Eve (non-sender, non-owner) tries to delete — should be rejected.
	processControl(ControlMsg{Type: "delete_message", MsgID: chatMsg.MsgID}, attacker, room)

	if attackerBuf.Len() != 0 {
		t.Error("non-sender non-owner should not be able to delete messages")
	}
}

func TestProcessControlDeleteMessageZeroMsgID(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "delete_message", MsgID: 0}, sender, room)

	if senderBuf.Len() != 0 {
		t.Error("deleting with msg_id=0 should be rejected")
	}
}

func TestProcessControlDeleteMessageUnknownMsgID(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "delete_message", MsgID: 9999}, sender, room)

	if senderBuf.Len() != 0 {
		t.Error("deleting unknown msg_id should be silently rejected")
	}
}

// --- Room: message ownership tracking ---

func TestRoomRecordAndGetMsgOwner(t *testing.T) {
	room := NewRoom()
	room.RecordMsgOwner(1, 42)
	room.RecordMsgOwner(2, 43)

	if id, ok := room.GetMsgOwner(1); !ok || id != 42 {
		t.Errorf("GetMsgOwner(1): got (%d, %v), want (42, true)", id, ok)
	}
	if id, ok := room.GetMsgOwner(2); !ok || id != 43 {
		t.Errorf("GetMsgOwner(2): got (%d, %v), want (43, true)", id, ok)
	}
	if _, ok := room.GetMsgOwner(999); ok {
		t.Error("GetMsgOwner(999): expected not found")
	}
}

func TestRoomMsgOwnerEviction(t *testing.T) {
	room := NewRoom()
	// Fill up to maxMsgOwners + 1 to trigger eviction.
	for i := uint64(1); i <= maxMsgOwners+1; i++ {
		room.RecordMsgOwner(i, uint16(i%100))
	}
	// First entry should have been evicted.
	if _, ok := room.GetMsgOwner(1); ok {
		t.Error("msg_id 1 should have been evicted")
	}
	// Last entry should still exist.
	if _, ok := room.GetMsgOwner(maxMsgOwners + 1); !ok {
		t.Error("msg_id maxMsgOwners+1 should still exist")
	}
}

// --- processControl: video_state ---

func TestProcessControlVideoStateStartBroadcasts(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(sender)
	room.AddClient(receiver)

	active := true
	processControl(ControlMsg{Type: "video_state", VideoActive: &active}, sender, room)

	// Both sender and receiver should get the broadcast.
	for _, tc := range []struct {
		name string
		buf  *bytes.Buffer
	}{
		{"sender", senderBuf},
		{"receiver", receiverBuf},
	} {
		got := decodeControl(t, tc.buf)
		if got.Type != "video_state" {
			t.Errorf("%s: type: got %q, want %q", tc.name, got.Type, "video_state")
		}
		if got.ID != sender.ID {
			t.Errorf("%s: id: got %d, want %d (server should stamp sender ID)", tc.name, got.ID, sender.ID)
		}
		if got.VideoActive == nil || !*got.VideoActive {
			t.Errorf("%s: video_active should be true", tc.name)
		}
	}
}

func TestProcessControlVideoStateStopBroadcasts(t *testing.T) {
	room := NewRoom()
	sender, _ := newCtrlClient("alice")
	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(sender)
	room.AddClient(receiver)

	inactive := false
	processControl(ControlMsg{Type: "video_state", VideoActive: &inactive}, sender, room)

	got := decodeControl(t, receiverBuf)
	if got.Type != "video_state" {
		t.Errorf("type: got %q, want %q", got.Type, "video_state")
	}
	if got.VideoActive == nil || *got.VideoActive {
		t.Error("video_active should be false")
	}
}

func TestProcessControlVideoStateScreenShare(t *testing.T) {
	room := NewRoom()
	sender, _ := newCtrlClient("alice")
	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(sender)
	room.AddClient(receiver)

	active := true
	screen := true
	processControl(ControlMsg{Type: "video_state", VideoActive: &active, ScreenShare: &screen}, sender, room)

	got := decodeControl(t, receiverBuf)
	if got.Type != "video_state" {
		t.Errorf("type: got %q, want %q", got.Type, "video_state")
	}
	if got.VideoActive == nil || !*got.VideoActive {
		t.Error("video_active should be true")
	}
	if got.ScreenShare == nil || !*got.ScreenShare {
		t.Error("screen_share should be true")
	}
	if got.ID != sender.ID {
		t.Errorf("id: got %d, want %d (server should stamp sender ID)", got.ID, sender.ID)
	}
}

// --- processControl: join_channel with user limit ---

func TestProcessControlJoinChannelFullRejected(t *testing.T) {
	room := NewRoom()
	room.SetChannels([]ChannelInfo{{ID: 5, Name: "Limited", MaxUsers: 1}})

	alice, _ := newCtrlClient("alice")
	bob, bobBuf := newCtrlClient("bob")
	room.AddClient(alice)
	room.AddClient(bob)

	// Alice joins channel 5 — should succeed.
	processControl(ControlMsg{Type: "join_channel", ChannelID: 5}, alice, room)
	if alice.channelID.Load() != 5 {
		t.Fatalf("alice should be in channel 5, got %d", alice.channelID.Load())
	}

	// Drain bob's user_channel broadcast from alice's join.
	_ = decodeControl(t, bobBuf)

	// Bob tries to join — channel is full (max_users=1).
	processControl(ControlMsg{Type: "join_channel", ChannelID: 5}, bob, room)

	// Bob should NOT be moved.
	if bob.channelID.Load() != 0 {
		t.Errorf("bob should remain in lobby (channel 0), got %d", bob.channelID.Load())
	}

	// Bob should receive an error message.
	got := decodeControl(t, bobBuf)
	if got.Type != "error" {
		t.Errorf("expected error message, got type %q", got.Type)
	}
	if got.Error != "Channel is full" {
		t.Errorf("error message: got %q, want %q", got.Error, "Channel is full")
	}
}

func TestProcessControlJoinChannelLobbyAlwaysAllowed(t *testing.T) {
	room := NewRoom()
	// Even if "channel 0" somehow had a max, joining lobby should always work.
	alice, _ := newCtrlClient("alice")
	room.AddClient(alice)
	alice.channelID.Store(5)

	processControl(ControlMsg{Type: "join_channel", ChannelID: 0}, alice, room)
	if alice.channelID.Load() != 0 {
		t.Errorf("should be able to leave to lobby, got channel %d", alice.channelID.Load())
	}
}

func TestProcessControlSetChannelLimitByOwner(t *testing.T) {
	room := NewRoom()
	room.SetChannels([]ChannelInfo{{ID: 5, Name: "General"}, {ID: 6, Name: "Music"}})
	room.SetOnRefreshChannels(func() ([]ChannelInfo, error) {
		return room.GetChannelList(), nil
	})

	owner, _ := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "set_channel_limit", ChannelID: 5, MaxUsers: 10}, owner, room)

	if max := room.GetChannelMaxUsers(5); max != 10 {
		t.Errorf("channel 5 max_users: got %d, want 10", max)
	}
}

func TestProcessControlSetChannelLimitByNonOwner(t *testing.T) {
	room := NewRoom()
	room.SetChannels([]ChannelInfo{{ID: 5, Name: "General"}})

	owner, _ := newCtrlClient("alice")
	attacker, _ := newCtrlClient("eve")
	room.AddClient(owner)
	room.AddClient(attacker)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "set_channel_limit", ChannelID: 5, MaxUsers: 10}, attacker, room)

	if max := room.GetChannelMaxUsers(5); max != 0 {
		t.Errorf("non-owner should not be able to set channel limit, got max_users=%d", max)
	}
}

func TestRoomCanJoinChannelUnlimited(t *testing.T) {
	room := NewRoom()
	room.SetChannels([]ChannelInfo{{ID: 5, Name: "General"}})
	// No limit set — always allowed.
	if !room.CanJoinChannel(5) {
		t.Error("should be able to join unlimited channel")
	}
}

func TestRoomChannelUserCount(t *testing.T) {
	room := NewRoom()
	a, _ := newCtrlClient("alice")
	b, _ := newCtrlClient("bob")
	room.AddClient(a)
	room.AddClient(b)
	a.channelID.Store(5)
	b.channelID.Store(5)

	if count := room.ChannelUserCount(5); count != 2 {
		t.Errorf("ChannelUserCount(5): got %d, want 2", count)
	}
	if count := room.ChannelUserCount(0); count != 0 {
		t.Errorf("ChannelUserCount(0): got %d, want 0", count)
	}
}

func TestProcessControlVideoStateSpoofedIDReplaced(t *testing.T) {
	room := NewRoom()
	sender, _ := newCtrlClient("alice")
	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(sender)
	room.AddClient(receiver)

	// Attacker tries to set a different ID — server should overwrite with sender's ID.
	active := true
	processControl(ControlMsg{Type: "video_state", ID: 9999, VideoActive: &active}, sender, room)

	got := decodeControl(t, receiverBuf)
	if got.ID != sender.ID {
		t.Errorf("server should stamp authoritative sender ID: got %d, want %d", got.ID, sender.ID)
	}
}

// --- Phase 7: Simulcast / Video Quality tests ---

func TestProcessControlVideoStateIncludesLayers(t *testing.T) {
	room := NewRoom()

	sender, _ := newCtrlClient("alice")
	sender.ID = 1
	room.AddClient(sender)

	receiver, receiverBuf := newCtrlClient("bob")
	receiver.ID = 2
	room.AddClient(receiver)

	active := true
	processControl(ControlMsg{Type: "video_state", VideoActive: &active}, sender, room)

	got := decodeControl(t, receiverBuf)
	if got.Type != "video_state" {
		t.Fatalf("expected video_state, got %q", got.Type)
	}
	if len(got.VideoLayers) != 3 {
		t.Fatalf("expected 3 simulcast layers, got %d", len(got.VideoLayers))
	}

	// Verify layers are high, medium, low.
	qualities := make(map[string]bool)
	for _, l := range got.VideoLayers {
		qualities[l.Quality] = true
		if l.Width <= 0 || l.Height <= 0 || l.Bitrate <= 0 {
			t.Errorf("layer %q has invalid dimensions: %dx%d @ %dkbps", l.Quality, l.Width, l.Height, l.Bitrate)
		}
	}
	for _, q := range []string{"high", "medium", "low"} {
		if !qualities[q] {
			t.Errorf("missing %q layer", q)
		}
	}
}

func TestProcessControlVideoStateStopNoLayers(t *testing.T) {
	room := NewRoom()

	sender, _ := newCtrlClient("alice")
	sender.ID = 1
	room.AddClient(sender)

	receiver, receiverBuf := newCtrlClient("bob")
	receiver.ID = 2
	room.AddClient(receiver)

	active := false
	processControl(ControlMsg{Type: "video_state", VideoActive: &active}, sender, room)

	got := decodeControl(t, receiverBuf)
	if len(got.VideoLayers) != 0 {
		t.Errorf("stop video should not include layers, got %d", len(got.VideoLayers))
	}
}

func TestProcessControlSetVideoQuality(t *testing.T) {
	room := NewRoom()

	receiver, _ := newCtrlClient("alice")
	receiver.ID = 1
	room.AddClient(receiver)

	target, targetBuf := newCtrlClient("bob")
	target.ID = 2
	room.AddClient(target)

	processControl(ControlMsg{Type: "set_video_quality", TargetID: 2, VideoQuality: "medium"}, receiver, room)

	got := decodeControl(t, targetBuf)
	if got.Type != "set_video_quality" {
		t.Fatalf("expected set_video_quality, got %q", got.Type)
	}
	if got.ID != 1 {
		t.Errorf("expected requesting user ID 1, got %d", got.ID)
	}
	if got.VideoQuality != "medium" {
		t.Errorf("expected quality 'medium', got %q", got.VideoQuality)
	}
}

func TestProcessControlSetVideoQualityInvalid(t *testing.T) {
	room := NewRoom()

	client1, _ := newCtrlClient("alice")
	client1.ID = 1
	room.AddClient(client1)

	target, targetBuf := newCtrlClient("bob")
	target.ID = 2
	room.AddClient(target)

	// Invalid quality should be silently dropped.
	processControl(ControlMsg{Type: "set_video_quality", TargetID: 2, VideoQuality: "ultra"}, client1, room)

	if targetBuf.Len() > 0 {
		t.Error("invalid quality should not forward a message to the target")
	}
}

func TestProcessControlSetVideoQualityNoTarget(t *testing.T) {
	room := NewRoom()

	client1, _ := newCtrlClient("alice")
	client1.ID = 1
	room.AddClient(client1)

	// Missing target ID should be silently dropped.
	processControl(ControlMsg{Type: "set_video_quality", TargetID: 0, VideoQuality: "high"}, client1, room)
}

func TestDefaultVideoLayers(t *testing.T) {
	layers := DefaultVideoLayers()
	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d", len(layers))
	}

	// Verify descending resolution order.
	for i := 1; i < len(layers); i++ {
		if layers[i].Width >= layers[i-1].Width {
			t.Errorf("layer %d width (%d) should be < layer %d width (%d)",
				i, layers[i].Width, i-1, layers[i-1].Width)
		}
		if layers[i].Bitrate >= layers[i-1].Bitrate {
			t.Errorf("layer %d bitrate (%d) should be < layer %d bitrate (%d)",
				i, layers[i].Bitrate, i-1, layers[i-1].Bitrate)
		}
	}
}

func TestSendControlToSpecificClient(t *testing.T) {
	room := NewRoom()

	client1, _ := newCtrlClient("alice")
	client1.ID = 1
	room.AddClient(client1)

	client2, buf2 := newCtrlClient("bob")
	client2.ID = 2
	room.AddClient(client2)

	client3, buf3 := newCtrlClient("charlie")
	client3.ID = 3
	room.AddClient(client3)

	// Send to client2 only.
	room.SendControlTo(2, ControlMsg{Type: "test", Message: "hello"})

	if buf2.Len() == 0 {
		t.Error("client2 should have received the message")
	}
	if buf3.Len() > 0 {
		t.Error("client3 should NOT have received the message")
	}

	got := decodeControl(t, buf2)
	if got.Message != "hello" {
		t.Errorf("expected message 'hello', got %q", got.Message)
	}
}

func TestSendControlToNonExistent(t *testing.T) {
	room := NewRoom()
	// Should not panic.
	room.SendControlTo(999, ControlMsg{Type: "test"})
}
