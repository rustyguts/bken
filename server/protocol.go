package main

// ControlMsg is a JSON control message sent over the reliable bidirectional stream.
type ControlMsg struct {
	Type      string     `json:"type"`
	Username  string     `json:"username,omitempty"`
	ID        uint16     `json:"id,omitempty"`
	Users     []UserInfo `json:"users,omitempty"`
	Timestamp int64      `json:"ts,omitempty"` // ping/pong Unix ms
}

// UserInfo is a brief snapshot of a connected user, used in user_list messages.
type UserInfo struct {
	ID       uint16 `json:"id"`
	Username string `json:"username"`
}
