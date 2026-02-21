package protocol

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
)

// Message is the JSON control envelope exchanged over websocket.
type Message struct {
	Type      string `json:"type"`
	SelfID    string `json:"self_id,omitempty"`
	Username  string `json:"username,omitempty"`
	ServerID  string `json:"server_id,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
	Message   string `json:"message,omitempty"`
	TS        int64  `json:"ts,omitempty"`
	Error     string `json:"error,omitempty"`
	User      *User  `json:"user,omitempty"`
	Users     []User `json:"users,omitempty"`
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
