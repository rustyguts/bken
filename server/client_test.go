package main

import (
	"bytes"
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
