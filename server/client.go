package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// sendHealth tracks per-client datagram send success and implements a
// lightweight circuit breaker so the server stops wasting effort on
// unreachable peers.
type sendHealth struct {
	failures atomic.Uint32 // consecutive SendDatagram failures
	skips    atomic.Uint32 // skips since the breaker opened; used for probe cadence
}

// shouldSkip returns true when the breaker is open and it is not yet time
// for a probe attempt.  Callers should skip the send when this returns true.
func (h *sendHealth) shouldSkip() bool {
	if h.failures.Load() < circuitBreakerThreshold {
		return false
	}
	// Breaker is open — allow a probe every probeInterval skips.
	s := h.skips.Add(1)
	return s%circuitBreakerProbeInterval != 0
}

// recordFailure increments the consecutive failure counter and returns
// the new value.
func (h *sendHealth) recordFailure() uint32 {
	return h.failures.Add(1)
}

// recordSuccess resets the failure and skip counters.  It returns true if
// the breaker was previously open (i.e. the send was a successful probe).
func (h *sendHealth) recordSuccess() bool {
	wasTripped := h.failures.Swap(0) >= circuitBreakerThreshold
	if wasTripped {
		h.skips.Store(0)
	}
	return wasTripped
}

// Client represents a connected client.
type Client struct {
	ID        uint16
	Username  string
	channelID atomic.Int64 // current channel; 0 = not in any channel; accessed atomically

	// session implements DatagramSender; retained for test-bot voice fan-out and
	// best-effort compatibility with legacy NACK logic.
	session DatagramSender

	health sendHealth // per-client circuit breaker for datagram fan-out

	ctrlMu sync.Mutex
	ctrl   io.Writer       // nil when websocket transport is active
	ws     *websocket.Conn // active websocket for control/signaling

	cancel context.CancelFunc
	closer io.Closer // closes the underlying connection; nil in unit tests

	// Administration
	role      string // OWNER/ADMIN/MODERATOR/USER; protected by Room.mu
	muted     bool   // server-side mute; protected by Room.mu
	muteExpiry int64 // unix ms when mute expires; 0 = permanent; protected by Room.mu
	remoteIP  string // client IP for ban checking

	// Rate limiting
	lastControlMsg time.Time // last control message time for rate limiting
	controlMsgCount int      // control messages in current second
	lastChatTime   map[int64]time.Time // last chat time per channel for slow mode
}

// sendRaw writes a pre-marshaled control message to the client.
// It is safe to call concurrently.
func (c *Client) sendRaw(data []byte) {
	c.ctrlMu.Lock()
	defer c.ctrlMu.Unlock()

	if c.ws != nil {
		payload := data
		if n := len(payload); n > 0 && payload[n-1] == '\n' {
			payload = payload[:n-1]
		}
		if err := c.ws.WriteMessage(websocket.TextMessage, payload); err != nil {
			log.Printf("[client %d] control write error: %v", c.ID, err)
		}
		return
	}

	if c.ctrl != nil {
		if _, err := c.ctrl.Write(data); err != nil {
			log.Printf("[client %d] control write error: %v", c.ID, err)
		}
	}
}

// SendControl writes a JSON control message to the client's control transport.
// It is safe to call concurrently.
func (c *Client) SendControl(msg ControlMsg) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[ctrl] marshal error: %v", err)
		return
	}
	c.sendRaw(append(data, '\n'))
}

