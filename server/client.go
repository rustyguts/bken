package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/webtransport-go"
)

// Circuit breaker constants for datagram fan-out.
// After circuitBreakerThreshold consecutive SendDatagram failures, the breaker
// opens and skips that client in Broadcast.  Every circuitBreakerProbeInterval
// skipped sends, it lets one datagram through to probe for recovery.
const (
	circuitBreakerThreshold     uint32 = 50 // ~1 s of voice at 50 fps
	circuitBreakerProbeInterval uint32 = 25 // attempt a probe every 25 skips
)

// NACK retransmission constants.
const (
	dgramCacheSize = 128 // per-sender ring buffer slots (~2.5 s at 50 fps)
	maxNACKSeqs    = 10  // max sequence numbers per NACK request
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

// cachedDatagram is a single entry in the per-sender datagram ring buffer.
type cachedDatagram struct {
	seq  uint16
	data []byte // full datagram copy (header + opus payload)
	set  bool   // true once this slot has been written at least once
}

// Client represents a connected voice client.
type Client struct {
	ID        uint16
	Username  string
	channelID atomic.Int64 // current channel; 0 = not in any channel; accessed atomically

	// session implements DatagramSender; stored as the interface so tests can mock it.
	session DatagramSender

	health sendHealth // per-client circuit breaker for datagram fan-out

	// dgramMu protects the datagram cache. Written by readDatagrams goroutine,
	// read by processControl (NACK handler). Contention is minimal since
	// NACKs are infrequent relative to the 50 fps datagram rate.
	dgramMu    sync.Mutex
	dgramCache [dgramCacheSize]cachedDatagram

	ctrlMu sync.Mutex
	ctrl   io.Writer // control stream; nil until the join handshake completes
	cancel context.CancelFunc
	closer io.Closer // closes the underlying connection; nil in unit tests
}

// cacheDatagram stores a copy of the datagram in the per-sender ring buffer,
// indexed by seq mod dgramCacheSize. Called from the readDatagrams goroutine.
func (c *Client) cacheDatagram(seq uint16, data []byte) {
	cp := make([]byte, len(data))
	copy(cp, data)
	idx := seq % dgramCacheSize
	c.dgramMu.Lock()
	c.dgramCache[idx] = cachedDatagram{seq: seq, data: cp, set: true}
	c.dgramMu.Unlock()
}

// getCachedDatagram retrieves a cached datagram by sequence number.
// Returns nil if the slot doesn't contain the requested sequence.
func (c *Client) getCachedDatagram(seq uint16) []byte {
	idx := seq % dgramCacheSize
	c.dgramMu.Lock()
	defer c.dgramMu.Unlock()
	entry := c.dgramCache[idx]
	if entry.set && entry.seq == seq {
		return entry.data
	}
	return nil
}

// sendRaw writes a pre-marshaled, newline-terminated JSON message to the control stream.
// It is safe to call concurrently.
func (c *Client) sendRaw(data []byte) {
	c.ctrlMu.Lock()
	defer c.ctrlMu.Unlock()
	if c.ctrl != nil {
		if _, err := c.ctrl.Write(data); err != nil {
			log.Printf("[client %d] control write error: %v", c.ID, err)
		}
	}
}

// sessionCloser adapts *webtransport.Session to io.Closer.
type sessionCloser struct{ sess *webtransport.Session }

func (s *sessionCloser) Close() error { return s.sess.CloseWithError(0, "") }

// SendControl writes a newline-delimited JSON control message to the client's control stream.
// It is safe to call concurrently.
func (c *Client) SendControl(msg ControlMsg) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[ctrl] marshal error: %v", err)
		return
	}
	c.sendRaw(append(data, '\n'))
}

