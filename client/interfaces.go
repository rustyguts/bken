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

	// Callback setters — prefer setters over exported fields so the interface
	// can be satisfied by both the real Transport and test doubles.
	SetOnUserList(fn func([]UserInfo))
	SetOnUserJoined(fn func(uint16, string))
	SetOnUserLeft(fn func(uint16))
	SetOnAudioReceived(fn func(uint16))
	SetOnDisconnected(fn func())
}
