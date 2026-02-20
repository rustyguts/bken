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
	Session  *webtransport.Session

	ctrlMu sync.Mutex
	ctrl   *webtransport.Stream
	cancel context.CancelFunc
}

// SendControl writes a JSON control message to this client's control stream.
func (c *Client) SendControl(msg ControlMsg) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	data = append(data, '\n')

	c.ctrlMu.Lock()
	defer c.ctrlMu.Unlock()
	if c.ctrl != nil {
		c.ctrl.Write(data)
	}
}

// Control message types (JSON over reliable bidirectional stream).
type ControlMsg struct {
	Type     string     `json:"type"`
	Username string     `json:"username,omitempty"`
	ID       uint16     `json:"id,omitempty"`
	Users    []UserInfo `json:"users,omitempty"`
	Ts       int64      `json:"ts,omitempty"` // ping/pong timestamp (Unix ms)
}

type UserInfo struct {
	ID       uint16 `json:"id"`
	Username string `json:"username"`
}

// handleClient manages a single WebTransport session.
func handleClient(ctx context.Context, sess *webtransport.Session, room *Room) {
	ctx, cancel := context.WithCancel(ctx)
	client := &Client{
		Session: sess,
		cancel:  cancel,
	}

	defer func() {
		cancel()
		if client.ID != 0 {
			room.RemoveClient(client.ID)
			broadcastUserLeft(room, client.ID)
		}
		sess.CloseWithError(0, "bye")
	}()

	// Accept the control stream (client opens it).
	stream, err := sess.AcceptStream(ctx)
	if err != nil {
		log.Printf("[client] accept stream: %v", err)
		return
	}
	client.ctrl = stream

	// Read join message.
	reader := bufio.NewReader(stream)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		log.Printf("[client] no join message: %v", err)
		return
	}

	var joinMsg ControlMsg
	if err := json.Unmarshal(line, &joinMsg); err != nil || joinMsg.Type != "join" {
		log.Printf("[client] invalid join message: %v", err)
		return
	}

	client.Username = joinMsg.Username
	room.AddClient(client)

	// Send current user list to the new client.
	sendUserList(client, room)

	// Notify all other clients about the new user.
	broadcastUserJoined(room, client)

	// Start reading datagrams in a goroutine.
	go readDatagrams(ctx, sess, room, client.ID)

	// Read further control messages until disconnect.
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
			client.SendControl(ControlMsg{Type: "pong", Ts: msg.Ts})
		}
	}
}

// readDatagrams reads voice datagrams from a client and broadcasts them.
func readDatagrams(ctx context.Context, sess *webtransport.Session, room *Room, senderID uint16) {
	for {
		data, err := sess.ReceiveDatagram(ctx)
		if err != nil {
			return
		}
		if len(data) < 4 {
			continue // Too small: need at least userID(2) + seq(2)
		}
		// Overwrite the sender ID in the datagram header to prevent spoofing.
		binary.BigEndian.PutUint16(data[:2], senderID)
		room.Broadcast(senderID, data)
	}
}

// sendUserList sends the current user list to a client.
func sendUserList(c *Client, room *Room) {
	clients := room.Clients()
	users := make([]UserInfo, len(clients))
	for i, cl := range clients {
		users[i] = UserInfo{ID: cl.ID, Username: cl.Username}
	}
	c.SendControl(ControlMsg{Type: "user_list", Users: users})
}

// broadcastUserJoined notifies all clients that a user joined.
func broadcastUserJoined(room *Room, newClient *Client) {
	msg := ControlMsg{Type: "user_joined", ID: newClient.ID, Username: newClient.Username}

	room.mu.RLock()
	defer room.mu.RUnlock()

	for id, c := range room.clients {
		if id == newClient.ID {
			continue
		}
		c.SendControl(msg)
	}
}

// broadcastUserLeft notifies all clients that a user left.
func broadcastUserLeft(room *Room, leftID uint16) {
	msg := ControlMsg{Type: "user_left", ID: leftID}

	room.mu.RLock()
	defer room.mu.RUnlock()

	for _, c := range room.clients {
		c.SendControl(msg)
	}
}
