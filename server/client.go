package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"sync"

	"github.com/quic-go/webtransport-go"
)

// Client represents a connected voice client.
type Client struct {
	ID       uint16
	Username string

	// session implements DatagramSender; stored as the interface so tests can mock it.
	session DatagramSender

	ctrlMu sync.Mutex
	ctrl   *webtransport.Stream
	cancel context.CancelFunc
}

// SendControl writes a newline-delimited JSON control message to the client's control stream.
// It is safe to call concurrently.
func (c *Client) SendControl(msg ControlMsg) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	data = append(data, '\n')

	c.ctrlMu.Lock()
	defer c.ctrlMu.Unlock()
	if c.ctrl != nil {
		if _, err := c.ctrl.Write(data); err != nil {
			log.Printf("[client %d] control write error: %v", c.ID, err)
		}
	}
}

// handleClient manages a single WebTransport session from join to disconnect.
func handleClient(ctx context.Context, sess *webtransport.Session, room *Room) {
	ctx, cancel := context.WithCancel(ctx)
	client := &Client{
		session: sess,
		cancel:  cancel,
	}

	defer func() {
		cancel()
		if client.ID != 0 {
			room.RemoveClient(client.ID)
			room.BroadcastControl(ControlMsg{Type: "user_left", ID: client.ID}, 0)
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

	client.Username = joinMsg.Username
	room.AddClient(client)

	// Send the current user list to the new client.
	client.SendControl(ControlMsg{Type: "user_list", Users: room.Clients()})

	// Notify all other clients that this user joined.
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
			continue
		}
		if msg.Type == "ping" {
			client.SendControl(ControlMsg{Type: "pong", Timestamp: msg.Timestamp})
		}
	}
}

// readDatagrams relays incoming voice datagrams from one client to all others.
// It stamps the sender ID into the datagram header before fan-out to prevent spoofing.
func readDatagrams(ctx context.Context, sess *webtransport.Session, room *Room, senderID uint16) {
	for {
		data, err := sess.ReceiveDatagram(ctx)
		if err != nil {
			return
		}
		if len(data) < 4 {
			continue // need at least senderID(2) + seq(2)
		}
		// Overwrite the client-supplied sender ID to prevent spoofing.
		binary.BigEndian.PutUint16(data[:2], senderID)
		room.Broadcast(senderID, data)
	}
}
