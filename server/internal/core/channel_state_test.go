package core

import (
	"testing"
	"time"

	"bken/server/internal/protocol"
)

func TestChannelStateMultiServerSingleVoiceLifecycle(t *testing.T) {
	r := NewChannelState("")
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

func TestChannelStateDisconnectServerOnlyClearsMatchingVoice(t *testing.T) {
	r := NewChannelState("")
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

func TestChannelStateBroadcastToServerScopesRecipients(t *testing.T) {
	r := NewChannelState("")
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

func TestChannelStateRemoveClosesChannel(t *testing.T) {
	r := NewChannelState("")
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

func TestCreateChannelLifecycle(t *testing.T) {
	r := NewChannelState("")
	s, _, err := r.Add("alice", 8)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, _, err := r.ConnectServer(s.UserID, "srv-1"); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// ConnectServer seeds a default "General" channel.
	chs := r.Channels("srv-1")
	if len(chs) != 1 || chs[0].Name != "General" {
		t.Fatalf("expected seeded General channel, got %#v", chs)
	}

	// Create an additional channel.
	chs, err = r.CreateChannel("srv-1", "voice")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(chs) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(chs))
	}

	// Rename first channel.
	chs, err = r.RenameChannel("srv-1", chs[0].ID, "lobby")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if chs[0].Name != "lobby" {
		t.Fatalf("expected renamed to lobby, got %s", chs[0].Name)
	}

	// Delete first channel.
	secondID := chs[1].ID
	chs, err = r.DeleteChannel("srv-1", chs[0].ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(chs) != 1 || chs[0].ID != secondID {
		t.Fatalf("unexpected channels after delete: %#v", chs)
	}
}

func TestCreateChannelValidation(t *testing.T) {
	r := NewChannelState("")
	if _, err := r.CreateChannel("srv-1", ""); err == nil {
		t.Fatal("expected error for empty name")
	}
	if _, err := r.CreateChannel("", "general"); err == nil {
		t.Fatal("expected error for empty server_id")
	}
}

func TestUserServerSingleConnection(t *testing.T) {
	r := NewChannelState("")
	s, _, err := r.Add("alice", 8)
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	// No connections → error.
	if _, err := r.UserServer(s.UserID); err == nil {
		t.Fatal("expected error when not connected")
	}

	if _, _, err := r.ConnectServer(s.UserID, "srv-1"); err != nil {
		t.Fatalf("connect: %v", err)
	}

	sid, err := r.UserServer(s.UserID)
	if err != nil || sid != "srv-1" {
		t.Fatalf("expected srv-1, got %q err=%v", sid, err)
	}

	// Multiple connections → error.
	if _, _, err := r.ConnectServer(s.UserID, "srv-2"); err != nil {
		t.Fatalf("connect srv-2: %v", err)
	}
	if _, err := r.UserServer(s.UserID); err == nil {
		t.Fatal("expected error for multiple connections")
	}
}

func TestChannelsPerServerIsolation(t *testing.T) {
	r := NewChannelState("")
	if _, err := r.CreateChannel("srv-1", "general"); err != nil {
		t.Fatalf("create on srv-1: %v", err)
	}
	if _, err := r.CreateChannel("srv-2", "lobby"); err != nil {
		t.Fatalf("create on srv-2: %v", err)
	}

	chs1 := r.Channels("srv-1")
	chs2 := r.Channels("srv-2")
	if len(chs1) != 1 || chs1[0].Name != "general" {
		t.Fatalf("srv-1 channels: %#v", chs1)
	}
	if len(chs2) != 1 || chs2[0].Name != "lobby" {
		t.Fatalf("srv-2 channels: %#v", chs2)
	}
}

func TestSetVoiceFlags_NotInVoice(t *testing.T) {
	r := NewChannelState("")
	s, _, err := r.Add("alice", 8)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	// Not in voice — should return changed=false.
	_, changed := r.SetVoiceFlags(s.UserID, true, false)
	if changed {
		t.Fatal("expected changed=false when user is not in voice")
	}
}

func TestSetVoiceFlags_InVoice(t *testing.T) {
	r := NewChannelState("")
	s, _, err := r.Add("alice", 8)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, _, err := r.ConnectServer(s.UserID, "srv-1"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, _, err := r.JoinVoice(s.UserID, "srv-1", "chan-a"); err != nil {
		t.Fatalf("join voice: %v", err)
	}

	// Set muted=true, deafened=false.
	user, changed := r.SetVoiceFlags(s.UserID, true, false)
	if !changed {
		t.Fatal("expected changed=true")
	}
	if user.Voice == nil {
		t.Fatal("expected non-nil voice")
	}
	if !user.Voice.Muted {
		t.Fatal("expected muted=true in returned user")
	}
	if user.Voice.Deafened {
		t.Fatal("expected deafened=false in returned user")
	}

	// Setting same values should return changed=false.
	_, changed = r.SetVoiceFlags(s.UserID, true, false)
	if changed {
		t.Fatal("expected changed=false when flags unchanged")
	}

	// Set deafened=true.
	user, changed = r.SetVoiceFlags(s.UserID, true, true)
	if !changed {
		t.Fatal("expected changed=true for deafened update")
	}
	if !user.Voice.Deafened {
		t.Fatal("expected deafened=true")
	}
}

func TestDisconnectVoice_ResetsFlags(t *testing.T) {
	r := NewChannelState("")
	s, _, err := r.Add("alice", 8)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, _, err := r.ConnectServer(s.UserID, "srv-1"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, _, err := r.JoinVoice(s.UserID, "srv-1", "chan-a"); err != nil {
		t.Fatalf("join voice: %v", err)
	}
	r.SetVoiceFlags(s.UserID, true, true)

	// Disconnect voice — flags should reset.
	r.DisconnectVoice(s.UserID)

	// Re-join voice and check flags are clean.
	user, _, err := r.JoinVoice(s.UserID, "srv-1", "chan-a")
	if err != nil {
		t.Fatalf("rejoin voice: %v", err)
	}
	if user.Voice == nil {
		t.Fatal("expected voice after rejoin")
	}
	if user.Voice.Muted || user.Voice.Deafened {
		t.Fatalf("expected flags reset after disconnect, got muted=%v deafened=%v", user.Voice.Muted, user.Voice.Deafened)
	}
}

func TestConnectServerSeedsDefaultChannel(t *testing.T) {
	r := NewChannelState("")
	s, _, err := r.Add("alice", 8)
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	// Before connecting, no channels exist.
	if chs := r.Channels("srv-1"); len(chs) != 0 {
		t.Fatalf("expected no channels before connect, got %d", len(chs))
	}

	// First connection to a server seeds a default "General" channel.
	if _, _, err := r.ConnectServer(s.UserID, "srv-1"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	chs := r.Channels("srv-1")
	if len(chs) != 1 || chs[0].Name != "General" {
		t.Fatalf("expected one General channel, got %#v", chs)
	}

	// A second user connecting to the same server does not duplicate.
	s2, _, err := r.Add("bob", 8)
	if err != nil {
		t.Fatalf("add bob: %v", err)
	}
	if _, _, err := r.ConnectServer(s2.UserID, "srv-1"); err != nil {
		t.Fatalf("bob connect: %v", err)
	}
	chs = r.Channels("srv-1")
	if len(chs) != 1 {
		t.Fatalf("expected still one channel after second connect, got %d", len(chs))
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
