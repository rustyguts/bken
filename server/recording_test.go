package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordingLifecycle(t *testing.T) {
	dir := t.TempDir()
	stopped := make(chan struct{}, 1)

	rec, err := StartRecording(1, "alice", dir, "recordings", func() {
		stopped <- struct{}{}
	})
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}

	// Feed some fake datagrams: [senderID:2][seq:2][opus_payload]
	for i := 0; i < 10; i++ {
		data := make([]byte, 104) // 4 header + 100 payload
		data[0] = 0
		data[1] = byte(i)
		data[2] = 0
		data[3] = byte(i)
		// Fill payload with non-zero data.
		for j := 4; j < 104; j++ {
			data[j] = byte(j)
		}
		rec.FeedDatagram(data)
	}

	rec.Stop()

	// Verify the file was created and has content.
	path := rec.FilePath()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat recording file: %v", err)
	}
	if fi.Size() == 0 {
		t.Error("recording file is empty")
	}

	info := rec.Info()
	if info.ChannelID != 1 {
		t.Errorf("ChannelID = %d, want 1", info.ChannelID)
	}
	if info.StartedBy != "alice" {
		t.Errorf("StartedBy = %q, want %q", info.StartedBy, "alice")
	}
	if info.FileName == "" {
		t.Error("FileName is empty")
	}
}

func TestRecordingFeedAfterStop(t *testing.T) {
	dir := t.TempDir()

	rec, err := StartRecording(1, "bob", dir, "recordings", nil)
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}

	rec.Stop()

	// Feeding after stop should not panic.
	data := make([]byte, 50)
	rec.FeedDatagram(data)
}

func TestRecordingStopIdempotent(t *testing.T) {
	dir := t.TempDir()

	rec, err := StartRecording(1, "charlie", dir, "recordings", nil)
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}

	rec.Stop()
	rec.Stop() // second stop should not panic
}

func TestRecordingEmptyDatagram(t *testing.T) {
	dir := t.TempDir()

	rec, err := StartRecording(1, "dave", dir, "recordings", nil)
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}
	defer rec.Stop()

	// Datagram with only header (<=4 bytes) should be ignored.
	rec.FeedDatagram([]byte{0, 1, 0, 1})
	rec.FeedDatagram([]byte{0, 1, 0})
	rec.FeedDatagram(nil)
}

func TestRecordingOGGFileHeaders(t *testing.T) {
	dir := t.TempDir()

	rec, err := StartRecording(1, "eve", dir, "recordings", nil)
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}

	// Feed one packet so the file has content beyond headers.
	data := make([]byte, 24)
	for i := 4; i < 24; i++ {
		data[i] = byte(i)
	}
	rec.FeedDatagram(data)
	rec.Stop()

	// Read the file and verify OGG magic bytes.
	content, err := os.ReadFile(rec.FilePath())
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if len(content) < 4 || string(content[:4]) != "OggS" {
		t.Error("file does not start with OggS magic bytes")
	}

	// Verify OpusHead marker is present somewhere in the file.
	found := false
	for i := 0; i <= len(content)-8; i++ {
		if string(content[i:i+8]) == "OpusHead" {
			found = true
			break
		}
	}
	if !found {
		t.Error("OpusHead marker not found in recording file")
	}
}

func TestRoomRecordingStartStop(t *testing.T) {
	room := NewRoom()
	room.SetDataDir(t.TempDir())

	// Should fail for non-existent channel (but still create the recording).
	if err := room.StartRecordingChannel(1, "alice"); err != nil {
		t.Fatalf("StartRecordingChannel: %v", err)
	}

	if !room.IsRecording(1) {
		t.Error("expected IsRecording(1) = true")
	}

	// Starting again should fail.
	if err := room.StartRecordingChannel(1, "bob"); err == nil {
		t.Error("expected error for duplicate recording")
	}

	// Stop the recording.
	if err := room.StopRecordingChannel(1); err != nil {
		t.Fatalf("StopRecordingChannel: %v", err)
	}

	if room.IsRecording(1) {
		t.Error("expected IsRecording(1) = false after stop")
	}

	// Should appear in done recordings.
	recs := room.ListRecordings()
	if len(recs) != 1 {
		t.Fatalf("ListRecordings() len = %d, want 1", len(recs))
	}
	if recs[0].ChannelID != 1 {
		t.Errorf("recording ChannelID = %d, want 1", recs[0].ChannelID)
	}
}

func TestRoomRecordingFeedFromBroadcast(t *testing.T) {
	room := NewRoom()
	room.SetDataDir(t.TempDir())

	if err := room.StartRecordingChannel(5, "alice"); err != nil {
		t.Fatalf("StartRecordingChannel: %v", err)
	}

	// Feed data directly.
	data := make([]byte, 24)
	data[0] = 0
	data[1] = 1
	data[2] = 0
	data[3] = 0
	for i := 4; i < 24; i++ {
		data[i] = byte(i)
	}
	room.FeedRecording(5, data)

	if err := room.StopRecordingChannel(5); err != nil {
		t.Fatalf("StopRecordingChannel: %v", err)
	}

	recs := room.ListRecordings()
	if len(recs) != 1 {
		t.Fatalf("ListRecordings() len = %d, want 1", len(recs))
	}
	if recs[0].FileSize == 0 {
		t.Error("expected non-zero file size after feeding data")
	}
}

func TestRoomRecordingNoRecordingFeedNoop(t *testing.T) {
	room := NewRoom()
	// FeedRecording with no active recording should not panic.
	room.FeedRecording(1, make([]byte, 24))
}

