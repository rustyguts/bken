package main

import (
	"context"
	"embed"
	"encoding/binary"
	"io"
	"log"
	"time"
)

//go:embed testdata/tone_frames.bin
var toneData embed.FS

// noopSender discards datagrams received by the virtual client.
type noopSender struct{}

func (noopSender) SendDatagram([]byte) error { return nil }

// RunTestBot creates a virtual client in the room that sends a periodic 440 Hz
// tone as Opus datagrams. It reuses pre-encoded Opus frames embedded at build
// time so no CGO dependency is required at runtime.
func RunTestBot(ctx context.Context, room *Room, name string) {
	frames, err := loadToneFrames()
	if err != nil {
		log.Printf("[testbot] failed to load tone frames: %v", err)
		return
	}
	if len(frames) == 0 {
		log.Println("[testbot] no tone frames found")
		return
	}

	client := &Client{
		Username: name,
		session:  noopSender{},
	}
	id := room.AddClient(client)
	room.BroadcastControl(ControlMsg{Type: "user_joined", ID: id, Username: name}, id)
	log.Printf("[testbot] %q connected as client %d with %d frames", name, id, len(frames))

	defer func() {
		room.RemoveClient(id)
		room.BroadcastControl(ControlMsg{Type: "user_left", ID: id}, 0)
		log.Printf("[testbot] %q disconnected", name)
	}()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	var seq uint16
	frameIdx := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		frame := frames[frameIdx]
		frameIdx = (frameIdx + 1) % len(frames)

		// Build datagram: [senderID:2][seq:2][opus_payload]
		dgram := make([]byte, 4+len(frame))
		binary.BigEndian.PutUint16(dgram[0:2], id)
		binary.BigEndian.PutUint16(dgram[2:4], seq)
		copy(dgram[4:], frame)
		seq++

		room.Broadcast(id, dgram)
	}
}

// loadToneFrames reads the pre-encoded Opus frames from the embedded binary.
// Format: repeated [uint16 BE length][frame data].
func loadToneFrames() ([][]byte, error) {
	f, err := toneData.Open("testdata/tone_frames.bin")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var frames [][]byte
	for {
		var length uint16
		if err := binary.Read(f, binary.BigEndian, &length); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		frame := make([]byte, length)
		if _, err := io.ReadFull(f, frame); err != nil {
			return nil, err
		}
		frames = append(frames, frame)
	}
	return frames, nil
}
