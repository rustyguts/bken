package main

// ControlMsg is a JSON control message sent over the reliable bidirectional stream.
type ControlMsg struct {
	Type       string        `json:"type"`
	Username   string        `json:"username,omitempty"`
	ID         uint16        `json:"id,omitempty"`
	Users      []UserInfo    `json:"users,omitempty"`
	Timestamp  int64         `json:"ts,omitempty"`           // ping/pong Unix ms
	Message    string        `json:"message,omitempty"`      // chat: body text (max 500 chars)
	ServerName string        `json:"server_name,omitempty"`  // user_list: human-readable server name
	OwnerID    uint16        `json:"owner_id,omitempty"`     // user_list/owner_changed: current room owner
	ChannelID  int64         `json:"channel_id,omitempty"`   // join_channel/user_channel: target channel
	Channels   []ChannelInfo `json:"channels,omitempty"`     // channel_list: full list of channels
}

// UserInfo is a brief snapshot of a connected user, used in user_list messages.
type UserInfo struct {
	ID        uint16 `json:"id"`
	Username  string `json:"username"`
	ChannelID int64  `json:"channel_id,omitempty"` // 0 = not in any channel
}

// ChannelInfo is a brief snapshot of a channel, used in channel_list messages.
type ChannelInfo struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