func TestRoomStopRecordingNotActive(t *testing.T) {
	room := NewRoom()
	if err := room.StopRecordingChannel(99); err == nil {
		t.Error("expected error when stopping non-existent recording")
	}
}

func TestRoomStopAllRecordings(t *testing.T) {
	room := NewRoom()
	room.SetDataDir(t.TempDir())

	for i := int64(1); i <= 3; i++ {
		if err := room.StartRecordingChannel(i, "alice"); err != nil {
			t.Fatalf("StartRecordingChannel(%d): %v", i, err)
		}
	}

	room.StopAllRecordings()

	for i := int64(1); i <= 3; i++ {
		if room.IsRecording(i) {
			t.Errorf("expected IsRecording(%d) = false after StopAllRecordings", i)
		}
	}
}

func TestRecordingMaxDuration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping max duration test in short mode")
	}

	// We cannot really wait 2 hours, but we can verify the timer is set.
	dir := t.TempDir()
	rec, err := StartRecording(1, "alice", dir, "recordings", nil)
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}
	defer rec.Stop()

	// Verify maxTimer is non-nil (set in StartRecording).
	rec.mu.Lock()
	hasTimer := rec.maxTimer != nil
	rec.mu.Unlock()

	if !hasTimer {
		t.Error("expected maxTimer to be set")
	}
}

func TestProcessControlStartRecording(t *testing.T) {
	room := NewRoom()
	room.SetDataDir(t.TempDir())

	// Add a channel so IsRecording context makes sense.
	room.SetChannels([]ChannelInfo{{ID: 1, Name: "General"}})

	client := &Client{ID: 1, Username: "alice"}
	client.channelID.Store(1)
	room.AddClient(client)
	room.ClaimOwnership(client.ID)

	// Start recording via processControl.
	processControl(ControlMsg{Type: "start_recording", ChannelID: 1}, client, room)

	if !room.IsRecording(1) {
		t.Error("expected recording to be active after start_recording")
	}

	// Stop recording via processControl.
	processControl(ControlMsg{Type: "stop_recording", ChannelID: 1}, client, room)

	if room.IsRecording(1) {
		t.Error("expected recording to be inactive after stop_recording")
	}

	recs := room.ListRecordings()
	if len(recs) != 1 {
		t.Errorf("ListRecordings() len = %d, want 1", len(recs))
	}
}

func TestProcessControlStartRecordingNonOwner(t *testing.T) {
	room := NewRoom()
	room.SetDataDir(t.TempDir())

	owner := &Client{ID: 1, Username: "alice"}
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	// Non-owner tries to start recording.
	nonOwner := &Client{ID: 2, Username: "bob"}
	room.AddClient(nonOwner)

	processControl(ControlMsg{Type: "start_recording", ChannelID: 1}, nonOwner, room)

	if room.IsRecording(1) {
		t.Error("non-owner should not be able to start recording")
	}
}

func TestGetRecordingFilePath(t *testing.T) {
	room := NewRoom()
	room.SetDataDir("/data")

	path := room.GetRecordingFilePath("test.ogg")
	expected := filepath.Join("/data", "recordings", "test.ogg")
	if path != expected {
		t.Errorf("GetRecordingFilePath = %q, want %q", path, expected)
	}
}

func TestRecordingInfoFields(t *testing.T) {
	dir := t.TempDir()
	rec, err := StartRecording(42, "testuser", dir, "recordings", nil)
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}

	// Feed some data.
	for i := 0; i < 5; i++ {
		data := make([]byte, 24)
		for j := 4; j < 24; j++ {
			data[j] = byte(j + i)
		}
		rec.FeedDatagram(data)
	}

	rec.Stop()
	info := rec.Info()

	if info.ChannelID != 42 {
		t.Errorf("ChannelID = %d, want 42", info.ChannelID)
	}
	if info.StartedBy != "testuser" {
		t.Errorf("StartedBy = %q, want %q", info.StartedBy, "testuser")
	}
	if info.StartedAt == 0 {
		t.Error("StartedAt should be non-zero")
	}
	// 5 packets * 20ms = 100ms
	if info.Duration != 100 {
		t.Errorf("Duration = %d, want 100", info.Duration)
	}
	if info.FileSize <= 0 {
		t.Errorf("FileSize = %d, should be > 0", info.FileSize)
	}
}

func TestOGGCRCDeterministic(t *testing.T) {
	header := []byte("OggS\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00BEKN\x00\x00\x00\x00\x00\x00\x00\x00\x01\x13")
	payload := []byte("OpusHead\x01\x01\x00\x00\x80\xbb\x00\x00\x00\x00\x00")

	crc1 := oggCRC(header, payload)
	crc2 := oggCRC(header, payload)
	if crc1 != crc2 {
		t.Errorf("CRC not deterministic: %08x != %08x", crc1, crc2)
	}
	// CRC should be non-zero for non-trivial input.
	if crc1 == 0 {
		t.Error("CRC should be non-zero for non-trivial input")
	}
}

func TestOGGCRCTableLength(t *testing.T) {
	if len(oggCRCTable) != 256 {
		t.Errorf("oggCRCTable length = %d, want 256", len(oggCRCTable))
	}
}

func TestRecordingTimestampReasonable(t *testing.T) {
	dir := t.TempDir()
	before := time.Now().UnixMilli()
	rec, err := StartRecording(1, "alice", dir, "recordings", nil)
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}
	after := time.Now().UnixMilli()
	rec.Stop()

	info := rec.Info()
	if info.StartedAt < before || info.StartedAt > after {
		t.Errorf("StartedAt %d not in range [%d, %d]", info.StartedAt, before, after)
	}
}
