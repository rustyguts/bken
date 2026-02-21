package main

import (
	"bytes"
	"testing"
)

// --- 6.01: @Mention Autocomplete ---

func TestParseMentionsSingleUser(t *testing.T) {
	room := NewRoom()
	alice, _ := newCtrlClient("alice")
	bob, _ := newCtrlClient("bob")
	room.AddClient(alice)
	room.AddClient(bob)

	mentions := parseMentions("hey @alice check this", room)
	if len(mentions) != 1 || mentions[0] != alice.ID {
		t.Errorf("expected [%d], got %v", alice.ID, mentions)
	}
}

func TestParseMentionsMultipleUsers(t *testing.T) {
	room := NewRoom()
	alice, _ := newCtrlClient("alice")
	bob, _ := newCtrlClient("bob")
	room.AddClient(alice)
	room.AddClient(bob)

	mentions := parseMentions("@alice and @bob hello", room)
	if len(mentions) != 2 {
		t.Errorf("expected 2 mentions, got %d: %v", len(mentions), mentions)
	}
}

func TestParseMentionsNoAt(t *testing.T) {
	room := NewRoom()
	alice, _ := newCtrlClient("alice")
	room.AddClient(alice)

	mentions := parseMentions("hello world", room)
	if len(mentions) != 0 {
		t.Errorf("expected no mentions, got %v", mentions)
	}
}

func TestParseMentionsUnknownUser(t *testing.T) {
	room := NewRoom()
	alice, _ := newCtrlClient("alice")
	room.AddClient(alice)

	mentions := parseMentions("@nobody says hi", room)
	if len(mentions) != 0 {
		t.Errorf("expected no mentions for unknown user, got %v", mentions)
	}
}

func TestParseMentionsDedup(t *testing.T) {
	room := NewRoom()
	alice, _ := newCtrlClient("alice")
	room.AddClient(alice)

	mentions := parseMentions("@alice says @alice again", room)
	if len(mentions) != 1 {
		t.Errorf("expected 1 deduplicated mention, got %d: %v", len(mentions), mentions)
	}
}

func TestChatBroadcastIncludesMentions(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	target, _ := newCtrlClient("bob")
	room.AddClient(sender)
	room.AddClient(target)

	processControl(ControlMsg{Type: "chat", Message: "hey @bob check this"}, sender, room)

	got := decodeControl(t, senderBuf)
	if got.Type != "chat" {
		t.Fatalf("type: got %q, want %q", got.Type, "chat")
	}
	if len(got.Mentions) != 1 || got.Mentions[0] != target.ID {
		t.Errorf("mentions: got %v, want [%d]", got.Mentions, target.ID)
	}
}

func TestChatBroadcastNoMentionsWhenNone(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "hello world"}, sender, room)

	got := decodeControl(t, senderBuf)
	if len(got.Mentions) != 0 {
		t.Errorf("expected no mentions, got %v", got.Mentions)
	}
}

// --- 6.02: Message Reactions ---

func TestAddReaction(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(sender)
	room.AddClient(receiver)

	// Send a message first
	processControl(ControlMsg{Type: "chat", Message: "funny"}, sender, room)
	chatMsg := decodeControl(t, senderBuf)
	_ = decodeControl(t, receiverBuf)

	// Add reaction
	processControl(ControlMsg{Type: "add_reaction", MsgID: chatMsg.MsgID, Emoji: "👍"}, receiver, room)

	got := decodeControl(t, senderBuf)
	if got.Type != "reaction_added" {
		t.Errorf("type: got %q, want %q", got.Type, "reaction_added")
	}
	if got.MsgID != chatMsg.MsgID {
		t.Errorf("msg_id: got %d, want %d", got.MsgID, chatMsg.MsgID)
	}
	if got.Emoji != "👍" {
		t.Errorf("emoji: got %q, want %q", got.Emoji, "👍")
	}
	if got.ID != receiver.ID {
		t.Errorf("user_id: got %d, want %d", got.ID, receiver.ID)
	}
}

