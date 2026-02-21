package main

import (
	"os"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Concurrent recordings on different channels
// ---------------------------------------------------------------------------

func TestConcurrentRecordingsDifferentChannels(t *testing.T) {
	room := NewRoom()
	room.SetDataDir(t.TempDir())

	const numChannels = 5
	for i := int64(1); i <= numChannels; i++ {
		if err := room.StartRecordingChannel(i, "alice"); err != nil {
			t.Fatalf("StartRecordingChannel(%d): %v", i, err)
		}
	}

	// Feed data concurrently to all channels.
	var wg sync.WaitGroup
	for i := int64(1); i <= numChannels; i++ {
		wg.Add(1)
		go func(ch int64) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				data := make([]byte, 24)
				data[0] = byte(ch >> 8)
				data[1] = byte(ch)
				data[2] = byte(j >> 8)
				data[3] = byte(j)
				for k := 4; k < 24; k++ {
					data[k] = byte(k + int(ch))
				}
				room.FeedRecording(ch, data)
			}
		}(i)
	}
	wg.Wait()

	// Stop each channel individually (StopRecordingChannel adds to doneRecs).
	for i := int64(1); i <= numChannels; i++ {
		if err := room.StopRecordingChannel(i); err != nil {
			t.Errorf("StopRecordingChannel(%d): %v", i, err)
		}
	}

	recs := room.ListRecordings()
	if len(recs) != numChannels {
		t.Errorf("expected %d recordings, got %d", numChannels, len(recs))
	}

	for _, rec := range recs {
		if rec.FileSize == 0 {
			t.Errorf("channel %d recording has zero size", rec.ChannelID)
		}
	}
}

// ---------------------------------------------------------------------------
// Recording with many users joining/leaving mid-recording
// ---------------------------------------------------------------------------

func TestRecordingConcurrentFeedAndStop(t *testing.T) {
	room := NewRoom()
	room.SetDataDir(t.TempDir())

	if err := room.StartRecordingChannel(1, "alice"); err != nil {
		t.Fatalf("StartRecordingChannel: %v", err)
	}

	var wg sync.WaitGroup

	// Feed data from multiple goroutines.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(uid int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				data := make([]byte, 24)
				data[0] = 0
				data[1] = byte(uid)
				data[2] = byte(j >> 8)
				data[3] = byte(j)
				for k := 4; k < 24; k++ {
					data[k] = byte(k)
				}
				room.FeedRecording(1, data)
			}
		}(i)
	}

	// Stop while feeds are still happening.
	go func() {
		room.StopRecordingChannel(1)
	}()

	wg.Wait()

	// Should not panic and recording should be stopped.
	if room.IsRecording(1) {
		// Wait a bit for the stop goroutine to finish.
		room.StopRecordingChannel(1)
	}
}

// ---------------------------------------------------------------------------
// OGG file output validity
// ---------------------------------------------------------------------------

func TestRecordingOGGFileValidity(t *testing.T) {
	dir := t.TempDir()

	rec, err := StartRecording(1, "alice", dir, "recordings", nil)
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}

	// Feed multiple packets.
	for i := 0; i < 20; i++ {
		data := make([]byte, 104) // 4 header + 100 payload
		data[0] = 0
		data[1] = 1
		data[2] = 0
		data[3] = byte(i)
		for j := 4; j < 104; j++ {
			data[j] = byte(j + i)
		}
		rec.FeedDatagram(data)
	}

	rec.Stop()

	content, err := os.ReadFile(rec.FilePath())
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	// Verify OGG structure: must start with OggS.
	if len(content) < 4 || string(content[:4]) != "OggS" {
		t.Error("file should start with OggS magic")
	}

	// Count OggS pages — should be at least 3 (OpusHead, OpusTags, 1+ data pages, EOS).
	pageCount := 0
	for i := 0; i <= len(content)-4; i++ {
		if string(content[i:i+4]) == "OggS" {
			pageCount++
		}
	}
	// OpusHead + OpusTags + 20 data pages + EOS = 23 pages minimum
	if pageCount < 23 {
		t.Errorf("expected at least 23 OGG pages, got %d", pageCount)
	}

	// Verify OpusHead and OpusTags markers.
	foundHead := false
	foundTags := false
	for i := 0; i <= len(content)-8; i++ {
		if string(content[i:i+8]) == "OpusHead" {
			foundHead = true
		}
		if string(content[i:i+8]) == "OpusTags" {
			foundTags = true
		}
	}
	if !foundHead {
		t.Error("OpusHead marker not found")
	}
	if !foundTags {
		t.Error("OpusTags marker not found")
	}
}

