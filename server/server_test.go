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

	tlsConfig, _ := generateTLSConfig()
	room := NewRoom()

	port := getFreePort()
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	ctx, cancel := context.WithCancel(context.Background())
	srv := NewServer(addr, tlsConfig, room)

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

func TestServerControlMessages(t *testing.T) {
	addr, cancel := startTestServer(t)
	defer cancel()

	// Connect client 1.
	sess1, ctrl1 := dialTestClient(t, addr, "alice")
	defer sess1.CloseWithError(0, "test done")

	// Read user_list for client 1.
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
