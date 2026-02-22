package ws

import (
	"errors"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bken/server/internal/core"
	"bken/server/internal/protocol"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

func TestDisconnectVoiceBroadcastClearsAvatar(t *testing.T) {
	_, baseURL := startTestServer(t)

	alice, aliceSnap := connectClient(t, baseURL, "alice")
	defer alice.Close()
	_ = aliceSnap

	bob, bobSnap := connectClient(t, baseURL, "bob")
	defer bob.Close()

	aliceID := findUserID(t, bobSnap.Users, "alice")

	writeMsg(t, alice, protocol.Message{Type: protocol.TypeConnectServer, ServerID: "srv-1"})
	readUntil(t, alice, func(m protocol.Message) bool {
		return m.Type == protocol.TypeUserState && m.User != nil && m.User.ID == aliceID && hasServer(m.User, "srv-1")
	})

	bobID := findUserID(t, bobSnap.Users, "bob")
	writeMsg(t, bob, protocol.Message{Type: protocol.TypeConnectServer, ServerID: "srv-1"})
	readUntil(t, bob, func(m protocol.Message) bool {
		return m.Type == protocol.TypeUserState && m.User != nil && m.User.ID == bobID && hasServer(m.User, "srv-1")
	})

	writeMsg(t, alice, protocol.Message{
		Type:      protocol.TypeJoinVoice,
		ServerID:  "srv-1",
		ChannelID: "chan-a",
	})
	readUntil(t, bob, func(m protocol.Message) bool {
		return m.Type == protocol.TypeUserState &&
			m.User != nil &&
			m.User.ID == aliceID &&
			m.User.Voice != nil &&
			m.User.Voice.ServerID == "srv-1" &&
			m.User.Voice.ChannelID == "chan-a"
	})

	writeMsg(t, alice, protocol.Message{Type: protocol.TypeDisconnectVoice})
	readUntil(t, bob, func(m protocol.Message) bool {
		return m.Type == protocol.TypeUserState && m.User != nil && m.User.ID == aliceID && m.User.Voice == nil
	})
}

func TestCreateChannelBroadcastsChannelList(t *testing.T) {
	_, baseURL := startTestServer(t)

	alice, _ := connectClient(t, baseURL, "alice")
	defer alice.Close()
	bob, _ := connectClient(t, baseURL, "bob")
	defer bob.Close()

	// Both connect to the same server.
	writeMsg(t, alice, protocol.Message{Type: protocol.TypeConnectServer, ServerID: "srv-1"})
	readUntil(t, alice, func(m protocol.Message) bool {
		return m.Type == protocol.TypeUserState
	})
	writeMsg(t, bob, protocol.Message{Type: protocol.TypeConnectServer, ServerID: "srv-1"})
	readUntil(t, bob, func(m protocol.Message) bool {
		return m.Type == protocol.TypeUserState
	})

	// Alice creates a channel.
	writeMsg(t, alice, protocol.Message{Type: protocol.TypeCreateChannel, Message: "general"})

	// Both should receive the channel_list.
	aliceList := readUntil(t, alice, func(m protocol.Message) bool {
		return m.Type == protocol.TypeChannelList
	})
	bobList := readUntil(t, bob, func(m protocol.Message) bool {
		return m.Type == protocol.TypeChannelList
	})

	// ConnectServer seeds a default "General" channel, so after creating
	// "general" there should be 2 channels total.
	if len(aliceList.Channels) != 2 || aliceList.Channels[1].Name != "general" {
		t.Fatalf("alice channels: %#v", aliceList.Channels)
	}
	if len(bobList.Channels) != 2 || bobList.Channels[1].Name != "general" {
		t.Fatalf("bob channels: %#v", bobList.Channels)
	}
}

func TestCreateChannelRequiresServerConnection(t *testing.T) {
	_, baseURL := startTestServer(t)

	alice, _ := connectClient(t, baseURL, "alice")
	defer alice.Close()

	// Try to create a channel without connecting to any server.
	writeMsg(t, alice, protocol.Message{Type: protocol.TypeCreateChannel, Message: "general"})

	// Should receive an error.
	readUntil(t, alice, func(m protocol.Message) bool {
		return m.Type == protocol.TypeError && m.Error != ""
	})
}

func startTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	channelState := core.NewChannelState("")
	e := echo.New()
	NewHandler(channelState, nil).Register(e)
	httpServer := httptest.NewServer(e)
	t.Cleanup(httpServer.Close)

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	return httpServer, wsURL
}

func connectClient(t *testing.T, baseWSURL, username string) (*websocket.Conn, protocol.Message) {
	t.Helper()

	conn, _, err := websocket.DefaultDialer.Dial(baseWSURL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}

	writeMsg(t, conn, protocol.Message{Type: protocol.TypeHello, Username: username})
	snapshot := readUntil(t, conn, func(m protocol.Message) bool {
		return m.Type == protocol.TypeSnapshot && m.SelfID != ""
	})
	return conn, snapshot
}

func writeMsg(t *testing.T, conn *websocket.Conn, msg protocol.Message) {
	t.Helper()
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("write json: %v", err)
	}
}

func readUntil(t *testing.T, conn *websocket.Conn, match func(protocol.Message) bool) protocol.Message {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		var msg protocol.Message
		err := conn.ReadJSON(&msg)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				t.Fatalf("connection closed unexpectedly: %v", err)
			}
			t.Fatalf("read json: %v", err)
		}
		if match(msg) {
			return msg
		}
	}
	t.Fatal("timed out waiting for matching message")
	return protocol.Message{}
}

func findUserID(t *testing.T, users []protocol.User, username string) string {
	t.Helper()
	for _, u := range users {
		if u.Username == username {
			return u.ID
		}
	}
	t.Fatalf("user %q not found in snapshot", username)
	return ""
}

func hasServer(u *protocol.User, serverID string) bool {
	if u == nil {
		return false
	}
	for _, sid := range u.ConnectedServers {
		if sid == serverID {
			return true
		}
	}
	return false
}
