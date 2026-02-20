package main

import "context"

// Transporter is the interface wrapping the Transport methods used by App.
// Defining it here lets App be tested with a mock transport.
type Transporter interface {
	Connect(ctx context.Context, addr, username string) error
	Disconnect()
	SendAudio(opusData []byte) error
	StartReceiving(ctx context.Context, playbackCh chan<- []byte)
	MyID() uint16
	GetMetrics() Metrics

	// Per-user local muting — purely client-side, no server involvement.
	MuteUser(id uint16)
	UnmuteUser(id uint16)
	IsUserMuted(id uint16) bool
	MutedUsers() []uint16

	// Callback setters — prefer setters over exported fields so the interface
	// can be satisfied by both the real Transport and test doubles.
	SetOnUserList(fn func([]UserInfo))
	SetOnUserJoined(fn func(uint16, string))
	SetOnUserLeft(fn func(uint16))
	SetOnAudioReceived(fn func(uint16))
	SetOnDisconnected(fn func())
	SetOnChatMessage(fn func(username, message string, ts int64))
	SetOnChannelChatMessage(fn func(channelID int64, username, message string, ts int64))
	SetOnServerInfo(fn func(name string))
	SetOnKicked(fn func())
	SetOnOwnerChanged(fn func(ownerID uint16))
	SetOnChannelList(fn func([]ChannelInfo))
	SetOnUserChannel(fn func(userID uint16, channelID int64))

	// Chat.
	SendChat(message string) error

	// Moderation.
	KickUser(id uint16) error

	// Server management (owner-only; server enforces).
	RenameServer(name string) error

	// Channels.
	JoinChannel(id int64) error
	SendChannelChat(channelID int64, message string) error
}
