package ws

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"bken/server/internal/core"
	"bken/server/internal/protocol"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

const writeTimeout = 5 * time.Second

// Handler owns websocket transport for the backend.
type Handler struct {
	channelState *core.ChannelState
	upgrader     websocket.Upgrader
}

// NewHandler creates a websocket handler bound to channelState.
func NewHandler(channelState *core.ChannelState) *Handler {
	return &Handler{
		channelState: channelState,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
	}
}

// Register binds websocket routes on an Echo router.
func (h *Handler) Register(e *echo.Echo) {
	e.GET("/ws", h.HandleWebSocket)
}

// HandleWebSocket upgrades one request and serves it until disconnect.
func (h *Handler) HandleWebSocket(c echo.Context) error {
	conn, err := h.upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return fmt.Errorf("upgrade websocket: %w", err)
	}
	h.serveConn(conn)
	return nil
}

func (h *Handler) serveConn(conn *websocket.Conn) {
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Time{})
	conn.SetReadLimit(1 << 20)

	var hello protocol.Message
	if err := conn.ReadJSON(&hello); err != nil {
		return
	}
	if hello.Type != protocol.TypeHello {
		h.writeDirectError(conn, "first message must be hello")
		return
	}

	session, snapshot, err := h.channelState.Add(hello.Username, 64)
	if err != nil {
		h.writeDirectError(conn, err.Error())
		return
	}

	defer func() {
		if removed, ok := h.channelState.Remove(session.UserID); ok {
			h.channelState.Broadcast(protocol.Message{Type: protocol.TypeUserLeft, User: &removed}, session.UserID)
		}
	}()

	go func() {
		for out := range session.Send {
			_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := conn.WriteJSON(out); err != nil {
				return
			}
		}
	}()

	h.channelState.SendTo(session.UserID, protocol.Message{
		Type:   protocol.TypeSnapshot,
		SelfID: session.UserID,
		Users:  snapshot,
	})
	if joined, ok := h.channelState.User(session.UserID); ok {
		h.channelState.Broadcast(protocol.Message{Type: protocol.TypeUserJoined, User: &joined}, session.UserID)
	}

	for {
		var in protocol.Message
		if err := conn.ReadJSON(&in); err != nil {
			return
		}
		h.handleInbound(session.UserID, in)
	}
}

func (h *Handler) handleInbound(userID string, in protocol.Message) {
	switch in.Type {
	case protocol.TypePing:
		h.channelState.SendTo(userID, protocol.Message{Type: protocol.TypePong, TS: in.TS})

	case protocol.TypeConnectServer:
		user, changed, err := h.channelState.ConnectServer(userID, in.ServerID)
		if err != nil {
			h.sendError(userID, err.Error())
			return
		}
		h.channelState.SendTo(userID, protocol.Message{Type: protocol.TypeUserState, User: &user})
		if changed {
			h.channelState.BroadcastToServer(in.ServerID, protocol.Message{Type: protocol.TypeUserState, User: &user}, userID)
		}

	case protocol.TypeDisconnectServer:
		user, changed, _, err := h.channelState.DisconnectServer(userID, in.ServerID)
		if err != nil {
			h.sendError(userID, err.Error())
			return
		}
		h.channelState.SendTo(userID, protocol.Message{Type: protocol.TypeUserState, User: &user})
		if changed {
			h.channelState.BroadcastToServer(in.ServerID, protocol.Message{Type: protocol.TypeUserState, User: &user}, userID)
		}

	case protocol.TypeJoinVoice:
		user, oldVoice, err := h.channelState.JoinVoice(userID, in.ServerID, in.ChannelID)
		if err != nil {
			h.sendError(userID, err.Error())
			return
		}
		h.channelState.SendTo(userID, protocol.Message{Type: protocol.TypeUserState, User: &user})
		if oldVoice != nil && oldVoice.ServerID != in.ServerID {
			h.channelState.BroadcastToServer(oldVoice.ServerID, protocol.Message{Type: protocol.TypeUserState, User: &user}, userID)
		}
		h.channelState.BroadcastToServer(in.ServerID, protocol.Message{Type: protocol.TypeUserState, User: &user}, userID)

	case protocol.TypeDisconnectVoice, protocol.TypeDisconnectVoiceLegacy:
		user, oldVoice, _ := h.channelState.DisconnectVoice(userID)
		h.channelState.SendTo(userID, protocol.Message{Type: protocol.TypeUserState, User: &user})
		if oldVoice != nil {
			h.channelState.BroadcastToServer(oldVoice.ServerID, protocol.Message{Type: protocol.TypeUserState, User: &user}, userID)
		}

	case protocol.TypeSendText:
		if strings.TrimSpace(in.ServerID) == "" || strings.TrimSpace(in.ChannelID) == "" {
			h.sendError(userID, "server_id and channel_id are required")
			return
		}
		if strings.TrimSpace(in.Message) == "" {
			h.sendError(userID, "message is required")
			return
		}
		if !h.channelState.CanSendText(userID, in.ServerID) {
			h.sendError(userID, "user is not connected to server")
			return
		}
		user, ok := h.channelState.User(userID)
		if !ok {
			h.sendError(userID, "user not found")
			return
		}
		h.channelState.BroadcastToServer(in.ServerID, protocol.Message{
			Type:      protocol.TypeTextMessage,
			ServerID:  in.ServerID,
			ChannelID: in.ChannelID,
			Message:   in.Message,
			TS:        time.Now().UnixMilli(),
			User:      &user,
		}, "")

	default:
		h.sendError(userID, "unsupported message type")
	}
}

func (h *Handler) sendError(userID, errMsg string) {
	h.channelState.SendTo(userID, protocol.Message{Type: protocol.TypeError, Error: errMsg})
}

func (h *Handler) writeDirectError(conn *websocket.Conn, errMsg string) {
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	_ = conn.WriteJSON(protocol.Message{Type: protocol.TypeError, Error: errMsg})
}