// handleClient manages a single WebTransport session from join to disconnect.
func handleClient(ctx context.Context, sess *webtransport.Session, room *Room) {
	ctx, cancel := context.WithCancel(ctx)
	client := &Client{
		session: sess,
		cancel:  cancel,
		closer:  &sessionCloser{sess},
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
		sess.CloseWithError(0, "bye")
	}()

	// The client is expected to open the control stream first.
	stream, err := sess.AcceptStream(ctx)
	if err != nil {
		log.Printf("[client] accept stream error: %v", err)
		return
	}
	client.ctrl = stream

	// The very first message must be a join.
	reader := bufio.NewReader(stream)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		log.Printf("[client] join read error: %v", err)
		return
	}

	var joinMsg ControlMsg
	if err := json.Unmarshal(line, &joinMsg); err != nil || joinMsg.Type != "join" {
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
			replaced.closer.Close() //nolint:errcheck // best-effort close of replaced duplicate session
		}
		room.BroadcastControl(ControlMsg{Type: "user_left", ID: replacedID}, client.ID)
		if newOwner, changed := room.TransferOwnership(replacedID); changed {
			room.BroadcastControl(ControlMsg{Type: "owner_changed", OwnerID: newOwner}, 0)
		}
		log.Printf("[client %d] replaced duplicate username %q (old client %d)", client.ID, client.Username, replacedID)
	}

	if room.ClaimOwnership(client.ID) {
		log.Printf("[client %d] %s claimed room ownership", client.ID, client.Username)
	}

	// Send the current user list (and server name) to the new client.
	client.SendControl(ControlMsg{Type: "user_list", Users: room.Clients(), ServerName: room.ServerName(), OwnerID: room.OwnerID(), APIPort: room.APIPort()})

	// Always send the channel list so the frontend receives the event even if empty.
	client.SendControl(ControlMsg{Type: "channel_list", Channels: room.GetChannelList()})

	// Notify all other clients that this user joined (channel 0 = not in any channel).
	room.BroadcastControl(
		ControlMsg{Type: "user_joined", ID: client.ID, Username: client.Username},
		client.ID,
	)

	log.Printf("[client %d] %s connected", client.ID, client.Username)

	// Start the datagram relay goroutine.
	go readDatagrams(ctx, sess, room, client)

	// Process control messages until the client disconnects.
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("[client %d] control read error: %v", client.ID, err)
			}
			return
		}
		var msg ControlMsg
		if err := json.Unmarshal(line, &msg); err != nil {
			log.Printf("[client %d] control unmarshal error: %v", client.ID, err)
			continue
		}
		processControl(msg, client, room)
	}
}

// processControl handles a single decoded control message from a client.
// Extracted from the read loop so it can be unit-tested without a real WebTransport session.
func processControl(msg ControlMsg, client *Client, room *Room) {
	switch msg.Type {
	case "ping":
		client.SendControl(ControlMsg{Type: "pong", Timestamp: msg.Timestamp})
	case "chat":
		// Relay to all clients (including sender) so everyone sees the message.
		// Server stamps the authoritative username and timestamp to prevent spoofing.
		// All chat messages (server-wide and channel-scoped) are broadcast to every
		// client; the frontend filters by channel_id on the receiving end so users
		// can read and send messages in any channel without being voice-connected.
		hasFile := msg.FileID != 0
		if msg.Message == "" && !hasFile {
			return
		}
		if len(msg.Message) > MaxChatLength {
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
		}
		room.RecordMsgOwner(msgID, client.ID)
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
			target.closer.Close()
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
	case "nack":
		// Client requests retransmission of missed voice packets from a specific sender.
		// The server looks up cached datagrams and resends them to the requester only.
		if msg.ID == 0 || msg.ID == client.ID || len(msg.Seqs) == 0 {
			return
		}
		seqs := msg.Seqs
		if len(seqs) > maxNACKSeqs {
			seqs = seqs[:maxNACKSeqs]
		}
		sender := room.GetClient(msg.ID)
		if sender == nil {
			return
		}
		// Only retransmit to clients in the same voice channel as the sender.
		senderCh := sender.channelID.Load()
		clientCh := client.channelID.Load()
		if senderCh == 0 || clientCh != senderCh {
			return
		}
		for _, seq := range seqs {
			if data := sender.getCachedDatagram(seq); data != nil {
				client.session.SendDatagram(data) //nolint:errcheck // best-effort retransmit
			}
		}
	case "rename_user":
		// Any client may rename themselves. The server validates the name and
		// broadcasts a user_renamed message so other clients update their user list.
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
		room.BroadcastControl(ControlMsg{
			Type:  "message_deleted",
			MsgID: msg.MsgID,
		}, 0)
		log.Printf("[client %d] %s deleted message %d", client.ID, client.Username, msg.MsgID)
	}
}

// readDatagrams relays incoming voice datagrams from one client to all others.
// It stamps the sender ID into the datagram header before fan-out to prevent
// spoofing, and caches each datagram for NACK-based retransmission.
func readDatagrams(ctx context.Context, sess *webtransport.Session, room *Room, client *Client) {
	for {
		data, err := sess.ReceiveDatagram(ctx)
		if err != nil {
			if ctx.Err() == nil {
				// Not a clean shutdown — log so operators can see unexpected drops.
				log.Printf("[client %d] datagram read error: %v", client.ID, err)
			}
			return
		}
		if len(data) < DatagramHeader || len(data) > MaxDatagramSize {
			continue // need header; reject oversized packets
		}
		// Overwrite the client-supplied sender ID to prevent spoofing.
		binary.BigEndian.PutUint16(data[:2], client.ID)

		// Cache for NACK retransmission before broadcasting.
		seq := binary.BigEndian.Uint16(data[2:4])
		client.cacheDatagram(seq, data)

		room.Broadcast(client.ID, data)
	}
}
