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
	room.SetOnRename(func(name string) { persisted = name })

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
	room.SetOnRename(func(_ string) { renameCalled = true })

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
	if client.channelID != 7 {
		t.Errorf("channelID: got %d, want 7", client.channelID)
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
	client.channelID = 5

	// Sending channel_id=0 means "leave all channels".
	processControl(ControlMsg{Type: "join_channel", ChannelID: 0}, client, room)

	if client.channelID != 0 {
		t.Errorf("channelID after leave: got %d, want 0", client.channelID)
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