// handleWebSocketClient manages a single WebSocket client from join to disconnect.
func handleWebSocketClient(ctx context.Context, conn *websocket.Conn, room *Room) {
	ctx, cancel := context.WithCancel(ctx)
	client := &Client{
		ws:     conn,
		cancel: cancel,
		closer: conn,
	}

	defer func() {
		cancel()
		if client.ID != 0 {
			if room.RemoveClient(client.ID) {
				room.BroadcastControl(ControlMsg{Type: "user_left", ID: client.ID}, 0)
				if newOwner, changed := room.TransferOwnership(client.ID); changed {
					room.BroadcastControl(ControlMsg{Type: "owner_changed", OwnerID: newOwner}, 0)
				}
			}
		}
		_ = conn.Close()
	}()

	_, data, err := conn.ReadMessage()
	if err != nil {
		log.Printf("[client] join read error: %v", err)
		return
	}

	var joinMsg ControlMsg
	if err := json.Unmarshal(data, &joinMsg); err != nil || joinMsg.Type != "join" {
		log.Printf("[client] invalid join message: %v", err)
		return
	}

	username, err := validateName(joinMsg.Username, MaxNameLength)
	if err != nil {
		log.Printf("[client] join rejected: %v", err)
		return
	}
	client.Username = username

	_, replaced, replacedID := room.AddOrReplaceClient(client)
	if replaced != nil {
		if replaced.cancel != nil {
			replaced.cancel()
		}
		if replaced.closer != nil {
			_ = replaced.closer.Close() //nolint:errcheck // best-effort close of replaced duplicate session
		}
		room.BroadcastControl(ControlMsg{Type: "user_left", ID: replacedID}, client.ID)
		if newOwner, changed := room.TransferOwnership(replacedID); changed {
			room.BroadcastControl(ControlMsg{Type: "owner_changed", OwnerID: newOwner}, 0)
		}
		log.Printf("[client %d] replaced duplicate username %q (old client %d)", client.ID, client.Username, replacedID)
	}

	if room.ClaimOwnership(client.ID) {
		room.SetClientRole(client.ID, RoleOwner)
		log.Printf("[client %d] %s claimed room ownership", client.ID, client.Username)
	}

	// Send the current user list (and server name) to the new client.
	welcome := ControlMsg{
		Type:       "user_list",
		SelfID:     client.ID,
		Users:      room.Clients(),
		ServerName: room.ServerName(),
		OwnerID:    room.OwnerID(),
		APIPort:    room.APIPort(),
	}
	if iceServers := room.ICEServers(); len(iceServers) > 0 {
		welcome.ICEServers = iceServers
	}
	client.SendControl(welcome)

	// Always send the channel list so the frontend receives the event even if empty.
	client.SendControl(ControlMsg{Type: "channel_list", Channels: room.GetChannelList()})

	// Send current announcement if one exists.
	if ann, annUser := room.GetAnnouncement(); ann != "" {
		client.SendControl(ControlMsg{
			Type:         "announcement",
			Announcement: ann,
			Username:     annUser,
		})
	}

	// Notify all other clients that this user joined (channel 0 = not in any channel).
	room.BroadcastControl(
		ControlMsg{Type: "user_joined", ID: client.ID, Username: client.Username, Role: room.GetClientRole(client.ID)},
		client.ID,
	)

	log.Printf("[client %d] %s connected", client.ID, client.Username)

	// Process control messages until the client disconnects.
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return
			}
			if ctx.Err() == nil {
				log.Printf("[client %d] control read error: %v", client.ID, err)
			}
			return
		}

		var msg ControlMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("[client %d] control unmarshal error: %v", client.ID, err)
			continue
		}
		processControl(msg, client, room)
	}
}

// parseMentions extracts @DisplayName tokens from the message text and resolves
// them to user IDs using the current room client list. Returns a deduplicated
// list of mentioned user IDs.
func parseMentions(text string, room *Room) []uint16 {
	if !strings.Contains(text, "@") {
		return nil
	}
	clients := room.Clients()
	var mentioned []uint16
	seen := make(map[uint16]bool)
	// Check longest usernames first to match greedily.
	for _, u := range clients {
		token := "@" + u.Username
		if strings.Contains(text, token) && !seen[u.ID] {
			seen[u.ID] = true
			mentioned = append(mentioned, u.ID)
		}
	}
	return mentioned
}

