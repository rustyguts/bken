package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/webtransport-go"
)

var testPort atomic.Int32

func init() {
	testPort.Store(14433)
}

func getFreePort() int {
	// Find a free UDP port.
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		return int(testPort.Add(1))
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return int(testPort.Add(1))
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	conn.Close()
	return port
}

func startTestServer(t *testing.T) (string, context.CancelFunc) {
	t.Helper()

	tlsConfig, _ := generateTLSConfig(24 * time.Hour)
	room := NewRoom()

	port := getFreePort()
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	ctx, cancel := context.WithCancel(context.Background())
	srv := NewServer(addr, tlsConfig, room, 30*time.Second)

	go func() {
		srv.Run(ctx)
	}()

	// Give the server time to start.
	time.Sleep(300 * time.Millisecond)

	return addr, cancel
}

func dialTestClient(t *testing.T, addr, username string) (*webtransport.Session, *webtransport.Stream) {
	t.Helper()

	d := webtransport.Dialer{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		QUICConfig: &quic.Config{
			EnableDatagrams:                  true,
			EnableStreamResetPartialDelivery: true,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, sess, err := d.Dial(ctx, "https://"+addr, http.Header{})
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}

	// Open control stream and send join.
	stream, err := sess.OpenStream()
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}

	joinMsg := ControlMsg{Type: "join", Username: username}
	data, _ := json.Marshal(joinMsg)
	data = append(data, '\n')
	if _, err := stream.Write(data); err != nil {
		t.Fatalf("write join: %v", err)
	}

	return sess, stream
}

