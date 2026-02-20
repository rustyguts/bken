// Package jitter implements a per-sender jitter buffer for voice datagrams.
//
// It reorders out-of-order packets using sequence numbers, buffers a
// configurable number of frames before starting playback, and signals
// missing frames so the caller can invoke Opus PLC (packet loss concealment).
package jitter

import "time"

const (
	ringSize = 16 // must be power of 2
	ringMask = ringSize - 1

	// staleTimeout is how long a sender must be silent before their stream
	// is pruned from the buffer.
	staleTimeout = 500 * time.Millisecond
)

// Frame is a single voice frame output from the jitter buffer.
type Frame struct {
	SenderID uint16
	OpusData []byte // nil signals a missing packet (caller should do PLC)
}

// slot holds one opus packet in the ring buffer.
type slot struct {
	opus []byte
	seq  uint16
	set  bool
}

// stream tracks per-sender jitter buffer state.
type stream struct {
	ring     [ringSize]slot
	nextPlay uint16    // next sequence number to consume
	primed   bool      // true once we've buffered enough frames to start
	count    int       // frames received during priming
	lastRecv time.Time // time of last Push
}

// Buffer is a per-sender jitter buffer. Not safe for concurrent use;
// the caller (playbackLoop) is the sole reader and synchronises externally.
type Buffer struct {
	streams map[uint16]*stream
	depth   int // frames to buffer before starting playback
}

// New creates a jitter buffer with the given depth (in 20 ms frames).
// A depth of 3 adds ~60 ms latency and tolerates reordering within that window.
func New(depth int) *Buffer {
	if depth < 1 {
		depth = 1
	}
	if depth > ringSize/2 {
		depth = ringSize / 2
	}
	return &Buffer{
		streams: make(map[uint16]*stream),
		depth:   depth,
	}
}

// Push inserts a received packet into the sender's ring buffer.
func (b *Buffer) Push(senderID, seq uint16, opus []byte) {
	s, ok := b.streams[senderID]
	if !ok {
		s = &stream{nextPlay: seq}
		b.streams[senderID] = s
	}
	s.lastRecv = time.Now()

	idx := int(seq) & ringMask

	if !s.primed {
		// During priming, accumulate frames without consuming.
		s.ring[idx] = slot{opus: opus, seq: seq, set: true}
		s.count++
		if s.count >= b.depth {
			s.primed = true
		}
		return
	}

	// Signed distance from nextPlay: positive = ahead, negative = behind.
	dist := int16(seq - s.nextPlay)

	if dist < 0 {
		// Late arrival (already played past this seq) — drop.
		return
	}
	if int(dist) >= ringSize {
		// Way ahead of expectation — likely a sender restart or long gap.
		// Reset the stream and start priming again.
		*s = stream{
			nextPlay: seq,
			lastRecv: time.Now(),
			count:    1,
		}
		s.ring[idx] = slot{opus: opus, seq: seq, set: true}
		if s.count >= b.depth {
			s.primed = true
		}
		return
	}

	s.ring[idx] = slot{opus: opus, seq: seq, set: true}
}

// Pop returns one frame per active sender for the current 20 ms playback tick.
// Senders that have gone silent for more than staleTimeout are pruned.
func (b *Buffer) Pop() []Frame {
	now := time.Now()
	var frames []Frame
	var stale []uint16

	for id, s := range b.streams {
		if now.Sub(s.lastRecv) > staleTimeout {
			stale = append(stale, id)
			continue
		}
		if !s.primed {
			continue
		}

		idx := int(s.nextPlay) & ringMask
		if s.ring[idx].set && s.ring[idx].seq == s.nextPlay {
			frames = append(frames, Frame{SenderID: id, OpusData: s.ring[idx].opus})
			s.ring[idx] = slot{} // clear
		} else {
			// Missing frame — signal PLC to the caller.
			s.ring[idx] = slot{} // clear any stale data
			frames = append(frames, Frame{SenderID: id, OpusData: nil})
		}
		s.nextPlay++
	}

	for _, id := range stale {
		delete(b.streams, id)
	}

	return frames
}

// Reset clears all buffered state (e.g. on disconnect).
func (b *Buffer) Reset() {
	b.streams = make(map[uint16]*stream)
}

// ActiveSenders returns the number of senders with primed streams.
func (b *Buffer) ActiveSenders() int {
	n := 0
	for _, s := range b.streams {
		if s.primed {
			n++
		}
	}
	return n
}
