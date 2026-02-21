package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func getFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func startTestServer(t *testing.T) (string, context.CancelFunc) {
	return startTestServerWithRoom(t, NewRoom())
}

func startTestServerWithRoom(t *testing.T, room *Room) (string, context.CancelFunc) {
	t.Helper()

	tlsConfig, _, err := generateTLSConfig(24*time.Hour, "")
	if err != nil {
		t.Fatalf("generateTLSConfig: %v", err)
	}

	port := getFreePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	ctx, cancel := context.WithCancel(context.Background())
	srv := NewServer(addr, tlsConfig, room, 30*time.Second)

	go func() {
		_ = srv.Run(ctx)
	}()

	// Give the server time to start.
	time.Sleep(300 * time.Millisecond)

	return addr, cancel
}

func dialTestClient(t *testing.T, addr, username string) *websocket.Conn {
	t.Helper()

	d := websocket.Dialer{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := d.DialContext(ctx, "wss://"+addr+"/ws", nil)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}

	writeControl(t, conn, ControlMsg{Type: "join", Username: username})
	return conn
}

func writeControl(t *testing.T, conn *websocket.Conn, msg ControlMsg) {
	t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal control: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write control: %v", err)
	}
}

func readControl(t *testing.T, conn *websocket.Conn, timeout time.Duration) ControlMsg {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read control: %v", err)
	}
	var msg ControlMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal control: %v", err)
	}
	return msg
}

func TestServerTwoClientsExchangeDatagrams(t *testing.T) {
	addr, cancel := startTestServer(t)
	defer cancel()

	alice := dialTestClient(t, addr, "alice")
	defer alice.Close()

	aliceWelcome := readControl(t, alice, 2*time.Second)
	if aliceWelcome.Type != "user_list" {
		t.Fatalf("expected user_list, got %q", aliceWelcome.Type)
	}
	aliceID := aliceWelcome.SelfID
	_ = readControl(t, alice, 2*time.Second) // channel_list

	bob := dialTestClient(t, addr, "bob")
	defer bob.Close()

	bobWelcome := readControl(t, bob, 2*time.Second)
	if bobWelcome.Type != "user_list" {
		t.Fatalf("expected bob user_list, got %q", bobWelcome.Type)
	}
	bobID := bobWelcome.SelfID
	_ = readControl(t, bob, 2*time.Second) // channel_list

	joined := readControl(t, alice, 2*time.Second)
	if joined.Type != "user_joined" {
		t.Fatalf("expected user_joined, got %q", joined.Type)
	}
	if joined.ID != bobID {
		t.Fatalf("user_joined id=%d want %d", joined.ID, bobID)
	}

	offerSDP := "v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n"
	writeControl(t, alice, ControlMsg{Type: "webrtc_offer", TargetID: bobID, SDP: offerSDP})

	relay := readControl(t, bob, 2*time.Second)
	if relay.Type != "webrtc_offer" {
		t.Fatalf("expected webrtc_offer relay, got %q", relay.Type)
	}
	if relay.ID != aliceID {
		t.Fatalf("relay sender id=%d want %d", relay.ID, aliceID)
	}
	if relay.SDP != offerSDP {
		t.Fatalf("relay sdp mismatch")
	}
}

func TestPingPong(t *testing.T) {
	addr, cancel := startTestServer(t)
	defer cancel()

	conn := dialTestClient(t, addr, "pinger")
	defer conn.Close()

	_ = readControl(t, conn, 2*time.Second) // user_list
	_ = readControl(t, conn, 2*time.Second) // channel_list

	pingTs := time.Now().UnixMilli()
	writeControl(t, conn, ControlMsg{Type: "ping", Timestamp: pingTs})

	pong := readControl(t, conn, 2*time.Second)
	if pong.Type != "pong" {
		t.Fatalf("expected pong, got %q", pong.Type)
	}
	if pong.Timestamp != pingTs {
		t.Fatalf("pong timestamp=%d want %d", pong.Timestamp, pingTs)
	}
}