// processControl handles a single decoded control message from a client.
func processControl(msg ControlMsg, client *Client, room *Room) {
	switch msg.Type {
	case "ping":
		client.SendControl(ControlMsg{Type: "pong", Timestamp: msg.Timestamp})
	case "chat":
		// Relay to all clients (including sender) so everyone sees the message.
		// Server stamps the authoritative username and timestamp to prevent spoofing.
		hasFile := msg.FileID != 0
		if msg.Message == "" && !hasFile {
			return
		}
		if len(msg.Message) > MaxChatLength {
			return
		}
		// Enforce slow mode.
		if msg.ChannelID != 0 && !room.CheckSlowMode(client.ID, msg.ChannelID) {
			client.SendControl(ControlMsg{Type: "error", Error: "slow_mode", ChannelID: msg.ChannelID})
			return
		}
		msgID := room.NextMsgID()
		out := ControlMsg{
			Type:      "chat",
			ID:        client.ID,
			Username:  client.Username,
			Message:   msg.Message,
			Timestamp: time.Now().UnixMilli(),
			ChannelID: msg.ChannelID, // 0 = server-wide, non-zero = channel-scoped
			FileID:    msg.FileID,
			FileName:  msg.FileName,
			FileSize:  msg.FileSize,
			MsgID:     msgID,
			ReplyTo:   msg.ReplyTo,
		}
		// Parse @mentions
		if mentions := parseMentions(msg.Message, room); len(mentions) > 0 {
			out.Mentions = mentions
		}
		// Attach reply preview if replying
		if msg.ReplyTo != 0 {
			if preview := room.GetMsgPreview(msg.ReplyTo); preview != nil {
				out.ReplyPreview = preview
			}
		}
		room.RecordMsg(msgID, client.ID, client.Username, msg.Message, msg.ChannelID)
		room.BufferMessage(msg.ChannelID, out)
		room.BroadcastControl(out, 0)

		// Asynchronously fetch a link preview if the message contains a URL.
		if rawURL := extractFirstURL(msg.Message); rawURL != "" {
			go func() {
				lp, err := fetchLinkPreview(rawURL)
				if err != nil {
					log.Printf("[linkpreview] fetch %q: %v", rawURL, err)
					return
				}
				if lp.Title == "" && lp.Desc == "" && lp.Image == "" {
					return // nothing useful to show
				}
				room.BroadcastControl(ControlMsg{
					Type:      "link_preview",
					MsgID:     msgID,
					ChannelID: msg.ChannelID,
					LinkURL:   lp.URL,
					LinkTitle: lp.Title,
					LinkDesc:  lp.Desc,
					LinkImage: lp.Image,
					LinkSite:  lp.SiteName,
				}, 0)
			}()
		}
	case "kick":
		// Only the room owner may kick. Owners cannot kick themselves.
		if room.OwnerID() != client.ID || msg.ID == 0 || msg.ID == client.ID {
			return
		}
		target := room.GetClient(msg.ID)
		if target == nil {
			return
		}
		log.Printf("[client %d] %s kicked client %d", client.ID, client.Username, msg.ID)
		target.SendControl(ControlMsg{Type: "kicked"})
		target.cancel()
		if target.closer != nil {
			_ = target.closer.Close()
		}
	case "rename":
		// Only the room owner may rename the server.
		if room.OwnerID() != client.ID {
			return
		}
		name, err := validateName(msg.ServerName, MaxNameLength)
		if err != nil {
			return
		}
		if err := room.Rename(name); err != nil {
			log.Printf("[client %d] rename persist error: %v", client.ID, err)
			return
		}
		room.BroadcastControl(ControlMsg{Type: "server_info", ServerName: name}, 0)
		log.Printf("[client %d] %s renamed server to %q", client.ID, client.Username, name)
	case "join_channel":
		// Any client may join a channel (including channel 0 to leave all channels).
		// Enforce the channel user limit (channel 0 = lobby, always allowed).
		if msg.ChannelID != 0 && !room.CanJoinChannel(msg.ChannelID) {
			client.SendControl(ControlMsg{Type: "error", Error: "Channel is full"})
			return
		}
		client.channelID.Store(msg.ChannelID)
		room.BroadcastControl(ControlMsg{
			Type:      "user_channel",
			ID:        client.ID,
			ChannelID: msg.ChannelID,
		}, 0)
		log.Printf("[client %d] %s joined channel %d", client.ID, client.Username, msg.ChannelID)
	case "create_channel":
		// Only the room owner may create channels.
		if room.OwnerID() != client.ID {
			return
		}
		name, err := validateName(msg.Message, MaxNameLength)
		if err != nil {
			return
		}
		id, err := room.CreateChannel(name)
		if err != nil {
			log.Printf("[client %d] create channel error: %v", client.ID, err)
			return
		}
		log.Printf("[client %d] %s created channel %d %q", client.ID, client.Username, id, name)
	case "rename_channel":
		// Only the room owner may rename channels.
		if room.OwnerID() != client.ID {
			return
		}
		if msg.ChannelID == 0 {
			return
		}
		name, err := validateName(msg.Message, MaxNameLength)
		if err != nil {
			return
		}
		if err := room.RenameChannel(msg.ChannelID, name); err != nil {
			log.Printf("[client %d] rename channel %d error: %v", client.ID, msg.ChannelID, err)
			return
		}
		log.Printf("[client %d] %s renamed channel %d to %q", client.ID, client.Username, msg.ChannelID, name)
	case "delete_channel":
		// Only the room owner may delete channels.
		if room.OwnerID() != client.ID {
			return
		}
		if msg.ChannelID == 0 {
			return
		}
		// Prevent deleting the last channel — there must always be at least one.
		if room.ChannelCount() <= 1 {
			return
		}
		if err := room.DeleteChannel(msg.ChannelID); err != nil {
			log.Printf("[client %d] delete channel %d error: %v", client.ID, msg.ChannelID, err)
			return
		}
		// Move users who were in the deleted channel back to the lobby.
		room.MoveChannelUsersToLobby(msg.ChannelID)
		log.Printf("[client %d] %s deleted channel %d", client.ID, client.Username, msg.ChannelID)
	case "rename_user":
		// Any client may rename themselves.
		name, err := validateName(msg.Username, MaxNameLength)
		if err != nil {
			return
		}
		client.Username = name
		room.BroadcastControl(ControlMsg{Type: "user_renamed", ID: client.ID, Username: name}, 0)
		log.Printf("[client %d] renamed to %q", client.ID, name)
	case "move_user":
		// Only the room owner may move other users between channels.
		if room.OwnerID() != client.ID || msg.ID == 0 || msg.ID == client.ID {
			return
		}
		target := room.GetClient(msg.ID)
		if target == nil {
			return
		}
		target.channelID.Store(msg.ChannelID)
		room.BroadcastControl(ControlMsg{
			Type:      "user_channel",
			ID:        msg.ID,
			ChannelID: msg.ChannelID,
		}, 0)
		log.Printf("[client %d] %s moved client %d to channel %d", client.ID, client.Username, msg.ID, msg.ChannelID)
	case "edit_message":
		// A user may only edit their own messages.
		if msg.MsgID == 0 || msg.Message == "" || len(msg.Message) > MaxChatLength {
			return
		}
		ownerID, ok := room.GetMsgOwner(msg.MsgID)
		if !ok || ownerID != client.ID {
			return
		}
		room.UpdateMsgContent(msg.MsgID, msg.Message)
		room.BroadcastControl(ControlMsg{
			Type:      "message_edited",
			MsgID:     msg.MsgID,
			Message:   msg.Message,
			Timestamp: time.Now().UnixMilli(),
		}, 0)
		log.Printf("[client %d] %s edited message %d", client.ID, client.Username, msg.MsgID)
	case "delete_message":
		// A user may delete their own messages; the room owner may delete any message.
		if msg.MsgID == 0 {
			return
		}
		ownerID, ok := room.GetMsgOwner(msg.MsgID)
		if !ok {
			return
		}
		isRoomOwner := room.OwnerID() == client.ID
		if ownerID != client.ID && !isRoomOwner {
			return
		}
		room.MarkMsgDeleted(msg.MsgID)
		room.BroadcastControl(ControlMsg{
			Type:  "message_deleted",
			MsgID: msg.MsgID,
		}, 0)
		log.Printf("[client %d] %s deleted message %d", client.ID, client.Username, msg.MsgID)
	case "webrtc_offer", "webrtc_answer", "webrtc_ice":
		if msg.TargetID == 0 || msg.TargetID == client.ID {
			return
		}
		target := room.GetClient(msg.TargetID)
		if target == nil {
			return
		}
		fwd := ControlMsg{
			Type:          msg.Type,
			ID:            client.ID,
			SDP:           msg.SDP,
			Candidate:     msg.Candidate,
			SDPMid:        msg.SDPMid,
			SDPMLineIndex: msg.SDPMLineIndex,
		}
		target.SendControl(fwd)
	case "video_state":
		// Broadcast the user's video state to all other clients so they
		// can update their UI (show/hide video grid tiles, screen share badges).
		// The server stamps the authoritative sender ID to prevent spoofing.
		// Include simulcast layers when video is starting.
		broadcast := ControlMsg{
			Type:        "video_state",
			ID:          client.ID,
			VideoActive: msg.VideoActive,
			ScreenShare: msg.ScreenShare,
		}
		active := msg.VideoActive != nil && *msg.VideoActive
		screen := msg.ScreenShare != nil && *msg.ScreenShare
		if active {
			// Attach default simulcast layers so receivers know what qualities
			// are available and can request their preferred layer.
			broadcast.VideoLayers = DefaultVideoLayers()
			if screen {
				log.Printf("[client %d] %s started screen share", client.ID, client.Username)
			} else {
				log.Printf("[client %d] %s started video", client.ID, client.Username)
			}
		} else {
			log.Printf("[client %d] %s stopped video", client.ID, client.Username)
		}
		room.BroadcastControl(broadcast, 0)

	case "set_video_quality":
		// A receiver requests a specific simulcast layer from a video sender.
		// The server relays this to the target user so they can adjust their
		// encoder output or select the appropriate simulcast track.
		if msg.TargetID == 0 || msg.VideoQuality == "" {
			return
		}
		// Validate quality value.
		switch msg.VideoQuality {
		case "high", "medium", "low":
		default:
			return
		}
		room.SendControlTo(msg.TargetID, ControlMsg{
			Type:         "set_video_quality",
			ID:           client.ID,
			VideoQuality: msg.VideoQuality,
		})

	case "add_reaction":
		if msg.MsgID == 0 || msg.Emoji == "" {
			return
		}
		if room.AddReaction(msg.MsgID, client.ID, msg.Emoji) {
			room.BroadcastControl(ControlMsg{
				Type:  "reaction_added",
				MsgID: msg.MsgID,
				Emoji: msg.Emoji,
				ID:    client.ID,
			}, 0)
		}

	case "remove_reaction":
		if msg.MsgID == 0 || msg.Emoji == "" {
			return
		}
		if room.RemoveReaction(msg.MsgID, client.ID, msg.Emoji) {
			room.BroadcastControl(ControlMsg{
				Type:  "reaction_removed",
				MsgID: msg.MsgID,
				Emoji: msg.Emoji,
				ID:    client.ID,
			}, 0)
		}

	case "typing":
		if msg.ChannelID == 0 {
			return
		}
		room.BroadcastControl(ControlMsg{
			Type:      "user_typing",
			ID:        client.ID,
			ChannelID: msg.ChannelID,
			Username:  client.Username,
		}, client.ID)

	case "search_messages":
		if msg.Query == "" || msg.ChannelID == 0 {
			return
		}
		limit := msg.Limit
		if limit <= 0 || limit > 50 {
			limit = 20
		}
		results := room.SearchMessages(msg.ChannelID, msg.Query, msg.Before, limit)
		client.SendControl(ControlMsg{
			Type:      "search_results",
			ChannelID: msg.ChannelID,
			Query:     msg.Query,
			Results:   results,
		})

	case "pin_message":
		if room.OwnerID() != client.ID {
			return
		}
		if msg.MsgID == 0 || msg.ChannelID == 0 {
			return
		}
		if room.PinMessage(msg.MsgID, msg.ChannelID, client.ID) {
			room.BroadcastControl(ControlMsg{
				Type:      "message_pinned",
				MsgID:     msg.MsgID,
				ChannelID: msg.ChannelID,
				ID:        client.ID,
			}, 0)
		}

	case "unpin_message":
		if room.OwnerID() != client.ID {
			return
		}
		if msg.MsgID == 0 {
			return
		}
		if room.UnpinMessage(msg.MsgID) {
			room.BroadcastControl(ControlMsg{
				Type:  "message_unpinned",
				MsgID: msg.MsgID,
			}, 0)
		}

	case "get_pinned":
		if msg.ChannelID == 0 {
			return
		}
		pinned := room.GetPinnedMessages(msg.ChannelID)
		client.SendControl(ControlMsg{
			Type:       "pinned_list",
			ChannelID:  msg.ChannelID,
			PinnedMsgs: pinned,
		})

	case "get_reactions":
		if msg.MsgID == 0 {
			return
		}
		reactions := room.GetReactions(msg.MsgID)
		client.SendControl(ControlMsg{
			Type:      "reactions_list",
			MsgID:     msg.MsgID,
			Reactions: reactions,
		})

	case "ban":
		// ADMIN+ can ban users. Cannot ban yourself or the owner.
		if !HasPermission(client.role, "ban") {
			return
		}
		if msg.ID == 0 || msg.ID == client.ID {
			return
		}
		target := room.GetClient(msg.ID)
		if target == nil {
			return
		}
		// Cannot ban the owner.
		if room.OwnerID() == msg.ID {
			return
		}
		reason := msg.Reason
		if reason == "" {
			reason = "No reason provided"
		}
		ip := ""
		if msg.BanIP {
			ip = target.remoteIP
		}
		room.RecordBan(target.Username, ip, reason, client.Username, msg.Duration)
		log.Printf("[client %d] %s banned client %d (%s), reason: %s", client.ID, client.Username, msg.ID, target.Username, reason)
		target.SendControl(ControlMsg{Type: "banned", Reason: reason})
		target.cancel()
		if target.closer != nil {
			_ = target.closer.Close()
		}

	case "unban":
		if !HasPermission(client.role, "unban") {
			return
		}
		if msg.BanID == 0 {
			return
		}
		room.RemoveBan(msg.BanID)
		log.Printf("[client %d] %s removed ban %d", client.ID, client.Username, msg.BanID)

	case "set_role":
		// Only the owner can set roles.
		if !HasPermission(client.role, "set_role") {
			return
		}
		if msg.ID == 0 || msg.ID == client.ID {
			return
		}
		role := msg.Role
		if role != RoleAdmin && role != RoleModerator && role != RoleUser {
			return
		}
		target := room.GetClient(msg.ID)
		if target == nil {
			return
		}
		room.SetClientRole(msg.ID, role)
		room.BroadcastControl(ControlMsg{
			Type: "role_changed",
			ID:   msg.ID,
			Role: role,
		}, 0)
		log.Printf("[client %d] %s set role of client %d to %s", client.ID, client.Username, msg.ID, role)

	case "announce":
		if !HasPermission(client.role, "announce") {
			return
		}
		if msg.Announcement == "" || len(msg.Announcement) > MaxChatLength {
			return
		}
		room.SetAnnouncement(msg.Announcement, client.Username)
		room.BroadcastControl(ControlMsg{
			Type:         "announcement",
			Announcement: msg.Announcement,
			Username:     client.Username,
		}, 0)
		log.Printf("[client %d] %s sent announcement", client.ID, client.Username)

	case "set_slow_mode":
		if !HasPermission(client.role, "set_slow_mode") {
			return
		}
		if msg.ChannelID == 0 {
			return
		}
		seconds := msg.SlowMode
		if seconds < 0 {
			seconds = 0
		}
		if seconds > 3600 {
			seconds = 3600
		}
		room.SetSlowMode(msg.ChannelID, seconds)
		room.BroadcastControl(ControlMsg{
			Type:      "slow_mode_set",
			ChannelID: msg.ChannelID,
			SlowMode:  seconds,
		}, 0)
		log.Printf("[client %d] %s set slow mode on channel %d to %ds", client.ID, client.Username, msg.ChannelID, seconds)

	case "mute_user":
		// ADMIN+ can mute users.
		if !HasPermission(client.role, "mute") {
			return
		}
		if msg.ID == 0 || msg.ID == client.ID {
			return
		}
		target := room.GetClient(msg.ID)
		if target == nil {
			return
		}
		if room.OwnerID() == msg.ID {
			return
		}
		var expiry int64
		if msg.Duration > 0 {
			expiry = time.Now().Add(time.Duration(msg.Duration) * time.Second).UnixMilli()
		}
		room.SetClientMute(msg.ID, true, expiry)
		room.BroadcastControl(ControlMsg{
			Type:       "user_muted",
			ID:         msg.ID,
			Muted:      true,
			MuteExpiry: expiry,
		}, 0)
		log.Printf("[client %d] %s muted client %d (duration=%ds)", client.ID, client.Username, msg.ID, msg.Duration)

	case "unmute_user":
		if !HasPermission(client.role, "unmute") {
			return
		}
		if msg.ID == 0 {
			return
		}
		room.SetClientMute(msg.ID, false, 0)
		room.BroadcastControl(ControlMsg{
			Type:  "user_muted",
			ID:    msg.ID,
			Muted: false,
		}, 0)
		log.Printf("[client %d] %s unmuted client %d", client.ID, client.Username, msg.ID)

	case "set_channel_limit":
		// Only the room owner may set channel user limits.
		if room.OwnerID() != client.ID {
			return
		}
		if msg.ChannelID == 0 {
			return
		}
		maxUsers := msg.MaxUsers
		if maxUsers < 0 {
			maxUsers = 0
		}
		if maxUsers > 999 {
			maxUsers = 999
		}
		room.SetChannelMaxUsers(msg.ChannelID, maxUsers)
		room.refreshChannels()
		log.Printf("[client %d] %s set channel %d max_users to %d", client.ID, client.Username, msg.ChannelID, maxUsers)

	case "start_recording":
		// Only the room owner may start recording.
		if room.OwnerID() != client.ID {
			return
		}
		if msg.ChannelID == 0 {
			return
		}
		if err := room.StartRecordingChannel(msg.ChannelID, client.Username); err != nil {
			client.SendControl(ControlMsg{Type: "error", Error: err.Error()})
			return
		}
		rec := true
		room.BroadcastToChannel(msg.ChannelID, ControlMsg{
			Type:      "recording_started",
			ChannelID: msg.ChannelID,
			Recording: &rec,
			Username:  client.Username,
		})
		log.Printf("[client %d] %s started recording channel %d", client.ID, client.Username, msg.ChannelID)

	case "stop_recording":
		// Only the room owner may stop recording.
		if room.OwnerID() != client.ID {
			return
		}
		if msg.ChannelID == 0 {
			return
		}
		if err := room.StopRecordingChannel(msg.ChannelID); err != nil {
			client.SendControl(ControlMsg{Type: "error", Error: err.Error()})
			return
		}
		rec := false
		room.BroadcastToChannel(msg.ChannelID, ControlMsg{
			Type:      "recording_stopped",
			ChannelID: msg.ChannelID,
			Recording: &rec,
		})
		log.Printf("[client %d] %s stopped recording channel %d", client.ID, client.Username, msg.ChannelID)

	case "list_recordings":
		recordings := room.ListRecordings()
		client.SendControl(ControlMsg{
			Type:       "recordings_list",
			Recordings: recordings,
		})

	case "replay":
		// Client requests replay of missed messages after reconnect.
		if msg.ChannelID == 0 {
			return
		}
		messages := room.GetMessagesSince(msg.ChannelID, msg.LastSeq)
		for _, m := range messages {
			client.SendControl(m)
		}
	}
}