// ---------------------------------------------------------------------------
// Cleanup on server shutdown (StopAllRecordings)
// ---------------------------------------------------------------------------

func TestRecordingCleanupOnShutdown(t *testing.T) {
	room := NewRoom()
	room.SetDataDir(t.TempDir())

	for i := int64(1); i <= 3; i++ {
		if err := room.StartRecordingChannel(i, "alice"); err != nil {
			t.Fatalf("StartRecordingChannel(%d): %v", i, err)
		}
		// Feed some data.
		data := make([]byte, 24)
		for j := 4; j < 24; j++ {
			data[j] = byte(j)
		}
		room.FeedRecording(i, data)
	}

	// Simulate shutdown via StopAllRecordings (does not add to doneRecs).
	room.StopAllRecordings()

	// All recordings should be stopped.
	for i := int64(1); i <= 3; i++ {
		if room.IsRecording(i) {
			t.Errorf("channel %d should not be recording after shutdown", i)
		}
	}
}

func TestRecordingStopViaStopRecordingChannelAddsToDoneRecs(t *testing.T) {
	room := NewRoom()
	room.SetDataDir(t.TempDir())

	for i := int64(1); i <= 3; i++ {
		if err := room.StartRecordingChannel(i, "alice"); err != nil {
			t.Fatalf("StartRecordingChannel(%d): %v", i, err)
		}
		data := make([]byte, 24)
		for j := 4; j < 24; j++ {
			data[j] = byte(j)
		}
		room.FeedRecording(i, data)
	}

	// Stop each via StopRecordingChannel (adds to doneRecs).
	for i := int64(1); i <= 3; i++ {
		if err := room.StopRecordingChannel(i); err != nil {
			t.Errorf("StopRecordingChannel(%d): %v", i, err)
		}
	}

	recs := room.ListRecordings()
	if len(recs) != 3 {
		t.Errorf("expected 3 completed recordings, got %d", len(recs))
	}

	// Verify all files exist on disk and are non-empty.
	for _, rec := range recs {
		path := room.GetRecordingFilePath(rec.FileName)
		fi, err := os.Stat(path)
		if err != nil {
			t.Errorf("recording file %q not found: %v", path, err)
			continue
		}
		if fi.Size() == 0 {
			t.Errorf("recording file %q is empty", path)
		}
	}
}

// ---------------------------------------------------------------------------
// Recording API: list recordings when none exist
// ---------------------------------------------------------------------------

func TestListRecordingsEmpty(t *testing.T) {
	room := NewRoom()
	recs := room.ListRecordings()
	if len(recs) != 0 {
		t.Errorf("expected 0 recordings, got %d", len(recs))
	}
}

// ---------------------------------------------------------------------------
// Recording Info: in-progress vs stopped
// ---------------------------------------------------------------------------

func TestRecordingInfoInProgress(t *testing.T) {
	dir := t.TempDir()
	rec, err := StartRecording(1, "alice", dir, "recordings", nil)
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}

	// Feed some data while recording is active.
	for i := 0; i < 3; i++ {
		data := make([]byte, 24)
		for j := 4; j < 24; j++ {
			data[j] = byte(j)
		}
		rec.FeedDatagram(data)
	}

	info := rec.Info()
	if info.StoppedAt != 0 {
		t.Errorf("in-progress recording should have StoppedAt=0, got %d", info.StoppedAt)
	}
	if info.Duration != 0 {
		t.Errorf("in-progress recording should have Duration=0, got %d", info.Duration)
	}

	rec.Stop()

	info = rec.Info()
	if info.StoppedAt == 0 {
		t.Error("stopped recording should have non-zero StoppedAt")
	}
	// 3 packets * 20ms = 60ms
	if info.Duration != 60 {
		t.Errorf("duration: got %d, want 60", info.Duration)
	}
}