func TestAddReactionDuplicatePrevented(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "test"}, sender, room)
	chatMsg := decodeControl(t, senderBuf)

	// First reaction should succeed
	processControl(ControlMsg{Type: "add_reaction", MsgID: chatMsg.MsgID, Emoji: "👍"}, sender, room)
	got := decodeControl(t, senderBuf)
	if got.Type != "reaction_added" {
		t.Fatalf("first reaction: type: got %q, want %q", got.Type, "reaction_added")
	}

	// Duplicate should be silently ignored
	processControl(ControlMsg{Type: "add_reaction", MsgID: chatMsg.MsgID, Emoji: "👍"}, sender, room)
	if senderBuf.Len() != 0 {
		t.Error("duplicate reaction should not broadcast")
	}
}

func TestRemoveReaction(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "test"}, sender, room)
	chatMsg := decodeControl(t, senderBuf)

	processControl(ControlMsg{Type: "add_reaction", MsgID: chatMsg.MsgID, Emoji: "👍"}, sender, room)
	_ = decodeControl(t, senderBuf) // drain reaction_added

	processControl(ControlMsg{Type: "remove_reaction", MsgID: chatMsg.MsgID, Emoji: "👍"}, sender, room)
	got := decodeControl(t, senderBuf)
	if got.Type != "reaction_removed" {
		t.Errorf("type: got %q, want %q", got.Type, "reaction_removed")
	}
}

func TestRemoveReactionNonExistent(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "test"}, sender, room)
	_ = decodeControl(t, senderBuf)

	processControl(ControlMsg{Type: "remove_reaction", MsgID: 1, Emoji: "👍"}, sender, room)
	if senderBuf.Len() != 0 {
		t.Error("removing non-existent reaction should not broadcast")
	}
}

func TestAddReactionEmptyEmoji(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "test"}, sender, room)
	chatMsg := decodeControl(t, senderBuf)

	processControl(ControlMsg{Type: "add_reaction", MsgID: chatMsg.MsgID, Emoji: ""}, sender, room)
	if senderBuf.Len() != 0 {
		t.Error("empty emoji should be rejected")
	}
}

func TestAddReactionZeroMsgID(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "add_reaction", MsgID: 0, Emoji: "👍"}, sender, room)
	if senderBuf.Len() != 0 {
		t.Error("zero msg_id should be rejected")
	}
}

func TestGetReactions(t *testing.T) {
	room := NewRoom()
	alice, aliceBuf := newCtrlClient("alice")
	bob, _ := newCtrlClient("bob")
	room.AddClient(alice)
	room.AddClient(bob)

	processControl(ControlMsg{Type: "chat", Message: "test"}, alice, room)
	chatMsg := decodeControl(t, aliceBuf)

	room.AddReaction(chatMsg.MsgID, alice.ID, "👍")
	room.AddReaction(chatMsg.MsgID, bob.ID, "👍")
	room.AddReaction(chatMsg.MsgID, alice.ID, "😂")

	processControl(ControlMsg{Type: "get_reactions", MsgID: chatMsg.MsgID}, alice, room)
	got := decodeControl(t, aliceBuf)
	if got.Type != "reactions_list" {
		t.Fatalf("type: got %q, want %q", got.Type, "reactions_list")
	}
	if len(got.Reactions) != 2 {
		t.Fatalf("expected 2 reaction groups, got %d", len(got.Reactions))
	}
	if got.Reactions[0].Emoji != "👍" || got.Reactions[0].Count != 2 {
		t.Errorf("first reaction: got %+v", got.Reactions[0])
	}
	if got.Reactions[1].Emoji != "😂" || got.Reactions[1].Count != 1 {
		t.Errorf("second reaction: got %+v", got.Reactions[1])
	}
}

// --- 6.03: Typing Indicators ---

