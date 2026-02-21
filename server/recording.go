package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Recording constants.
const (
	maxRecordingDuration = 2 * time.Hour
	recordingsDir        = "recordings"
)

// RecordingInfo holds metadata about a completed or in-progress recording.
type RecordingInfo struct {
	ID        string `json:"id"`
	ChannelID int64  `json:"channel_id"`
	StartedBy string `json:"started_by"` // username
	StartedAt int64  `json:"started_at"` // unix ms
	StoppedAt int64  `json:"stopped_at"` // unix ms; 0 if still recording
	Duration  int64  `json:"duration_ms"`
	FileName  string `json:"file_name"`
	FileSize  int64  `json:"file_size"`
}

// ChannelRecorder captures incoming Opus datagrams for a voice channel and
// writes them to an OGG/Opus file. The server calls FeedDatagram from the
// Broadcast path with raw 4-byte-header datagrams.
//
// File format: OGG container with Opus payload. Each Opus packet from any
// sender is written as a separate OGG page with the granule position
// advancing by 960 samples (20 ms at 48 kHz).
type ChannelRecorder struct {
	mu        sync.Mutex
	channelID int64
	startedBy string
	startedAt time.Time
	file      *os.File
	ogg       *oggWriter
	stopped   bool
	maxTimer  *time.Timer
	stopFn    func() // called when max duration reached
	packets   uint64
}

// StartRecording begins recording a channel's audio to disk.
// dataDir is the base data directory (e.g. "." or "/data").
// stopFn is called if the max duration is reached.
func StartRecording(channelID int64, startedBy, dataDir string, stopFn func()) (*ChannelRecorder, error) {
	dir := filepath.Join(dataDir, recordingsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create recordings dir: %w", err)
	}

	now := time.Now()
	filename := fmt.Sprintf("ch%d_%s.ogg", channelID, now.Format("20060102_150405"))
	path := filepath.Join(dir, filename)

	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create recording file: %w", err)
	}

	ogg := newOGGWriter(f)
	if err := ogg.writeHeaders(); err != nil {
		f.Close()
		os.Remove(path)
		return nil, fmt.Errorf("write OGG headers: %w", err)
	}

	cr := &ChannelRecorder{
		channelID: channelID,
		startedBy: startedBy,
		startedAt: now,
		file:      f,
		ogg:       ogg,
		stopFn:    stopFn,
	}

	cr.maxTimer = time.AfterFunc(maxRecordingDuration, func() {
		log.Printf("[recording] channel %d: max duration reached, auto-stopping", channelID)
		cr.Stop()
		if stopFn != nil {
			stopFn()
		}
	})

	log.Printf("[recording] channel %d: started by %s, file=%s", channelID, startedBy, filename)
	return cr, nil
}

// FeedDatagram writes an incoming voice datagram to the recording.
// The datagram has the standard format: [senderID:2][seq:2][opus_payload].
func (cr *ChannelRecorder) FeedDatagram(data []byte) {
	if len(data) <= 4 {
		return // no payload
	}
	opus := data[4:] // skip senderID (2) + seq (2)

	cr.mu.Lock()
	defer cr.mu.Unlock()
	if cr.stopped {
		return
	}

	cr.packets++
	if err := cr.ogg.writeOpusPacket(opus, cr.packets); err != nil {
		log.Printf("[recording] channel %d: write error: %v", cr.channelID, err)
	}
}

// Stop ends the recording and closes the file. Safe to call multiple times.
func (cr *ChannelRecorder) Stop() {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	if cr.stopped {
		return
	}
	cr.stopped = true
	if cr.maxTimer != nil {
		cr.maxTimer.Stop()
	}
	if cr.ogg != nil {
		cr.ogg.close()
	}
	if cr.file != nil {
		cr.file.Close()
	}
	log.Printf("[recording] channel %d: stopped, %d packets recorded", cr.channelID, cr.packets)
}

// Info returns metadata about this recording.
func (cr *ChannelRecorder) Info() RecordingInfo {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	info := RecordingInfo{
		ID:        filepath.Base(cr.file.Name()),
		ChannelID: cr.channelID,
		StartedBy: cr.startedBy,
		StartedAt: cr.startedAt.UnixMilli(),
		FileName:  filepath.Base(cr.file.Name()),
	}

	if cr.stopped {
		dur := time.Duration(cr.packets) * 20 * time.Millisecond
		info.Duration = dur.Milliseconds()
		info.StoppedAt = cr.startedAt.Add(dur).UnixMilli()
		if fi, err := os.Stat(cr.file.Name()); err == nil {
			info.FileSize = fi.Size()
		}
	}

	return info
}

// FilePath returns the full file path of the recording.
func (cr *ChannelRecorder) FilePath() string {
	return cr.file.Name()
}