// ---------------------------------------------------------------------------
// processControl: list_recordings
// ---------------------------------------------------------------------------

func TestProcessControlListRecordings(t *testing.T) {
	room := NewRoom()
	room.SetDataDir(t.TempDir())

	client, clientBuf := newCtrlClient("alice")
	room.AddClient(client)
	room.ClaimOwnership(client.ID)
	room.SetClientRole(client.ID, RoleOwner)
	client.channelID.Store(1) // Put client in channel 1

	// No recordings yet.
	processControl(ControlMsg{Type: "list_recordings"}, client, room)
	got := decodeControl(t, clientBuf)
	if got.Type != "recordings_list" {
		t.Errorf("type: got %q, want %q", got.Type, "recordings_list")
	}
	if len(got.Recordings) != 0 {
		t.Errorf("expected 0 recordings, got %d", len(got.Recordings))
	}

	// Start recording.
	processControl(ControlMsg{Type: "start_recording", ChannelID: 1}, client, room)
	_ = decodeControl(t, clientBuf) // drain recording_started (client is in channel 1)

	// Stop recording.
	processControl(ControlMsg{Type: "stop_recording", ChannelID: 1}, client, room)
	_ = decodeControl(t, clientBuf) // drain recording_stopped

	// Now list should show 1.
	processControl(ControlMsg{Type: "list_recordings"}, client, room)
	got = decodeControl(t, clientBuf)
	if len(got.Recordings) != 1 {
		t.Errorf("expected 1 recording, got %d", len(got.Recordings))
	}
}

// ---------------------------------------------------------------------------
// Recording: stop via processControl sends broadcast
// ---------------------------------------------------------------------------

func TestProcessControlStopRecordingBroadcasts(t *testing.T) {
	room := NewRoom()
	room.SetDataDir(t.TempDir())
	room.SetChannels([]ChannelInfo{{ID: 1, Name: "General"}})

	owner, ownerBuf := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)
	owner.channelID.Store(1)

	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(receiver)
	receiver.channelID.Store(1)

	// Start recording.
	processControl(ControlMsg{Type: "start_recording", ChannelID: 1}, owner, room)
	_ = decodeControl(t, ownerBuf)   // drain recording_started (owner is in channel)
	_ = decodeControl(t, receiverBuf) // drain recording_started (receiver is in channel)

	// Stop recording.
	processControl(ControlMsg{Type: "stop_recording", ChannelID: 1}, owner, room)

	got := decodeControl(t, receiverBuf)
	if got.Type != "recording_stopped" {
		t.Errorf("type: got %q, want %q", got.Type, "recording_stopped")
	}
	if got.ChannelID != 1 {
		t.Errorf("channel_id: got %d, want 1", got.ChannelID)
	}
	if got.Recording == nil || *got.Recording != false {
		t.Error("recording should be false after stop")
	}
}

// ---------------------------------------------------------------------------
// Large datagram payload
// ---------------------------------------------------------------------------

func TestRecordingLargeDatagram(t *testing.T) {
	dir := t.TempDir()
	rec, err := StartRecording(1, "alice", dir, "recordings", nil)
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}

	// Max Opus packet is ~1275 bytes. Create a larger payload.
	data := make([]byte, 2000)
	data[0] = 0
	data[1] = 1
	data[2] = 0
	data[3] = 0
	for i := 4; i < 2000; i++ {
		data[i] = byte(i % 256)
	}
	rec.FeedDatagram(data)
	rec.Stop()

	fi, err := os.Stat(rec.FilePath())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Size() == 0 {
		t.Error("file should not be empty after large datagram")
	}
}

// ---------------------------------------------------------------------------
// processControl: start recording on channel 0 should be rejected
// ---------------------------------------------------------------------------

func TestProcessControlStartRecordingZeroChannel(t *testing.T) {
	room := NewRoom()
	room.SetDataDir(t.TempDir())

	owner, _ := newCtrlClient("alice")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	processControl(ControlMsg{Type: "start_recording", ChannelID: 0}, owner, room)

	if room.IsRecording(0) {
		t.Error("recording on channel 0 should be rejected")
	}
}