func TestTypingIndicatorBroadcast(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(sender)
	room.AddClient(receiver)

	processControl(ControlMsg{Type: "typing", ChannelID: 1}, sender, room)

	// Sender should NOT receive the typing event (excluded).
	if senderBuf.Len() != 0 {
		t.Error("sender should not receive their own typing event")
	}

	// Receiver should get it.
	got := decodeControl(t, receiverBuf)
	if got.Type != "user_typing" {
		t.Errorf("type: got %q, want %q", got.Type, "user_typing")
	}
	if got.ID != sender.ID {
		t.Errorf("id: got %d, want %d", got.ID, sender.ID)
	}
	if got.ChannelID != 1 {
		t.Errorf("channel_id: got %d, want 1", got.ChannelID)
	}
	if got.Username != "alice" {
		t.Errorf("username: got %q, want %q", got.Username, "alice")
	}
}

func TestTypingIndicatorZeroChannelRejected(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(sender)
	room.AddClient(receiver)

	processControl(ControlMsg{Type: "typing", ChannelID: 0}, sender, room)

	if senderBuf.Len() != 0 || receiverBuf.Len() != 0 {
		t.Error("typing with channel_id=0 should be rejected")
	}
}

// --- 6.04: Reply Threads ---

func TestReplyToMessage(t *testing.T) {
	room := NewRoom()
	alice, aliceBuf := newCtrlClient("alice")
	bob, bobBuf := newCtrlClient("bob")
	room.AddClient(alice)
	room.AddClient(bob)

	// Alice sends a message
	processControl(ControlMsg{Type: "chat", Message: "hello world", ChannelID: 1}, alice, room)
	originalMsg := decodeControl(t, aliceBuf)
	_ = decodeControl(t, bobBuf)

	// Bob replies to alice's message
	processControl(ControlMsg{Type: "chat", Message: "hey alice!", ChannelID: 1, ReplyTo: originalMsg.MsgID}, bob, room)
	reply := decodeControl(t, bobBuf)

	if reply.ReplyTo != originalMsg.MsgID {
		t.Errorf("reply_to: got %d, want %d", reply.ReplyTo, originalMsg.MsgID)
	}
	if reply.ReplyPreview == nil {
		t.Fatal("reply_preview should not be nil")
	}
	if reply.ReplyPreview.Username != "alice" {
		t.Errorf("reply preview username: got %q, want %q", reply.ReplyPreview.Username, "alice")
	}
	if reply.ReplyPreview.Message != "hello world" {
		t.Errorf("reply preview message: got %q, want %q", reply.ReplyPreview.Message, "hello world")
	}
}

func TestReplyToDeletedMessage(t *testing.T) {
	room := NewRoom()
	alice, aliceBuf := newCtrlClient("alice")
	bob, bobBuf := newCtrlClient("bob")
	room.AddClient(alice)
	room.AddClient(bob)

	// Alice sends and then deletes a message
	processControl(ControlMsg{Type: "chat", Message: "secret"}, alice, room)
	originalMsg := decodeControl(t, aliceBuf)
	_ = decodeControl(t, bobBuf) // drain

	processControl(ControlMsg{Type: "delete_message", MsgID: originalMsg.MsgID}, alice, room)
	_ = decodeControl(t, aliceBuf) // drain message_deleted
	_ = decodeControl(t, bobBuf)   // drain message_deleted

	// Bob replies to deleted message
	processControl(ControlMsg{Type: "chat", Message: "replying", ReplyTo: originalMsg.MsgID}, bob, room)
	reply := decodeControl(t, bobBuf)

	if reply.ReplyPreview == nil {
		t.Fatal("reply_preview should not be nil even for deleted messages")
	}
	if !reply.ReplyPreview.Deleted {
		t.Error("reply preview should indicate the original was deleted")
	}
}