// ---------------------------------------------------------------------------
// OGG/Opus writer — minimal implementation for writing Opus packets into an
// OGG container. Reference: RFC 7845 (Ogg Encapsulation for Opus).
// ---------------------------------------------------------------------------

type oggWriter struct {
	w         *os.File
	serial    uint32
	pageSeqNo uint32
}

func newOGGWriter(f *os.File) *oggWriter {
	return &oggWriter{
		w:      f,
		serial: 0x42454B4E, // "BEKN"
	}
}

// writeHeaders writes the mandatory OpusHead and OpusTags pages.
func (o *oggWriter) writeHeaders() error {
	// OpusHead (RFC 7845 section 5.1)
	head := make([]byte, 19)
	copy(head[0:8], "OpusHead")
	head[8] = 1  // version
	head[9] = 1  // channel count (mono mix from server perspective)
	binary.LittleEndian.PutUint16(head[10:12], 0)     // pre-skip
	binary.LittleEndian.PutUint32(head[12:16], 48000)  // sample rate
	binary.LittleEndian.PutUint16(head[16:18], 0)      // output gain
	head[18] = 0 // channel mapping family

	if err := o.writePage(head, 0, 2); err != nil { // flag 2 = beginning of stream
		return err
	}

	// OpusTags (RFC 7845 section 5.2)
	vendor := "bken"
	tags := make([]byte, 8+4+len(vendor)+4)
	copy(tags[0:8], "OpusTags")
	binary.LittleEndian.PutUint32(tags[8:12], uint32(len(vendor)))
	copy(tags[12:12+len(vendor)], vendor)
	binary.LittleEndian.PutUint32(tags[12+len(vendor):], 0) // no user comments

	return o.writePage(tags, 0, 0)
}

// writeOpusPacket writes a single Opus packet as an OGG page.
// packetNum is 1-based; granule advances by 960 per packet (20 ms at 48 kHz).
func (o *oggWriter) writeOpusPacket(opus []byte, packetNum uint64) error {
	granule := packetNum * 960
	return o.writePage(opus, granule, 0)
}

// close writes the final empty page with EOS flag.
func (o *oggWriter) close() {
	// Write EOS page (flag 4).
	_ = o.writePage(nil, 0, 4)
}

// writePage writes a single OGG page.
// headerType: 0=normal, 1=continuation, 2=BOS, 4=EOS.
func (o *oggWriter) writePage(payload []byte, granulePos uint64, headerType byte) error {
	// Compute segment table.
	segments := len(payload) / 255
	if len(payload)%255 != 0 || len(payload) == 0 {
		segments++
	}
	if segments == 0 {
		segments = 1
	}

	segTable := make([]byte, segments)
	remaining := len(payload)
	for i := 0; i < segments; i++ {
		if remaining >= 255 {
			segTable[i] = 255
			remaining -= 255
		} else {
			segTable[i] = byte(remaining)
			remaining = 0
		}
	}

	// Build OGG page header (27 bytes + segment table).
	header := make([]byte, 27+len(segTable))
	copy(header[0:4], "OggS")
	header[4] = 0          // version
	header[5] = headerType // header type
	binary.LittleEndian.PutUint64(header[6:14], granulePos)
	binary.LittleEndian.PutUint32(header[14:18], o.serial)
	binary.LittleEndian.PutUint32(header[18:22], o.pageSeqNo)
	// header[22:26] = checksum (computed below)
	header[26] = byte(len(segTable))
	copy(header[27:], segTable)

	// Compute CRC-32 over header + payload.
	crc := oggCRC(header, payload)
	binary.LittleEndian.PutUint32(header[22:26], crc)

	o.pageSeqNo++

	if _, err := o.w.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := o.w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// oggCRC computes the OGG CRC-32 using the polynomial 0x04C11DB7.
// This is NOT the standard CRC-32 (which uses a reflected polynomial);
// OGG uses the unreflected form defined in the Ogg spec.
func oggCRC(header, payload []byte) uint32 {
	var crc uint32
	for _, b := range header {
		crc = (crc << 8) ^ oggCRCTable[byte(crc>>24)^b]
	}
	for _, b := range payload {
		crc = (crc << 8) ^ oggCRCTable[byte(crc>>24)^b]
	}
	return crc
}

// oggCRCTable is the lookup table for the OGG CRC-32 polynomial 0x04C11DB7.
var oggCRCTable = func() [256]uint32 {
	const poly = 0x04C11DB7
	var table [256]uint32
	for i := range table {
		r := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if r&0x80000000 != 0 {
				r = (r << 1) ^ poly
			} else {
				r <<= 1
			}
		}
		table[i] = r
	}
	return table
}()
