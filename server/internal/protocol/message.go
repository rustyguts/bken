package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Message types used by the websocket protocol.
const (
	TypeHello                 = "hello"
	TypeSnapshot              = "snapshot"
	TypeUserJoined            = "user_joined"
	TypeUserLeft              = "user_left"
	TypeUserState             = "user_state"
	TypeConnectServer         = "connect_server"
	TypeDisconnectServer      = "disconnect_server"
	TypeJoinVoice             = "join_voice"
	TypeDisconnectVoice       = "DisconnectVoice"
	TypeDisconnectVoiceLegacy = "disconnect_voice"
	TypeSendText              = "send_text"
	TypeTextMessage           = "text_message"
	TypePing                  = "ping"
	TypePong                  = "pong"
	TypeError                 = "error"
	TypeCreateChannel         = "create_channel"
	TypeRenameChannel         = "rename_channel"
	TypeDeleteChannel         = "delete_channel"
	TypeChannelList           = "channel_list"
	TypeGetChannels           = "get_channels"
	TypeGetMessages           = "get_messages"
	TypeMessageHistory        = "message_history"
	TypeGetServerInfo         = "get_server_info"
	TypeServerInfo            = "server_info"
)

// Message is the JSON control envelope exchanged over websocket.
type Message struct {
	Type       string        `json:"type"`
	SelfID     string        `json:"self_id,omitempty"`
	Username   string        `json:"username,omitempty"`
	ServerID   string        `json:"server_id,omitempty"`
	ChannelID  string        `json:"channel_id,omitempty"`
	Message    string        `json:"message,omitempty"`
	TS         int64         `json:"ts,omitempty"`
	MsgID      int64         `json:"msg_id,omitempty"`
	Error      string        `json:"error,omitempty"`
	ServerName string        `json:"server_name,omitempty"`
	User       *User         `json:"user,omitempty"`
	Users      []User        `json:"users,omitempty"`
	Channels   []Channel     `json:"channels,omitempty"`
	Messages   []TextMessage `json:"messages,omitempty"`
}

// TextMessage is a persisted chat message returned in history queries.
type TextMessage struct {
	MsgID     int64  `json:"msg_id"`
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	ChannelID string `json:"channel_id"`
	Message   string `json:"message"`
	TS        int64  `json:"ts"`
}

// UnmarshalJSON handles channel_id being either a JSON string or number.
// The client sends it as a number for channel CRUD and as a string for voice/text.
func (m *Message) UnmarshalJSON(data []byte) error {
	type Alias Message
	aux := &struct {
		ChannelID json.RawMessage `json:"channel_id,omitempty"`
		*Alias
	}{Alias: (*Alias)(m)}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if len(aux.ChannelID) > 0 {
		raw := strings.TrimSpace(string(aux.ChannelID))
		if raw == "null" {
			return nil
		}
		if len(raw) > 0 && raw[0] == '"' {
			var s string
			if err := json.Unmarshal(aux.ChannelID, &s); err != nil {
				return fmt.Errorf("channel_id: %w", err)
			}
			m.ChannelID = s
		} else {
			// Numeric — store the string representation.
			m.ChannelID = raw
		}
	}
	return nil
}

// Channel describes a named channel within a server.
type Channel struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	MaxUsers int    `json:"max_users,omitempty"`
}

// User is the authoritative presence payload for one user.
type User struct {
	ID               string      `json:"id"`
	Username         string      `json:"username"`
	ConnectedServers []string    `json:"connected_servers,omitempty"`
	Voice            *VoiceState `json:"voice,omitempty"`
}

// VoiceState is the global voice presence for a user.
// A user can only have at most one voice state at a time.
type VoiceState struct {
	ServerID  string `json:"server_id"`
	ChannelID string `json:"channel_id"`
}