func TestReplyToUnknownMessage(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "replying to nothing", ReplyTo: 9999}, sender, room)
	got := decodeControl(t, senderBuf)

	if got.ReplyTo != 9999 {
		t.Errorf("reply_to should be preserved: got %d, want 9999", got.ReplyTo)
	}
	if got.ReplyPreview != nil {
		t.Error("reply_preview should be nil for unknown message")
	}
}

// --- 6.05: Message Search ---

func TestSearchMessages(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	// Send some messages
	processControl(ControlMsg{Type: "chat", Message: "hello world", ChannelID: 1}, sender, room)
	_ = decodeControl(t, senderBuf)
	processControl(ControlMsg{Type: "chat", Message: "goodbye world", ChannelID: 1}, sender, room)
	_ = decodeControl(t, senderBuf)
	processControl(ControlMsg{Type: "chat", Message: "hello again", ChannelID: 1}, sender, room)
	_ = decodeControl(t, senderBuf)

	// Search for "hello"
	processControl(ControlMsg{Type: "search_messages", ChannelID: 1, Query: "hello"}, sender, room)
	got := decodeControl(t, senderBuf)

	if got.Type != "search_results" {
		t.Fatalf("type: got %q, want %q", got.Type, "search_results")
	}
	if len(got.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got.Results))
	}
	// Results should be newest first
	if got.Results[0].Message != "hello again" {
		t.Errorf("first result: got %q, want %q", got.Results[0].Message, "hello again")
	}
	if got.Results[1].Message != "hello world" {
		t.Errorf("second result: got %q, want %q", got.Results[1].Message, "hello world")
	}
}

func TestSearchMessagesCaseInsensitive(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "Hello World", ChannelID: 1}, sender, room)
	_ = decodeControl(t, senderBuf)

	processControl(ControlMsg{Type: "search_messages", ChannelID: 1, Query: "hello"}, sender, room)
	got := decodeControl(t, senderBuf)
	if len(got.Results) != 1 {
		t.Errorf("expected 1 result for case-insensitive search, got %d", len(got.Results))
	}
}

func TestSearchMessagesEmptyQuery(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "search_messages", ChannelID: 1, Query: ""}, sender, room)
	if senderBuf.Len() != 0 {
		t.Error("empty query should be rejected")
	}
}

func TestSearchMessagesZeroChannel(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "search_messages", ChannelID: 0, Query: "test"}, sender, room)
	if senderBuf.Len() != 0 {
		t.Error("zero channel_id should be rejected")
	}
}

func TestSearchMessagesChannelIsolation(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "in channel 1", ChannelID: 1}, sender, room)
	_ = decodeControl(t, senderBuf)
	processControl(ControlMsg{Type: "chat", Message: "in channel 2", ChannelID: 2}, sender, room)
	_ = decodeControl(t, senderBuf)

	processControl(ControlMsg{Type: "search_messages", ChannelID: 1, Query: "channel"}, sender, room)
	got := decodeControl(t, senderBuf)
	if len(got.Results) != 1 {
		t.Errorf("expected 1 result (channel isolated), got %d", len(got.Results))
	}
}

func TestSearchMessagesDeletedExcluded(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "to delete", ChannelID: 1}, sender, room)
	chatMsg := decodeControl(t, senderBuf)

	processControl(ControlMsg{Type: "delete_message", MsgID: chatMsg.MsgID}, sender, room)
	_ = decodeControl(t, senderBuf)

	processControl(ControlMsg{Type: "search_messages", ChannelID: 1, Query: "delete"}, sender, room)
	got := decodeControl(t, senderBuf)
	if len(got.Results) != 0 {
		t.Errorf("deleted messages should not appear in search, got %d results", len(got.Results))
	}
}

func TestSearchMessagesNoResults(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "hello", ChannelID: 1}, sender, room)
	_ = decodeControl(t, senderBuf)

	processControl(ControlMsg{Type: "search_messages", ChannelID: 1, Query: "xyznotfound"}, sender, room)
	got := decodeControl(t, senderBuf)
	if got.Type != "search_results" {
		t.Fatalf("type: got %q, want %q", got.Type, "search_results")
	}
	if len(got.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(got.Results))
	}
}