func TestServerTooSmallDatagramDropped(t *testing.T) {
	addr, cancel := startTestServer(t)
	defer cancel()

	conn := dialTestClient(t, addr, "alice")
	defer conn.Close()

	_ = readControl(t, conn, 2*time.Second) // user_list
	_ = readControl(t, conn, 2*time.Second) // channel_list

	// Send malformed JSON and confirm the connection remains usable.
	if err := conn.WriteMessage(websocket.TextMessage, []byte("{malformed")); err != nil {
		t.Fatalf("write malformed message: %v", err)
	}

	pingTs := time.Now().UnixMilli()
	writeControl(t, conn, ControlMsg{Type: "ping", Timestamp: pingTs})
	pong := readControl(t, conn, 2*time.Second)
	if pong.Type != "pong" || pong.Timestamp != pingTs {
		t.Fatalf("expected pong after malformed control, got type=%q ts=%d", pong.Type, pong.Timestamp)
	}
}

func TestServerControlMessages(t *testing.T) {
	addr, cancel := startTestServer(t)
	defer cancel()

	alice := dialTestClient(t, addr, "alice")
	defer alice.Close()

	msg := readControl(t, alice, 2*time.Second)
	if msg.Type != "user_list" {
		t.Fatalf("expected user_list, got %q", msg.Type)
	}
	if msg.SelfID == 0 {
		t.Fatal("expected non-zero self_id")
	}

	msg = readControl(t, alice, 2*time.Second)
	if msg.Type != "channel_list" {
		t.Fatalf("expected channel_list, got %q", msg.Type)
	}

	bob := dialTestClient(t, addr, "bob")
	defer bob.Close()
	_ = readControl(t, bob, 2*time.Second) // bob user_list
	_ = readControl(t, bob, 2*time.Second) // bob channel_list

	msg = readControl(t, alice, 2*time.Second)
	if msg.Type != "user_joined" {
		t.Fatalf("expected user_joined, got %q", msg.Type)
	}
	if msg.Username != "bob" {
		t.Fatalf("expected username bob, got %q", msg.Username)
	}
}

func TestUserListIncludesICEServersWhenTURNConfigured(t *testing.T) {
	room := NewRoom()
	room.SetICEServers([]ICEServerInfo{
		{URLs: []string{"stun:stun.l.google.com:19302"}},
		{URLs: []string{"turn:turn.example.com:3478"}, Username: "user", Credential: "pass"},
	})

	addr, cancel := startTestServerWithRoom(t, room)
	defer cancel()

	alice := dialTestClient(t, addr, "alice")
	defer alice.Close()

	msg := readControl(t, alice, 2*time.Second)
	if msg.Type != "user_list" {
		t.Fatalf("expected user_list, got %q", msg.Type)
	}
	if len(msg.ICEServers) != 2 {
		t.Fatalf("expected 2 ICE servers, got %d", len(msg.ICEServers))
	}

	// Verify STUN server.
	if msg.ICEServers[0].URLs[0] != "stun:stun.l.google.com:19302" {
		t.Errorf("ICE[0] URL: got %q, want stun:stun.l.google.com:19302", msg.ICEServers[0].URLs[0])
	}

	// Verify TURN server with credentials.
	turn := msg.ICEServers[1]
	if turn.URLs[0] != "turn:turn.example.com:3478" {
		t.Errorf("ICE[1] URL: got %q, want turn:turn.example.com:3478", turn.URLs[0])
	}
	if turn.Username != "user" {
		t.Errorf("ICE[1] Username: got %q, want %q", turn.Username, "user")
	}
	if turn.Credential != "pass" {
		t.Errorf("ICE[1] Credential: got %q, want %q", turn.Credential, "pass")
	}
}

func TestUserListNoICEServersWhenNotConfigured(t *testing.T) {
	room := NewRoom()
	// Do not set ICE servers.

	addr, cancel := startTestServerWithRoom(t, room)
	defer cancel()

	alice := dialTestClient(t, addr, "alice")
	defer alice.Close()

	msg := readControl(t, alice, 2*time.Second)
	if msg.Type != "user_list" {
		t.Fatalf("expected user_list, got %q", msg.Type)
	}
	if len(msg.ICEServers) != 0 {
		t.Fatalf("expected 0 ICE servers when not configured, got %d", len(msg.ICEServers))
	}
}