func TestServerTwoClientsExchangeDatagrams(t *testing.T) {
	addr, cancel := startTestServer(t)
	defer cancel()

	// Connect two clients.
	sess1, ctrl1 := dialTestClient(t, addr, "alice")
	defer sess1.CloseWithError(0, "test done")

	sess2, ctrl2 := dialTestClient(t, addr, "bob")
	defer sess2.CloseWithError(0, "test done")

	// Drain control messages in background.
	go func() {
		scanner := bufio.NewScanner(ctrl1)
		for scanner.Scan() {
		}
	}()
	go func() {
		scanner := bufio.NewScanner(ctrl2)
		for scanner.Scan() {
		}
	}()

	// Wait for both clients to be fully registered.
	time.Sleep(200 * time.Millisecond)

	// Both clients must join the same non-zero channel for voice to route.
	joinCh := ControlMsg{Type: "join_channel", ChannelID: 1}
	joinData, _ := json.Marshal(joinCh)
	joinData = append(joinData, '\n')
	if _, err := ctrl1.Write(joinData); err != nil {
		t.Fatalf("write join_channel for client1: %v", err)
	}
	if _, err := ctrl2.Write(joinData); err != nil {
		t.Fatalf("write join_channel for client2: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Client 1 sends a datagram.
	payload := []byte("test-opus-data")
	dgram := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint16(dgram[0:2], 1) // userID (overwritten by server)
	binary.BigEndian.PutUint16(dgram[2:4], 1) // seq
	copy(dgram[4:], payload)

	if err := sess1.SendDatagram(dgram); err != nil {
		t.Fatalf("send datagram: %v", err)
	}

	// Client 2 should receive it.
	ctx, rcvCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer rcvCancel()

	received, err := sess2.ReceiveDatagram(ctx)
	if err != nil {
		t.Fatalf("receive datagram: %v", err)
	}

	if len(received) < 4 {
		t.Fatalf("datagram too short: %d bytes", len(received))
	}

	// Verify the payload is intact.
	receivedPayload := received[4:]
	if string(receivedPayload) != string(payload) {
		t.Errorf("payload mismatch: got %q, want %q", receivedPayload, payload)
	}
}

func TestPingPong(t *testing.T) {
	addr, cancel := startTestServer(t)
	defer cancel()

	sess, ctrl := dialTestClient(t, addr, "pinger")
	defer sess.CloseWithError(0, "test done")

	reader := bufio.NewReader(ctrl)

	// Drain the initial user_list and channel_list.
	if _, err := reader.ReadBytes('\n'); err != nil {
		t.Fatalf("read user_list: %v", err)
	}
	if _, err := reader.ReadBytes('\n'); err != nil {
		t.Fatalf("read channel_list: %v", err)
	}

	// Send a ping with a known timestamp.
	pingTs := time.Now().UnixMilli()
	pingMsg := ControlMsg{Type: "ping", Timestamp: pingTs}
	data, _ := json.Marshal(pingMsg)
	data = append(data, '\n')
	if _, err := ctrl.Write(data); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	// Expect a pong echoing the timestamp.
	ctx, rcvCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer rcvCancel()

	pongCh := make(chan ControlMsg, 1)
	go func() {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var msg ControlMsg
		if json.Unmarshal(line, &msg) == nil {
			pongCh <- msg
		}
	}()

	select {
	case <-ctx.Done():
		t.Fatal("timed out waiting for pong")
	case msg := <-pongCh:
		if msg.Type != "pong" {
			t.Errorf("expected pong, got %q", msg.Type)
		}
		if msg.Timestamp != pingTs {
			t.Errorf("pong timestamp mismatch: got %d, want %d", msg.Timestamp, pingTs)
		}
	}
}

func TestMaxDatagramSizeConstant(t *testing.T) {
	// Verify the constant is sensible: header(4) + max Opus(1275) = 1279.
	if MaxDatagramSize != 1279 {
		t.Errorf("MaxDatagramSize: got %d, want 1279", MaxDatagramSize)
	}
	if MaxDatagramSize != DatagramHeader+1275 {
		t.Errorf("MaxDatagramSize should equal DatagramHeader + 1275, got %d", MaxDatagramSize)
	}
}

func TestServerTooSmallDatagramDropped(t *testing.T) {
	addr, cancel := startTestServer(t)
	defer cancel()

	sess1, ctrl1 := dialTestClient(t, addr, "alice")
	defer sess1.CloseWithError(0, "test done")

	sess2, ctrl2 := dialTestClient(t, addr, "bob")
	defer sess2.CloseWithError(0, "test done")

	// Drain control messages.
	go func() {
		scanner := bufio.NewScanner(ctrl1)
		for scanner.Scan() {
		}
	}()
	go func() {
		scanner := bufio.NewScanner(ctrl2)
		for scanner.Scan() {
		}
	}()

	time.Sleep(200 * time.Millisecond)

	// Both join the same channel.
	joinCh := ControlMsg{Type: "join_channel", ChannelID: 1}
	joinData, _ := json.Marshal(joinCh)
	joinData = append(joinData, '\n')
	ctrl1.Write(joinData)
	ctrl2.Write(joinData)
	time.Sleep(100 * time.Millisecond)

	// Send a too-short datagram (< DatagramHeader bytes).
	if err := sess1.SendDatagram([]byte{0x01, 0x02}); err != nil {
		t.Fatalf("send short datagram: %v", err)
	}

	// Then send a valid datagram.
	payload := []byte("valid-data")
	dgram := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint16(dgram[0:2], 1)
	binary.BigEndian.PutUint16(dgram[2:4], 2)
	copy(dgram[4:], payload)
	if err := sess1.SendDatagram(dgram); err != nil {
		t.Fatalf("send valid datagram: %v", err)
	}

	// Client 2 should only receive the valid datagram.
	ctx, rcvCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer rcvCancel()

	received, err := sess2.ReceiveDatagram(ctx)
	if err != nil {
		t.Fatalf("receive datagram: %v", err)
	}
	if len(received) < 4 {
		t.Fatalf("datagram too short: %d bytes", len(received))
	}
	receivedPayload := received[4:]
	if string(receivedPayload) != string(payload) {
		t.Errorf("payload: got %q, want %q", receivedPayload, payload)
	}
}

func TestServerControlMessages(t *testing.T) {
	addr, cancel := startTestServer(t)
	defer cancel()

	// Connect client 1.
	sess1, ctrl1 := dialTestClient(t, addr, "alice")
	defer sess1.CloseWithError(0, "test done")

	// Read user_list and channel_list for client 1.
	reader1 := bufio.NewReader(ctrl1)
	line, err := reader1.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read user_list: %v", err)
	}

	var msg ControlMsg
	if err := json.Unmarshal(line, &msg); err != nil {
		t.Fatalf("unmarshal user_list: %v", err)
	}
	if msg.Type != "user_list" {
		t.Errorf("expected user_list, got %s", msg.Type)
	}

	if _, err := reader1.ReadBytes('\n'); err != nil {
		t.Fatalf("read channel_list: %v", err)
	}

	// Connect client 2 - client 1 should get user_joined.
	sess2, _ := dialTestClient(t, addr, "bob")
	defer sess2.CloseWithError(0, "test done")

	line, err = reader1.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read user_joined: %v", err)
	}

	if err := json.Unmarshal(line, &msg); err != nil {
		t.Fatalf("unmarshal user_joined: %v", err)
	}
	if msg.Type != "user_joined" {
		t.Errorf("expected user_joined, got %s", msg.Type)
	}
	if msg.Username != "bob" {
		t.Errorf("expected username bob, got %s", msg.Username)
	}
}