// --- 6.06: Pinned Messages ---

func TestPinMessageByOwner(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newCtrlClient("alice")
	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(owner)
	room.AddClient(receiver)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "chat", Message: "important", ChannelID: 1}, owner, room)
	chatMsg := decodeControl(t, ownerBuf)
	_ = decodeControl(t, receiverBuf)

	processControl(ControlMsg{Type: "pin_message", MsgID: chatMsg.MsgID, ChannelID: 1}, owner, room)
	got := decodeControl(t, receiverBuf)
	if got.Type != "message_pinned" {
		t.Errorf("type: got %q, want %q", got.Type, "message_pinned")
	}
	if got.MsgID != chatMsg.MsgID {
		t.Errorf("msg_id: got %d, want %d", got.MsgID, chatMsg.MsgID)
	}
	if got.ID != owner.ID {
		t.Errorf("pinned_by: got %d, want %d", got.ID, owner.ID)
	}
}

func TestPinMessageByNonOwner(t *testing.T) {
	room := NewRoom()
	owner, _ := newCtrlClient("alice")
	user, userBuf := newCtrlClient("bob")
	room.AddClient(owner)
	room.AddClient(user)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "chat", Message: "test", ChannelID: 1}, user, room)
	chatMsg := decodeControl(t, userBuf)

	processControl(ControlMsg{Type: "pin_message", MsgID: chatMsg.MsgID, ChannelID: 1}, user, room)
	if userBuf.Len() != 0 {
		t.Error("non-owner should not be able to pin messages")
	}
}

func TestPinMessageMaxLimit(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	// Create and pin 25 messages
	for i := 0; i < 25; i++ {
		processControl(ControlMsg{Type: "chat", Message: "msg", ChannelID: 1}, owner, room)
		chatMsg := decodeControl(t, ownerBuf)
		processControl(ControlMsg{Type: "pin_message", MsgID: chatMsg.MsgID, ChannelID: 1}, owner, room)
		_ = decodeControl(t, ownerBuf) // drain message_pinned
	}

	// 26th pin should fail
	processControl(ControlMsg{Type: "chat", Message: "one more", ChannelID: 1}, owner, room)
	chatMsg := decodeControl(t, ownerBuf)
	processControl(ControlMsg{Type: "pin_message", MsgID: chatMsg.MsgID, ChannelID: 1}, owner, room)
	if ownerBuf.Len() != 0 {
		t.Error("26th pin should be rejected (max 25 per channel)")
	}
}

func TestPinMessageDuplicate(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "chat", Message: "pin me", ChannelID: 1}, owner, room)
	chatMsg := decodeControl(t, ownerBuf)

	processControl(ControlMsg{Type: "pin_message", MsgID: chatMsg.MsgID, ChannelID: 1}, owner, room)
	_ = decodeControl(t, ownerBuf) // drain first pin

	// Duplicate pin should fail silently
	processControl(ControlMsg{Type: "pin_message", MsgID: chatMsg.MsgID, ChannelID: 1}, owner, room)
	if ownerBuf.Len() != 0 {
		t.Error("duplicate pin should be silently rejected")
	}
}

func TestUnpinMessage(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "chat", Message: "pin me", ChannelID: 1}, owner, room)
	chatMsg := decodeControl(t, ownerBuf)

	processControl(ControlMsg{Type: "pin_message", MsgID: chatMsg.MsgID, ChannelID: 1}, owner, room)
	_ = decodeControl(t, ownerBuf) // drain

	processControl(ControlMsg{Type: "unpin_message", MsgID: chatMsg.MsgID}, owner, room)
	got := decodeControl(t, ownerBuf)
	if got.Type != "message_unpinned" {
		t.Errorf("type: got %q, want %q", got.Type, "message_unpinned")
	}
	if got.MsgID != chatMsg.MsgID {
		t.Errorf("msg_id: got %d, want %d", got.MsgID, chatMsg.MsgID)
	}
}

