package ws

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bken/server/internal/core"
	"bken/server/internal/protocol"
	"bken/server/internal/store"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

const writeTimeout = 5 * time.Second

// Handler owns websocket transport for the backend.
type Handler struct {
	channelState *core.ChannelState
	store        *store.Store
	upgrader     websocket.Upgrader
}

// NewHandler creates a websocket handler bound to channelState.
func NewHandler(channelState *core.ChannelState, st *store.Store) *Handler {
	return &Handler{
		channelState: channelState,
		store:        st,
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
	remoteAddr := c.RealIP()
	slog.Debug("ws upgrade request", "remote", remoteAddr)

	conn, err := h.upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		slog.Error("ws upgrade failed", "remote", remoteAddr, "err", err)
		return fmt.Errorf("upgrade websocket: %w", err)
	}
	h.serveConn(conn, remoteAddr)
	return nil
}

func (h *Handler) serveConn(conn *websocket.Conn, remoteAddr string) {
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Time{})
	conn.SetReadLimit(1 << 20)

	var hello protocol.Message
	if err := conn.ReadJSON(&hello); err != nil {
		slog.Debug("ws read hello failed", "remote", remoteAddr, "err", err)
		return
	}
	if hello.Type != protocol.TypeHello {
		slog.Debug("ws bad first message", "remote", remoteAddr, "type", hello.Type)
		h.writeDirectError(conn, "first message must be hello")
		return
	}

	slog.Debug("ws hello received", "remote", remoteAddr, "username", hello.Username)

	session, snapshot, err := h.channelState.Add(hello.Username, 64)
	if err != nil {
		slog.Warn("ws session rejected", "remote", remoteAddr, "username", hello.Username, "err", err)
		h.writeDirectError(conn, err.Error())
		return
	}

	slog.Info("ws connected", "user_id", session.UserID, "username", hello.Username, "remote", remoteAddr)

	defer func() {
		if removed, ok := h.channelState.Remove(session.UserID); ok {
			slog.Info("ws disconnected", "user_id", session.UserID, "username", removed.Username, "remote", remoteAddr)
			h.channelState.Broadcast(protocol.Message{Type: protocol.TypeUserLeft, User: &removed}, session.UserID)
		}
	}()

	go func() {
		for out := range session.Send {
			_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := conn.WriteJSON(out); err != nil {
				slog.Debug("ws write error", "user_id", session.UserID, "type", out.Type, "err", err)
				return
			}
		}
		slog.Debug("ws send channel closed", "user_id", session.UserID)
	}()

	h.channelState.SendTo(session.UserID, protocol.Message{
		Type:   protocol.TypeSnapshot,
		SelfID: session.UserID,
		Users:  snapshot,
	})
	slog.Debug("ws snapshot sent", "user_id", session.UserID, "user_count", len(snapshot))

	if joined, ok := h.channelState.User(session.UserID); ok {
		h.channelState.Broadcast(protocol.Message{Type: protocol.TypeUserJoined, User: &joined}, session.UserID)
	}

	for {
		var in protocol.Message
		if err := conn.ReadJSON(&in); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Debug("ws unexpected close", "user_id", session.UserID, "err", err)
			}
			return
		}
		slog.Debug("ws recv", "user_id", session.UserID, "type", in.Type, "server_id", in.ServerID, "channel_id", in.ChannelID)
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
			slog.Debug("connect_server error", "user_id", userID, "server_id", in.ServerID, "err", err)
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
			slog.Debug("disconnect_server error", "user_id", userID, "server_id", in.ServerID, "err", err)
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
			slog.Debug("join_voice error", "user_id", userID, "server_id", in.ServerID, "channel_id", in.ChannelID, "err", err)
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
		if strings.TrimSpace(in.Message) == "" && strings.TrimSpace(in.FileID) == "" {
			h.sendError(userID, "message or file is required")
			return
		}
		if !h.channelState.CanSendText(userID, in.ServerID) {
			slog.Debug("send_text denied", "user_id", userID, "server_id", in.ServerID)
			h.sendError(userID, "user is not connected to server")
			return
		}
		user, ok := h.channelState.User(userID)
		if !ok {
			h.sendError(userID, "user not found")
			return
		}
		ts := time.Now().UnixMilli()
		var msgID int64
		if h.store != nil {
			id, err := h.store.InsertMessage(context.Background(), in.ServerID, in.ChannelID, userID, user.Username, in.Message, ts, in.FileID, in.FileName, in.FileSize)
			if err != nil {
				slog.Error("persist message", "user_id", userID, "err", err)
			} else {
				msgID = id
			}
		}
		slog.Debug("send_text", "user_id", userID, "server_id", in.ServerID, "channel_id", in.ChannelID, "msg_id", msgID, "len", len(in.Message))
		h.channelState.BroadcastToServer(in.ServerID, protocol.Message{
			Type:      protocol.TypeTextMessage,
			ServerID:  in.ServerID,
			ChannelID: in.ChannelID,
			Message:   in.Message,
			MsgID:     msgID,
			TS:        ts,
			User:      &user,
			FileID:    in.FileID,
			FileName:  in.FileName,
			FileSize:  in.FileSize,
		}, "")

	case protocol.TypeCreateChannel:
		if strings.TrimSpace(in.Message) == "" {
			h.sendError(userID, "channel name is required")
			return
		}
		serverID, err := h.channelState.UserServer(userID)
		if err != nil {
			h.sendError(userID, err.Error())
			return
		}
		channels, err := h.channelState.CreateChannel(serverID, in.Message)
		if err != nil {
			h.sendError(userID, err.Error())
			return
		}
		h.channelState.BroadcastToServer(serverID, protocol.Message{
			Type:     protocol.TypeChannelList,
			Channels: channels,
		}, "")

	case protocol.TypeRenameChannel:
		if strings.TrimSpace(in.ChannelID) == "" || strings.TrimSpace(in.Message) == "" {
			h.sendError(userID, "channel_id and name are required")
			return
		}
		serverID, err := h.channelState.UserServer(userID)
		if err != nil {
			h.sendError(userID, err.Error())
			return
		}
		chID, err := parseChannelID(in.ChannelID)
		if err != nil {
			h.sendError(userID, err.Error())
			return
		}
		channels, err := h.channelState.RenameChannel(serverID, chID, in.Message)
		if err != nil {
			h.sendError(userID, err.Error())
			return
		}
		h.channelState.BroadcastToServer(serverID, protocol.Message{
			Type:     protocol.TypeChannelList,
			Channels: channels,
		}, "")

	case protocol.TypeDeleteChannel:
		if strings.TrimSpace(in.ChannelID) == "" {
			h.sendError(userID, "channel_id is required")
			return
		}
		serverID, err := h.channelState.UserServer(userID)
		if err != nil {
			h.sendError(userID, err.Error())
			return
		}
		chID, err := parseChannelID(in.ChannelID)
		if err != nil {
			h.sendError(userID, err.Error())
			return
		}
		channels, err := h.channelState.DeleteChannel(serverID, chID)
		if err != nil {
			h.sendError(userID, err.Error())
			return
		}
		h.channelState.BroadcastToServer(serverID, protocol.Message{
			Type:     protocol.TypeChannelList,
			Channels: channels,
		}, "")

	case protocol.TypeGetChannels:
		serverID, err := h.channelState.UserServer(userID)
		if err != nil {
			h.sendError(userID, err.Error())
			return
		}
		channels := h.channelState.Channels(serverID)
		slog.Debug("get_channels", "user_id", userID, "server_id", serverID, "count", len(channels))
		h.channelState.SendTo(userID, protocol.Message{
			Type:     protocol.TypeChannelList,
			Channels: channels,
		})

	case protocol.TypeAddReaction:
		if in.MsgID <= 0 || strings.TrimSpace(in.Emoji) == "" {
			h.sendError(userID, "msg_id and emoji are required")
			return
		}
		serverID, err := h.channelState.UserServer(userID)
		if err != nil {
			h.sendError(userID, err.Error())
			return
		}
		if h.store != nil {
			if err := h.store.AddReaction(context.Background(), in.MsgID, userID, in.Emoji); err != nil {
				slog.Error("add reaction", "user_id", userID, "msg_id", in.MsgID, "err", err)
			}
		}
		h.channelState.BroadcastToServer(serverID, protocol.Message{
			Type:   protocol.TypeReactionAdded,
			MsgID:  in.MsgID,
			Emoji:  in.Emoji,
			UserID: userID,
		}, "")

	case protocol.TypeRemoveReaction:
		if in.MsgID <= 0 || strings.TrimSpace(in.Emoji) == "" {
			h.sendError(userID, "msg_id and emoji are required")
			return
		}
		serverID, err := h.channelState.UserServer(userID)
		if err != nil {
			h.sendError(userID, err.Error())
			return
		}
		if h.store != nil {
			if err := h.store.RemoveReaction(context.Background(), in.MsgID, userID, in.Emoji); err != nil {
				slog.Error("remove reaction", "user_id", userID, "msg_id", in.MsgID, "err", err)
			}
		}
		h.channelState.BroadcastToServer(serverID, protocol.Message{
			Type:   protocol.TypeReactionRemoved,
			MsgID:  in.MsgID,
			Emoji:  in.Emoji,
			UserID: userID,
		}, "")

	case protocol.TypeGetMessages:
		if h.store == nil {
			h.sendError(userID, "message history not available")
			return
		}
		if strings.TrimSpace(in.ChannelID) == "" {
			h.sendError(userID, "channel_id is required")
			return
		}
		serverID, err := h.channelState.UserServer(userID)
		if err != nil {
			h.sendError(userID, err.Error())
			return
		}
		rows, err := h.store.GetMessages(context.Background(), serverID, in.ChannelID, 50)
		if err != nil {
			h.sendError(userID, "failed to load messages")
			slog.Error("get messages", "user_id", userID, "server_id", serverID, "channel_id", in.ChannelID, "err", err)
			return
		}
		msgs := make([]protocol.TextMessage, len(rows))
		msgIDs := make([]int64, len(rows))
		for i, r := range rows {
			msgs[i] = protocol.TextMessage{
				MsgID:     r.ID,
				UserID:    r.UserID,
				Username:  r.Username,
				ChannelID: r.ChannelID,
				Message:   r.Message,
				TS:        r.TS,
				FileID:    r.FileID,
				FileName:  r.FileName,
				FileSize:  r.FileSize,
			}
			msgIDs[i] = r.ID
		}
		// Attach reactions to messages.
		if len(msgIDs) > 0 {
			reactionMap, err := h.store.GetReactionsForMessages(context.Background(), msgIDs)
			if err != nil {
				slog.Error("get reactions for messages", "err", err)
			} else {
				for i := range msgs {
					rxRows := reactionMap[msgs[i].MsgID]
					if len(rxRows) == 0 {
						continue
					}
					// Group by emoji.
					emojiMap := make(map[string][]string)
					var order []string
					for _, rx := range rxRows {
						if _, seen := emojiMap[rx.Emoji]; !seen {
							order = append(order, rx.Emoji)
						}
						emojiMap[rx.Emoji] = append(emojiMap[rx.Emoji], rx.UserID)
					}
					for _, emoji := range order {
						uids := emojiMap[emoji]
						msgs[i].Reactions = append(msgs[i].Reactions, protocol.ReactionInfo{
							Emoji:   emoji,
							UserIDs: uids,
							Count:   len(uids),
						})
					}
				}
			}
		}
		slog.Debug("get_messages", "user_id", userID, "server_id", serverID, "channel_id", in.ChannelID, "count", len(msgs))
		h.channelState.SendTo(userID, protocol.Message{
			Type:      protocol.TypeMessageHistory,
			ChannelID: in.ChannelID,
			Messages:  msgs,
		})

	case protocol.TypeSetVoiceState:
		muted := in.Muted != nil && *in.Muted
		deafened := in.Deafened != nil && *in.Deafened
		user, changed := h.channelState.SetVoiceFlags(userID, muted, deafened)
		if changed {
			h.channelState.SendTo(userID, protocol.Message{Type: protocol.TypeUserState, User: &user})
			if user.Voice != nil {
				h.channelState.BroadcastToServer(user.Voice.ServerID, protocol.Message{Type: protocol.TypeUserState, User: &user}, userID)
			}
		}

	case protocol.TypeGetServerInfo:
		slog.Debug("get_server_info", "user_id", userID)
		h.channelState.SendTo(userID, protocol.Message{
			Type:       protocol.TypeServerInfo,
			ServerName: h.channelState.ServerName(),
		})

	default:
		slog.Warn("ws unknown message type", "user_id", userID, "type", in.Type)
		h.sendError(userID, "unsupported message type")
	}
}

func (h *Handler) sendError(userID, errMsg string) {
	slog.Debug("ws sending error", "user_id", userID, "error", errMsg)
	h.channelState.SendTo(userID, protocol.Message{Type: protocol.TypeError, Error: errMsg})
}

func parseChannelID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid channel_id: %s", s)
	}
	return id, nil
}

func (h *Handler) writeDirectError(conn *websocket.Conn, errMsg string) {
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	_ = conn.WriteJSON(protocol.Message{Type: protocol.TypeError, Error: errMsg})
}
