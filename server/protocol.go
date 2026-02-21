package main

import (
	"fmt"
	"strings"
)

// Wire-protocol limits.
const (
	MaxNameLength = 50               // max UTF-8 bytes for server names, channel names, and usernames
	MaxChatLength = 500              // max bytes for a single chat message body
	MaxFileSize   = 10 * 1024 * 1024 // max upload size (10 MB)
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

// ICEServerInfo describes a STUN or TURN server for WebRTC peer connections.
type ICEServerInfo struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// ControlMsg is a JSON control message sent over the reliable bidirectional stream.
type ControlMsg struct {
	Type          string          `json:"type"`
	Username      string          `json:"username,omitempty"`
	ID            uint16          `json:"id,omitempty"`
	SelfID        uint16          `json:"self_id,omitempty"`   // user_list: authoritative ID for the receiving client
	TargetID      uint16          `json:"target_id,omitempty"` // webrtc_*: intended remote peer
	Users         []UserInfo      `json:"users,omitempty"`
	Timestamp     int64           `json:"ts,omitempty"`              // ping/pong Unix ms
	Message       string          `json:"message,omitempty"`         // chat: body text (max 500 chars)
	ServerName    string          `json:"server_name,omitempty"`     // user_list: human-readable server name
	OwnerID       uint16          `json:"owner_id,omitempty"`        // user_list/owner_changed: current room owner
	ChannelID     int64           `json:"channel_id,omitempty"`      // join_channel/user_channel: target channel
	Channels      []ChannelInfo   `json:"channels,omitempty"`        // channel_list: full list of channels
	APIPort       int             `json:"api_port,omitempty"`        // user_list: HTTP API port for file uploads
	ICEServers    []ICEServerInfo `json:"ice_servers,omitempty"`     // user_list: ICE servers for WebRTC
	FileID        int64           `json:"file_id,omitempty"`         // chat: uploaded file DB id
	FileName      string          `json:"file_name,omitempty"`       // chat: original filename
	FileSize      int64           `json:"file_size,omitempty"`       // chat: file size in bytes
	MsgID         uint64          `json:"msg_id,omitempty"`          // chat/link_preview: server-assigned message ID
	LinkURL       string          `json:"link_url,omitempty"`        // link_preview: the URL that was fetched
	LinkTitle     string          `json:"link_title,omitempty"`      // link_preview: page title
	LinkDesc      string          `json:"link_desc,omitempty"`       // link_preview: page description
	LinkImage     string          `json:"link_image,omitempty"`      // link_preview: preview image URL
	LinkSite      string          `json:"link_site,omitempty"`       // link_preview: site name
	Mentions      []uint16        `json:"mentions,omitempty"`        // chat: user IDs mentioned via @DisplayName
	SDP           string          `json:"sdp,omitempty"`             // webrtc_offer/webrtc_answer: SDP payload
	Candidate     string          `json:"candidate,omitempty"`       // webrtc_ice: ICE candidate
	SDPMid        string          `json:"sdp_mid,omitempty"`         // webrtc_ice: SDP mid
	SDPMLineIndex *uint16         `json:"sdp_mline_index,omitempty"` // webrtc_ice: SDP m-line index
	VideoActive   *bool           `json:"video_active,omitempty"`    // video_state: whether user has video on
	ScreenShare   *bool           `json:"screen_share,omitempty"`    // video_state: whether this is a screen share
	Emoji         string          `json:"emoji,omitempty"`           // add_reaction/remove_reaction/reaction_added/reaction_removed: emoji character
	Reactions     []ReactionInfo  `json:"reactions,omitempty"`       // chat: reactions on a message
	ReplyTo       uint64          `json:"reply_to,omitempty"`        // chat: message ID being replied to
	ReplyPreview  *ReplyPreview   `json:"reply_preview,omitempty"`   // chat: preview of the replied-to message
	Query         string          `json:"query,omitempty"`           // search_messages: search query
	Results       []SearchResult  `json:"results,omitempty"`         // search_results: matching messages
	Before        uint64          `json:"before,omitempty"`          // search_messages: cursor for pagination
	Limit         int             `json:"limit,omitempty"`           // search_messages: max results
	Pinned        *bool           `json:"pinned,omitempty"`          // pin_message/message_pinned: pin state
	PinnedMsgs    []PinnedMsg     `json:"pinned_msgs,omitempty"`     // get_pinned: list of pinned messages
	EmojiName     string          `json:"emoji_name,omitempty"`      // custom_emoji: name for custom emoji
	EmojiURL      string          `json:"emoji_url,omitempty"`       // custom_emoji: URL for custom emoji image
	CustomEmojis  []CustomEmoji   `json:"custom_emojis,omitempty"`   // custom_emoji_list: all custom emojis

	// Phase 8: Server Administration
	Reason       string `json:"reason,omitempty"`        // ban/kick: ban reason
	Duration     int    `json:"duration,omitempty"`       // ban/mute: duration in seconds (0=permanent)
	BanIP        bool   `json:"ban_ip,omitempty"`         // ban: also ban the IP address
	Role         string `json:"role,omitempty"`           // set_role: target role
	Announcement string `json:"announcement,omitempty"`   // announcement: content
	BanID        int64  `json:"ban_id,omitempty"`         // unban: ban ID to remove
	SlowMode     int    `json:"slow_mode,omitempty"`      // set_slow_mode: cooldown in seconds
	Muted        bool   `json:"muted,omitempty"`          // user_muted: whether user is muted
	MuteExpiry   int64  `json:"mute_expiry,omitempty"`    // user_muted: when mute expires (unix ms, 0=permanent)
	MaxUsers     int    `json:"max_users,omitempty"`      // set_channel_limit/create_channel: user limit (0=unlimited)

	// Phase 7: Recording
	Recording  *bool           `json:"recording,omitempty"`   // recording_started/stopped: recording state
	Recordings []RecordingInfo `json:"recordings,omitempty"`  // list_recordings response

	// Phase 7: Simulcast / Video Quality
	VideoLayers  []VideoLayer `json:"video_layers,omitempty"`  // video_state: available simulcast layers
	VideoQuality string       `json:"video_quality,omitempty"` // set_video_quality: requested layer ("high","medium","low")

	// Phase 10: Performance & Reliability
	SeqNum  uint64 `json:"seq_num,omitempty"`  // message delivery: sequence number per channel
	LastSeq uint64 `json:"last_seq,omitempty"` // reconnect: last known sequence number
	Error   string `json:"error,omitempty"`    // error responses
}

// ReactionInfo describes a single emoji reaction on a message.
type ReactionInfo struct {
	Emoji   string   `json:"emoji"`
	UserIDs []uint16 `json:"user_ids"`
	Count   int      `json:"count"`
}

// ReplyPreview is a compact preview of the original message in a reply.
type ReplyPreview struct {
	MsgID    uint64 `json:"msg_id"`
	Username string `json:"username"`
	Message  string `json:"message"`
	Deleted  bool   `json:"deleted,omitempty"`
}

// SearchResult is a single search result returned by search_messages.
type SearchResult struct {
	MsgID     uint64 `json:"msg_id"`
	Username  string `json:"username"`
	Message   string `json:"message"`
	Timestamp int64  `json:"ts"`
	ChannelID int64  `json:"channel_id"`
}

// PinnedMsg represents a pinned message.
type PinnedMsg struct {
	MsgID     uint64 `json:"msg_id"`
	Username  string `json:"username"`
	Message   string `json:"message"`
	Timestamp int64  `json:"ts"`
	PinnedBy  uint16 `json:"pinned_by"`
}

// CustomEmoji represents a custom emoji uploaded to the server.
type CustomEmoji struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// UserInfo is a brief snapshot of a connected user, used in user_list messages.
type UserInfo struct {
	ID        uint16 `json:"id"`
	Username  string `json:"username"`
	ChannelID int64  `json:"channel_id,omitempty"` // 0 = not in any channel
	Role      string `json:"role,omitempty"`        // OWNER/ADMIN/MODERATOR/USER
	Muted     bool   `json:"muted,omitempty"`       // server-muted by admin
}

// VideoLayer describes a single simulcast layer available from a video sender.
// When a user starts video, they may offer multiple layers (e.g. high/medium/low)
// at different resolutions and bitrates. Receivers request their preferred layer
// via the set_video_quality command.
type VideoLayer struct {
	Quality string `json:"quality"` // "high", "medium", or "low"
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Bitrate int    `json:"bitrate"` // kbps
}

// DefaultVideoLayers returns the standard set of simulcast layers offered
// when a user starts video.
func DefaultVideoLayers() []VideoLayer {
	return []VideoLayer{
		{Quality: "high", Width: 1280, Height: 720, Bitrate: 2500},
		{Quality: "medium", Width: 640, Height: 360, Bitrate: 800},
		{Quality: "low", Width: 320, Height: 180, Bitrate: 200},
	}
}

// ChannelInfo is a brief snapshot of a channel, used in channel_list messages.
type ChannelInfo struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	SlowModeSeconds int    `json:"slow_mode_seconds,omitempty"`
	MaxUsers        int    `json:"max_users,omitempty"` // 0 = unlimited
}

// Roles for the permission hierarchy.
const (
	RoleOwner     = "OWNER"
	RoleAdmin     = "ADMIN"
	RoleModerator = "MODERATOR"
	RoleUser      = "USER"
)

// roleLevel returns a numeric level for comparison. Higher = more permissions.
func roleLevel(role string) int {
	switch role {
	case RoleOwner:
		return 4
	case RoleAdmin:
		return 3
	case RoleModerator:
		return 2
	case RoleUser:
		return 1
	default:
		return 0
	}
}

// HasPermission checks if the given role has permission for the specified action.
func HasPermission(role, action string) bool {
	level := roleLevel(role)
	switch action {
	case "kick":
		return level >= roleLevel(RoleModerator)
	case "mute", "unmute":
		return level >= roleLevel(RoleAdmin)
	case "ban", "unban", "manage_channels":
		return level >= roleLevel(RoleAdmin)
	case "delete_any_message", "pin_message":
		return level >= roleLevel(RoleModerator)
	case "set_role", "server_settings", "announce", "set_slow_mode":
		return level >= roleLevel(RoleOwner)
	default:
		return level >= roleLevel(RoleUser)
	}
}