func TestUnpinMessageByNonOwner(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newCtrlClient("alice")
	user, userBuf := newCtrlClient("bob")
	room.AddClient(owner)
	room.AddClient(user)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "chat", Message: "pin me", ChannelID: 1}, owner, room)
	chatMsg := decodeControl(t, ownerBuf)
	_ = decodeControl(t, userBuf)

	processControl(ControlMsg{Type: "pin_message", MsgID: chatMsg.MsgID, ChannelID: 1}, owner, room)
	_ = decodeControl(t, ownerBuf) // drain
	_ = decodeControl(t, userBuf)  // drain

	processControl(ControlMsg{Type: "unpin_message", MsgID: chatMsg.MsgID}, user, room)
	if userBuf.Len() != 0 {
		t.Error("non-owner should not be able to unpin messages")
	}
}

func TestGetPinnedMessages(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "chat", Message: "important1", ChannelID: 1}, owner, room)
	msg1 := decodeControl(t, ownerBuf)
	processControl(ControlMsg{Type: "chat", Message: "important2", ChannelID: 1}, owner, room)
	msg2 := decodeControl(t, ownerBuf)

	processControl(ControlMsg{Type: "pin_message", MsgID: msg1.MsgID, ChannelID: 1}, owner, room)
	_ = decodeControl(t, ownerBuf)
	processControl(ControlMsg{Type: "pin_message", MsgID: msg2.MsgID, ChannelID: 1}, owner, room)
	_ = decodeControl(t, ownerBuf)

	processControl(ControlMsg{Type: "get_pinned", ChannelID: 1}, owner, room)
	got := decodeControl(t, ownerBuf)
	if got.Type != "pinned_list" {
		t.Fatalf("type: got %q, want %q", got.Type, "pinned_list")
	}
	if len(got.PinnedMsgs) != 2 {
		t.Fatalf("expected 2 pinned messages, got %d", len(got.PinnedMsgs))
	}
}

func TestGetPinnedMessagesZeroChannel(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "get_pinned", ChannelID: 0}, sender, room)
	if senderBuf.Len() != 0 {
		t.Error("get_pinned with channel_id=0 should be rejected")
	}
}

func TestPinMessageZeroMsgID(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "pin_message", MsgID: 0, ChannelID: 1}, owner, room)
	if ownerBuf.Len() != 0 {
		t.Error("pin with msg_id=0 should be rejected")
	}
}

func TestPinMessageZeroChannel(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "pin_message", MsgID: 1, ChannelID: 0}, owner, room)
	if ownerBuf.Len() != 0 {
		t.Error("pin with channel_id=0 should be rejected")
	}
}

// --- Room: message store ---

func TestRecordMsgAndGetPreview(t *testing.T) {
	room := NewRoom()
	room.RecordMsg(1, 42, "alice", "hello world", 1)

	preview := room.GetMsgPreview(1)
	if preview == nil {
		t.Fatal("expected preview, got nil")
	}
	if preview.Username != "alice" {
		t.Errorf("username: got %q, want %q", preview.Username, "alice")
	}
	if preview.Message != "hello world" {
		t.Errorf("message: got %q, want %q", preview.Message, "hello world")
	}
}

func TestGetPreviewTruncatesLongMessage(t *testing.T) {
	room := NewRoom()
	longMsg := ""
	for i := 0; i < 150; i++ {
		longMsg += "a"
	}
	room.RecordMsg(1, 42, "alice", longMsg, 1)

	preview := room.GetMsgPreview(1)
	if preview == nil {
		t.Fatal("expected preview, got nil")
	}
	if len(preview.Message) > 104 { // 100 + "..."
		t.Errorf("message should be truncated, got length %d", len(preview.Message))
	}
}

