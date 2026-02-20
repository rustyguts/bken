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

// Client represents a connected voice client.
type Client struct {
	ID        uint16
	Username  string
	channelID atomic.Int64 // current channel; 0 = not in any channel; accessed atomically

	// session implements DatagramSender; stored as the interface so tests can mock it.
	session DatagramSender

	ctrlMu sync.Mutex
	ctrl   io.Writer // control stream; nil until the join handshake completes
	cancel context.CancelFunc
	closer io.Closer // closes the underlying connection; nil in unit tests
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
			room.RemoveClient(client.ID)
			room.BroadcastControl(ControlMsg{Type: "user_left", ID: client.ID}, 0)
			if newOwner, changed := room.TransferOwnership(client.ID); changed {
				room.BroadcastControl(ControlMsg{Type: "owner_changed", OwnerID: newOwner}, 0)
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
	room.AddClient(client)

	if room.ClaimOwnership(client.ID) {
		log.Printf("[client %d] %s claimed room ownership", client.ID, client.Username)
	}

	// Send the current user list (and server name) to the new client.
	client.SendControl(ControlMsg{Type: "user_list", Users: room.Clients(), ServerName: room.ServerName(), OwnerID: room.OwnerID()})

	// Send the current channel list to the new client.
	if channels := room.GetChannelList(); len(channels) > 0 {
		client.SendControl(ControlMsg{Type: "channel_list", Channels: channels})
	}

	// Notify all other clients that this user joined (channel 0 = not in any channel).
	room.BroadcastControl(
		ControlMsg{Type: "user_joined", ID: client.ID, Username: client.Username},
		client.ID,
	)

	log.Printf("[client %d] %s connected", client.ID, client.Username)

	// Start the datagram relay goroutine.
	go readDatagrams(ctx, sess, room, client.ID)

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
		// Server stamps the authoritative username, timestamp, and channel_id to
		// prevent spoofing.  When the client requests a channel-scoped message
		// (ChannelID != 0), the server uses the SENDER'S actual channelID —
		// ignoring the client-supplied value — so clients cannot fake routing.
		if msg.Message == "" || len(msg.Message) > MaxChatLength {
			return
		}
		out := ControlMsg{
			Type:      "chat",
			ID:        client.ID,
			Username:  client.Username,
			Message:   msg.Message,
			Timestamp: time.Now().UnixMilli(),
		}
		chID := client.channelID.Load()
		if msg.ChannelID != 0 && chID != 0 {
			// Channel-scoped: fan out only to users in the sender's current channel.
			out.ChannelID = chID
			room.BroadcastToChannel(chID, out)
		} else {
			// Server-level: fan out to everyone.
			room.BroadcastControl(out, 0)
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
		room.Rename(name)
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
	}
}

// readDatagrams relays incoming voice datagrams from one client to all others.
// It stamps the sender ID into the datagram header before fan-out to prevent spoofing.
func readDatagrams(ctx context.Context, sess *webtransport.Session, room *Room, senderID uint16) {
	for {
		data, err := sess.ReceiveDatagram(ctx)
		if err != nil {
			if ctx.Err() == nil {
				// Not a clean shutdown — log so operators can see unexpected drops.
				log.Printf("[client %d] datagram read error: %v", senderID, err)
			}
			return
		}
		if len(data) < DatagramHeader {
			continue // need at least senderID(2) + seq(2)
		}
		// Overwrite the client-supplied sender ID to prevent spoofing.
		binary.BigEndian.PutUint16(data[:2], senderID)
		room.Broadcast(senderID, data)
	}
}
