package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/webtransport-go"
)

// ControlMsg mirrors the server's control message format.
type ControlMsg struct {
	Type     string     `json:"type"`
	Username string     `json:"username,omitempty"`
	ID       uint16     `json:"id,omitempty"`
	Users    []UserInfo `json:"users,omitempty"`
	Ts       int64      `json:"ts,omitempty"` // ping/pong timestamp (Unix ms)
}

type UserInfo struct {
	ID       uint16 `json:"id"`
	Username string `json:"username"`
}

// Metrics holds connection quality metrics shown in the UI.
type Metrics struct {
	RTTMs       float64 `json:"rtt_ms"`
	PacketLoss  float64 `json:"packet_loss"`  // 0.0–1.0
	BitrateKbps float64 `json:"bitrate_kbps"` // outgoing audio
}

// Transport manages the WebTransport connection to the server.
type Transport struct {
	mu      sync.Mutex
	session *webtransport.Session
	cancel  context.CancelFunc
	myID    uint16

	// Control stream write serialisation.
	ctrlMu sync.Mutex
	ctrl   *webtransport.Stream

	// Sequence counter for outgoing datagrams.
	seq atomic.Uint32

	// RTT: smoothed via EWMA, stored as float64 bits.
	smoothedRTT atomic.Uint64
	lastPingTs  atomic.Int64 // Unix ms of last ping sent

	// Bitrate: bytes sent since last GetMetrics call.
	bytesSent atomic.Uint64

	// Packet loss: incoming sequence-gap accounting.
	lostPackets     atomic.Uint64
	expectedPackets atomic.Uint64

	// For per-interval bitrate calculation.
	metricsMu       sync.Mutex
	lastMetricsTime time.Time

	// Callbacks set by App.
	OnUserList      func([]UserInfo)
	OnUserJoined    func(uint16, string)
	OnUserLeft      func(uint16)
	OnAudioReceived func(uint16)
	OnDisconnected  func()
}

func NewTransport() *Transport {
	return &Transport{lastMetricsTime: time.Now()}
}

// writeCtrl serialises a control message write (safe for concurrent callers).
func (t *Transport) writeCtrl(msg ControlMsg) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	data = append(data, '\n')
	t.ctrlMu.Lock()
	defer t.ctrlMu.Unlock()
	if t.ctrl != nil {
		t.ctrl.Write(data)
	}
}

// Connect establishes a WebTransport session and sends the join message.
func (t *Transport) Connect(ctx context.Context, addr, username string) error {
	ctx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	d := webtransport.Dialer{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		QUICConfig: &quic.Config{
			EnableDatagrams:                  true,
			EnableStreamResetPartialDelivery: true,
		},
	}

	_, sess, err := d.Dial(ctx, "https://"+addr, http.Header{})
	if err != nil {
		cancel()
		return err
	}

	t.mu.Lock()
	t.session = sess
	t.mu.Unlock()

	// Open the control stream.
	stream, err := sess.OpenStream()
	if err != nil {
		cancel()
		sess.CloseWithError(0, "failed to open stream")
		return err
	}
	t.ctrlMu.Lock()
	t.ctrl = stream
	t.ctrlMu.Unlock()

	// Reset per-session metrics.
	t.smoothedRTT.Store(0)
	t.bytesSent.Store(0)
	t.lostPackets.Store(0)
	t.expectedPackets.Store(0)
	t.metricsMu.Lock()
	t.lastMetricsTime = time.Now()
	t.metricsMu.Unlock()

	// Send join message.
	t.writeCtrl(ControlMsg{Type: "join", Username: username})

	// Read control messages.
	go t.readControl(ctx, stream)

	// Send periodic pings for RTT measurement.
	go t.pingLoop(ctx)

	return nil
}

// Disconnect closes the WebTransport session.
func (t *Transport) Disconnect() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	if t.session != nil {
		t.session.CloseWithError(0, "disconnect")
		t.session = nil
	}
}

// SendAudio sends an encoded OPUS frame as a datagram.
func (t *Transport) SendAudio(opusData []byte) error {
	t.mu.Lock()
	sess := t.session
	t.mu.Unlock()

	if sess == nil {
		return nil
	}

	seq := uint16(t.seq.Add(1))

	// Build datagram: [userID:2][seq:2][opus_payload]
	dgram := make([]byte, 4+len(opusData))
	binary.BigEndian.PutUint16(dgram[0:2], t.myID)
	binary.BigEndian.PutUint16(dgram[2:4], seq)
	copy(dgram[4:], opusData)

	t.bytesSent.Add(uint64(len(dgram)))
	return sess.SendDatagram(dgram)
}

// MyID returns the local client's assigned user ID (0 if not yet assigned).
func (t *Transport) MyID() uint16 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.myID
}