func TestMarkMsgDeletedAffectsPreview(t *testing.T) {
	room := NewRoom()
	room.RecordMsg(1, 42, "alice", "secret", 1)
	room.MarkMsgDeleted(1)

	preview := room.GetMsgPreview(1)
	if preview == nil {
		t.Fatal("expected preview, got nil")
	}
	if !preview.Deleted {
		t.Error("preview should indicate deleted")
	}
	if preview.Message != "" {
		t.Errorf("deleted message content should be empty, got %q", preview.Message)
	}
}

func TestUpdateMsgContent(t *testing.T) {
	room := NewRoom()
	room.RecordMsg(1, 42, "alice", "original", 1)
	room.UpdateMsgContent(1, "edited")

	preview := room.GetMsgPreview(1)
	if preview == nil {
		t.Fatal("expected preview, got nil")
	}
	if preview.Message != "edited" {
		t.Errorf("message: got %q, want %q", preview.Message, "edited")
	}
}

// --- Room: reaction tracking ---

func TestRoomAddRemoveReaction(t *testing.T) {
	room := NewRoom()

	if !room.AddReaction(1, 10, "👍") {
		t.Error("first add should succeed")
	}
	if room.AddReaction(1, 10, "👍") {
		t.Error("duplicate add should fail")
	}
	if !room.AddReaction(1, 10, "😂") {
		t.Error("different emoji should succeed")
	}
	if !room.AddReaction(1, 20, "👍") {
		t.Error("different user same emoji should succeed")
	}

	if !room.RemoveReaction(1, 10, "👍") {
		t.Error("remove existing should succeed")
	}
	if room.RemoveReaction(1, 10, "👍") {
		t.Error("remove already-removed should fail")
	}
}

func TestRoomGetReactionsAggregation(t *testing.T) {
	room := NewRoom()
	room.AddReaction(1, 10, "👍")
	room.AddReaction(1, 20, "👍")
	room.AddReaction(1, 10, "😂")

	reactions := room.GetReactions(1)
	if len(reactions) != 2 {
		t.Fatalf("expected 2 reaction groups, got %d", len(reactions))
	}
	if reactions[0].Emoji != "👍" || reactions[0].Count != 2 {
		t.Errorf("first: %+v", reactions[0])
	}
	if reactions[1].Emoji != "😂" || reactions[1].Count != 1 {
		t.Errorf("second: %+v", reactions[1])
	}
}

func TestRoomGetReactionsEmpty(t *testing.T) {
	room := NewRoom()
	reactions := room.GetReactions(999)
	if reactions != nil {
		t.Errorf("expected nil for no reactions, got %v", reactions)
	}
}

// --- Room: search ---

func TestRoomSearchMessages(t *testing.T) {
	room := NewRoom()
	room.RecordMsg(1, 10, "alice", "hello world", 1)
	room.RecordMsg(2, 10, "alice", "goodbye world", 1)
	room.RecordMsg(3, 10, "alice", "hello again", 1)

	results := room.SearchMessages(1, "hello", 0, 20)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Newest first
	if results[0].MsgID != 3 {
		t.Errorf("first result msg_id: got %d, want 3", results[0].MsgID)
	}
	if results[1].MsgID != 1 {
		t.Errorf("second result msg_id: got %d, want 1", results[1].MsgID)
	}
}

func TestRoomSearchMessagesLimit(t *testing.T) {
	room := NewRoom()
	for i := uint64(1); i <= 10; i++ {
		room.RecordMsg(i, 10, "alice", "test message", 1)
	}

	results := room.SearchMessages(1, "test", 0, 3)
	if len(results) != 3 {
		t.Errorf("expected 3 results (limited), got %d", len(results))
	}
}

