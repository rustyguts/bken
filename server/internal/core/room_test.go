package core

import (
	"testing"
	"time"

	"bken/server/internal/protocol"
)

func TestRoomMultiServerSingleVoiceLifecycle(t *testing.T) {
	r := NewRoom()
	s, _, err := r.Add("alice", 8)
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	u, changed, err := r.ConnectServer(s.UserID, "srv-1")
	if err != nil || !changed {
		t.Fatalf("connect srv-1 failed: changed=%v err=%v", changed, err)
	}
	if len(u.ConnectedServers) != 1 || u.ConnectedServers[0] != "srv-1" {
		t.Fatalf("unexpected servers after first connect: %#v", u.ConnectedServers)
	}

	u, changed, err = r.ConnectServer(s.UserID, "srv-2")
	if err != nil || !changed {
		t.Fatalf("connect srv-2 failed: changed=%v err=%v", changed, err)
	}
	if len(u.ConnectedServers) != 2 {
		t.Fatalf("expected 2 connected servers, got %d", len(u.ConnectedServers))
	}

	u, prevVoice, err := r.JoinVoice(s.UserID, "srv-1", "chan-a")
	if err != nil {
		t.Fatalf("join voice srv-1/chan-a: %v", err)
	}
	if prevVoice != nil {
		t.Fatalf("unexpected previous voice: %#v", prevVoice)
	}
	if u.Voice == nil || u.Voice.ServerID != "srv-1" || u.Voice.ChannelID != "chan-a" {
		t.Fatalf("unexpected active voice: %#v", u.Voice)
	}

	if !r.CanSendText(s.UserID, "srv-2") {
		t.Fatal("expected text send on srv-2 while voice is on srv-1")
	}

	u, prevVoice, err = r.JoinVoice(s.UserID, "srv-2", "chan-b")
	if err != nil {
		t.Fatalf("join voice srv-2/chan-b: %v", err)
	}
	if prevVoice == nil || prevVoice.ServerID != "srv-1" || prevVoice.ChannelID != "chan-a" {
		t.Fatalf("expected previous voice on srv-1/chan-a, got %#v", prevVoice)
	}
	if u.Voice == nil || u.Voice.ServerID != "srv-2" || u.Voice.ChannelID != "chan-b" {
		t.Fatalf("expected active voice srv-2/chan-b, got %#v", u.Voice)
	}

	u, oldVoice, changed := r.DisconnectVoice(s.UserID)
	if !changed {
		t.Fatal("expected DisconnectVoice to change state")
	}
	if oldVoice == nil || oldVoice.ServerID != "srv-2" || oldVoice.ChannelID != "chan-b" {
		t.Fatalf("expected old voice srv-2/chan-b, got %#v", oldVoice)
	}
	if u.Voice != nil {
		t.Fatalf("expected voice to be cleared, got %#v", u.Voice)
	}
}

func TestRoomDisconnectServerOnlyClearsMatchingVoice(t *testing.T) {
	r := NewRoom()
	s, _, err := r.Add("alice", 8)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, _, err := r.ConnectServer(s.UserID, "srv-1"); err != nil {
		t.Fatalf("connect srv-1: %v", err)
	}
	if _, _, err := r.ConnectServer(s.UserID, "srv-2"); err != nil {
		t.Fatalf("connect srv-2: %v", err)
	}
	if _, _, err := r.JoinVoice(s.UserID, "srv-1", "chan-a"); err != nil {
		t.Fatalf("join voice: %v", err)
	}

	u, changed, oldVoice, err := r.DisconnectServer(s.UserID, "srv-2")
	if err != nil || !changed {
		t.Fatalf("disconnect srv-2 failed: changed=%v err=%v", changed, err)
	}
	if oldVoice != nil {
		t.Fatalf("voice should not clear when disconnecting another server: %#v", oldVoice)
	}
	if u.Voice == nil || u.Voice.ServerID != "srv-1" {
		t.Fatalf("voice should still be in srv-1, got %#v", u.Voice)
	}

	u, changed, oldVoice, err = r.DisconnectServer(s.UserID, "srv-1")
	if err != nil || !changed {
		t.Fatalf("disconnect srv-1 failed: changed=%v err=%v", changed, err)
	}
	if oldVoice == nil || oldVoice.ServerID != "srv-1" || oldVoice.ChannelID != "chan-a" {
		t.Fatalf("expected old voice srv-1/chan-a, got %#v", oldVoice)
	}
	if u.Voice != nil {
		t.Fatalf("expected voice cleared after disconnecting srv-1, got %#v", u.Voice)
	}
}

func TestRoomBroadcastToServerScopesRecipients(t *testing.T) {
	r := NewRoom()
	alice, _, err := r.Add("alice", 8)
	if err != nil {
		t.Fatalf("add alice: %v", err)
	}
	bob, _, err := r.Add("bob", 8)
	if err != nil {
		t.Fatalf("add bob: %v", err)
	}
	charlie, _, err := r.Add("charlie", 8)
	if err != nil {
		t.Fatalf("add charlie: %v", err)
	}

	if _, _, err := r.ConnectServer(alice.UserID, "srv-1"); err != nil {
		t.Fatalf("alice connect: %v", err)
	}
	if _, _, err := r.ConnectServer(bob.UserID, "srv-1"); err != nil {
		t.Fatalf("bob connect srv-1: %v", err)
	}
	if _, _, err := r.ConnectServer(bob.UserID, "srv-2"); err != nil {
		t.Fatalf("bob connect srv-2: %v", err)
	}
	if _, _, err := r.ConnectServer(charlie.UserID, "srv-2"); err != nil {
		t.Fatalf("charlie connect: %v", err)
	}

	r.BroadcastToServer("srv-1", protocol.Message{Type: "test"}, "")

	assertRecvType(t, alice.Send, "test")
	assertRecvType(t, bob.Send, "test")
	assertNoRecv(t, charlie.Send)
}

func TestRoomRemoveClosesChannel(t *testing.T) {
	r := NewRoom()
	s, _, err := r.Add("alice", 8)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, ok := r.Remove(s.UserID); !ok {
		t.Fatal("expected remove to succeed")
	}
	_, ok := <-s.Send
	if ok {
		t.Fatal("expected send channel to be closed")
	}
}

func assertRecvType(t *testing.T, ch <-chan protocol.Message, typ string) {
	t.Helper()
	select {
	case msg := <-ch:
		if msg.Type != typ {
			t.Fatalf("expected message type %q, got %q", typ, msg.Type)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for message %q", typ)
	}
}

func assertNoRecv(t *testing.T, ch <-chan protocol.Message) {
	t.Helper()
	select {
	case msg := <-ch:
		t.Fatalf("expected no message, got %#v", msg)
	case <-time.After(100 * time.Millisecond):
	}
}