// StartReceiving reads incoming datagrams and sends decoded audio to the playback channel.
func (t *Transport) StartReceiving(ctx context.Context, playbackCh chan<- []byte) {
	go func() {
		speakTimers := make(map[uint16]time.Time)
		lastSeq := make(map[uint16]uint16) // senderID → last seq

		for {
			t.mu.Lock()
			sess := t.session
			t.mu.Unlock()

			if sess == nil {
				return
			}

			data, err := sess.ReceiveDatagram(ctx)
			if err != nil {
				return
			}
			if len(data) < 4 {
				continue
			}

			userID := binary.BigEndian.Uint16(data[0:2])
			seq := binary.BigEndian.Uint16(data[2:4])

			// Sequence-gap packet loss accounting.
			if prev, ok := lastSeq[userID]; ok {
				diff := int(seq) - int(prev)
				if diff < 0 {
					diff += 65536 // handle uint16 wraparound
				}
				if diff > 0 {
					t.expectedPackets.Add(uint64(diff))
					if diff > 1 {
						t.lostPackets.Add(uint64(diff - 1))
					}
				}
			}
			lastSeq[userID] = seq

			// Speaking notification, throttled per user to ~80ms.
			if t.OnAudioReceived != nil {
				if last, ok := speakTimers[userID]; !ok || time.Since(last) > 80*time.Millisecond {
					speakTimers[userID] = time.Now()
					t.OnAudioReceived(userID)
				}
			}

			// Extract OPUS payload (skip userID + seq header).
			opusData := make([]byte, len(data)-4)
			copy(opusData, data[4:])

			select {
			case playbackCh <- opusData:
			default:
			}
		}
	}()
}

// GetMetrics returns current connection quality metrics.
func (t *Transport) GetMetrics() Metrics {
	now := time.Now()

	t.metricsMu.Lock()
	elapsed := now.Sub(t.lastMetricsTime).Seconds()
	if elapsed <= 0 {
		elapsed = 2
	}
	t.lastMetricsTime = now
	t.metricsMu.Unlock()

	bytes := t.bytesSent.Swap(0)
	bitrate := float64(bytes*8) / elapsed / 1000 // kbps

	lost := t.lostPackets.Swap(0)
	expected := t.expectedPackets.Swap(0)
	var loss float64
	if expected > 0 {
		loss = float64(lost) / float64(expected)
		if loss > 1 {
			loss = 1
		}
	}

	rtt := math.Float64frombits(t.smoothedRTT.Load())

	return Metrics{
		RTTMs:       rtt,
		PacketLoss:  loss,
		BitrateKbps: bitrate,
	}
}

// pingLoop sends a ping every 2 s for RTT measurement.
func (t *Transport) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ts := time.Now().UnixMilli()
			t.lastPingTs.Store(ts)
			t.writeCtrl(ControlMsg{Type: "ping", Ts: ts})
		}
	}
}

// readControl reads JSON control messages from the server.
func (t *Transport) readControl(ctx context.Context, stream *webtransport.Stream) {
	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		var msg ControlMsg
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Printf("[transport] invalid control msg: %v", err)
			continue
		}

		switch msg.Type {
		case "user_list":
			if t.OnUserList != nil {
				t.OnUserList(msg.Users)
			}
			// Set our own ID (we are the last entry the server added).
			for _, u := range msg.Users {
				if u.Username != "" {
					t.myID = u.ID
				}
			}
		case "user_joined":
			if t.OnUserJoined != nil {
				t.OnUserJoined(msg.ID, msg.Username)
			}
		case "user_left":
			if t.OnUserLeft != nil {
				t.OnUserLeft(msg.ID)
			}
		case "pong":
			sent := t.lastPingTs.Load()
			if sent != 0 {
				sample := float64(time.Now().UnixMilli() - sent)
				old := math.Float64frombits(t.smoothedRTT.Load())
				var next float64
				if old == 0 {
					next = sample
				} else {
					next = 0.125*sample + 0.875*old // EWMA (RFC 6298)
				}
				t.smoothedRTT.Store(math.Float64bits(next))
			}
		}
	}
	if t.OnDisconnected != nil {
		t.OnDisconnected()
	}
}

// MarshalDatagram builds a voice datagram. Exported for testing.
func MarshalDatagram(userID, seq uint16, opus []byte) []byte {
	dgram := make([]byte, 4+len(opus))
	binary.BigEndian.PutUint16(dgram[0:2], userID)
	binary.BigEndian.PutUint16(dgram[2:4], seq)
	copy(dgram[4:], opus)
	return dgram
}

// ParseDatagram parses a voice datagram. Exported for testing.
func ParseDatagram(data []byte) (userID, seq uint16, opus []byte, ok bool) {
	if len(data) < 4 {
		return 0, 0, nil, false
	}
	userID = binary.BigEndian.Uint16(data[0:2])
	seq = binary.BigEndian.Uint16(data[2:4])
	opus = data[4:]
	return userID, seq, opus, true
}
