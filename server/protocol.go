package main

import (
	"fmt"
	"strings"
)

// Wire-protocol limits.
const (
	MaxNameLength  = 50  // max UTF-8 bytes for server names, channel names, and usernames
	MaxChatLength  = 500 // max bytes for a single chat message body
	DatagramHeader = 4   // senderID(2) + seq(2) bytes prepended to every voice datagram
)

// validateName trims whitespace from s and returns the trimmed string, or an
// error if the result is empty or exceeds maxLen bytes.
func validateName(s string, maxLen int) (string, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "", fmt.Errorf("name must not be empty")
	case len(s) > maxLen:
		return "", fmt.Errorf("name must not exceed %d characters", maxLen)
	}
	return s, nil
}

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