func TestRoomSearchMessagesBefore(t *testing.T) {
	room := NewRoom()
	room.RecordMsg(1, 10, "alice", "first", 1)
	room.RecordMsg(2, 10, "alice", "second", 1)
	room.RecordMsg(3, 10, "alice", "third", 1)

	results := room.SearchMessages(1, "first", 2, 20)
	if len(results) != 1 {
		t.Fatalf("expected 1 result (before=2), got %d", len(results))
	}
	if results[0].MsgID != 1 {
		t.Errorf("result msg_id: got %d, want 1", results[0].MsgID)
	}
}

// --- Room: pinning ---

func TestRoomPinUnpin(t *testing.T) {
	room := NewRoom()
	room.RecordMsg(1, 10, "alice", "important", 1)

	if !room.PinMessage(1, 1, 10) {
		t.Error("first pin should succeed")
	}
	if room.PinMessage(1, 1, 10) {
		t.Error("duplicate pin should fail")
	}

	pinned := room.GetPinnedMessages(1)
	if len(pinned) != 1 {
		t.Fatalf("expected 1 pinned, got %d", len(pinned))
	}
	if pinned[0].Message != "important" {
		t.Errorf("pinned message: got %q, want %q", pinned[0].Message, "important")
	}

	if !room.UnpinMessage(1) {
		t.Error("unpin should succeed")
	}
	if room.UnpinMessage(1) {
		t.Error("unpin non-existent should fail")
	}

	pinned = room.GetPinnedMessages(1)
	if len(pinned) != 0 {
		t.Errorf("expected 0 pinned after unpin, got %d", len(pinned))
	}
}

func TestRoomPinMaxPerChannel(t *testing.T) {
	room := NewRoom()
	for i := uint64(1); i <= 25; i++ {
		room.RecordMsg(i, 10, "alice", "msg", 1)
		if !room.PinMessage(i, 1, 10) {
			t.Fatalf("pin %d should succeed", i)
		}
	}
	room.RecordMsg(26, 10, "alice", "one more", 1)
	if room.PinMessage(26, 1, 10) {
		t.Error("26th pin should be rejected")
	}

	// Different channel should still allow pins
	room.RecordMsg(27, 10, "alice", "other channel", 2)
	if !room.PinMessage(27, 2, 10) {
		t.Error("pin in different channel should succeed")
	}
}

// drainAllControl reads all pending control messages from the buffer.
func drainAllControl(t *testing.T, buf *bytes.Buffer) []ControlMsg {
	t.Helper()
	var msgs []ControlMsg
	for buf.Len() > 0 {
		msgs = append(msgs, decodeControl(t, buf))
	}
	return msgs
}

// --- Integration: chat messages record for search/reply ---

func TestChatMessageRecordedForSearch(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "searchable content", ChannelID: 5}, sender, room)
	_ = decodeControl(t, senderBuf) // drain chat

	results := room.SearchMessages(5, "searchable", 0, 20)
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	if results[0].Message != "searchable content" {
		t.Errorf("message: got %q, want %q", results[0].Message, "searchable content")
	}
}

func TestEditedMessageUpdatesStore(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "original", ChannelID: 1}, sender, room)
	chatMsg := decodeControl(t, senderBuf)

	processControl(ControlMsg{Type: "edit_message", MsgID: chatMsg.MsgID, Message: "edited"}, sender, room)
	_ = decodeControl(t, senderBuf) // drain message_edited

	preview := room.GetMsgPreview(chatMsg.MsgID)
	if preview == nil {
		t.Fatal("expected preview")
	}
	if preview.Message != "edited" {
		t.Errorf("message: got %q, want %q", preview.Message, "edited")
	}
}

func TestDeletedMessageExcludedFromSearch(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "delete me", ChannelID: 1}, sender, room)
	chatMsg := decodeControl(t, senderBuf)

	processControl(ControlMsg{Type: "delete_message", MsgID: chatMsg.MsgID}, sender, room)
	_ = decodeControl(t, senderBuf)

	results := room.SearchMessages(1, "delete", 0, 20)
	if len(results) != 0 {
		t.Errorf("deleted message should not appear in search, got %d results", len(results))
	}
}
