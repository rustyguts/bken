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

// UserInfo describes a connected peer.
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
// It implements the Transporter interface.
type Transport struct {
	mu      sync.Mutex
	session *webtransport.Session
	cancel  context.CancelFunc

	// myID is the server-assigned ID for this client.
	// Written once in readControl; protected by mu.
	myID uint16

	// Control stream write serialisation.
	ctrlMu sync.Mutex
	ctrl   *webtransport.Stream

	// Sequence counter for outgoing datagrams (monotonically increasing).
	seq atomic.Uint32

	// RTT: smoothed via EWMA (RFC 6298), stored as float64 bits for atomic access.
	smoothedRTT atomic.Uint64
	lastPingTs  atomic.Int64 // Unix ms of the last ping sent

	// lastPongTime records when the most recent pong was received (Unix nanoseconds).
	// Initialised to the connection start time; 0 means never received.
	lastPongTime atomic.Int64

	// Bytes sent since the last GetMetrics call (for bitrate calculation).
	bytesSent atomic.Uint64

	// Packet loss accounting via incoming sequence-gap detection.
	lostPackets     atomic.Uint64
	expectedPackets atomic.Uint64

	// lastMetricsTime is the timestamp of the previous GetMetrics call.
	metricsMu       sync.Mutex
	lastMetricsTime time.Time

	// Callbacks — set via setters before calling Connect.
	cbMu            sync.RWMutex
	onUserList      func([]UserInfo)
	onUserJoined    func(uint16, string)
	onUserLeft      func(uint16)
	onAudioReceived func(uint16)
	onDisconnected  func()
}

// Verify Transport satisfies the Transporter interface at compile time.
var _ Transporter = (*Transport)(nil)

// NewTransport creates a ready-to-use Transport.
func NewTransport() *Transport {
	return &Transport{lastMetricsTime: time.Now()}
}

// --- Callback setters (satisfy Transporter interface) ---

func (t *Transport) SetOnUserList(fn func([]UserInfo)) {
	t.cbMu.Lock()
	t.onUserList = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnUserJoined(fn func(uint16, string)) {
	t.cbMu.Lock()
	t.onUserJoined = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnUserLeft(fn func(uint16)) {
	t.cbMu.Lock()
	t.onUserLeft = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnAudioReceived(fn func(uint16)) {
	t.cbMu.Lock()
	t.onAudioReceived = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnDisconnected(fn func()) {
	t.cbMu.Lock()
	t.onDisconnected = fn
	t.cbMu.Unlock()
}

// writeCtrl serialises a control message write; safe for concurrent callers.
func (t *Transport) writeCtrl(msg ControlMsg) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	data = append(data, '\n')
	t.ctrlMu.Lock()
	defer t.ctrlMu.Unlock()
	if t.ctrl != nil {
		t.ctrl.Write(data) //nolint:errcheck — best-effort write on a closing stream
	}
}

// Connect establishes a WebTransport session and sends the join message.
// Callbacks must be registered via Set* methods before calling Connect.
func (t *Transport) Connect(ctx context.Context, addr, username string) error {
	ctx, cancel := context.WithCancel(ctx)
	t.mu.Lock()
	t.cancel = cancel
	t.mu.Unlock()

	d := webtransport.Dialer{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec — self-signed server cert
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

	stream, err := sess.OpenStream()
	if err != nil {
		cancel()
		sess.CloseWithError(0, "failed to open control stream")
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
	t.lastPongTime.Store(time.Now().UnixNano()) // baseline: treat connection start as a pong
	t.metricsMu.Lock()
	t.lastMetricsTime = time.Now()
	t.metricsMu.Unlock()

	t.writeCtrl(ControlMsg{Type: "join", Username: username})

	go t.readControl(ctx, stream)
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

// SendAudio sends an encoded Opus frame as an unreliable datagram.
// The datagram header is: [userID:2][seq:2][opus_payload].
func (t *Transport) SendAudio(opusData []byte) error {
	t.mu.Lock()
	sess := t.session
	myID := t.myID
	t.mu.Unlock()

	if sess == nil {
		return nil
	}

	seq := uint16(t.seq.Add(1))

	dgram := make([]byte, 4+len(opusData))
	binary.BigEndian.PutUint16(dgram[0:2], myID)
	binary.BigEndian.PutUint16(dgram[2:4], seq)
	copy(dgram[4:], opusData)

	t.bytesSent.Add(uint64(len(dgram)))
	return sess.SendDatagram(dgram)
}

// MyID returns the local client's server-assigned user ID (0 before join ack).
func (t *Transport) MyID() uint16 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.myID
}

// StartReceiving pumps incoming datagrams to playbackCh in a background goroutine.
// Each datagram payload is the raw Opus bytes (header stripped).
func (t *Transport) StartReceiving(ctx context.Context, playbackCh chan<- []byte) {
	go func() {
		speakTimers := make(map[uint16]time.Time)
		lastSeq := make(map[uint16]uint16) // senderID → last received seq

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

			userID, seq, opusData, ok := ParseDatagram(data)
			if !ok {
				continue
			}

			// Sequence-gap packet loss accounting.
			if prev, ok := lastSeq[userID]; ok {
				diff := int(seq) - int(prev)
				if diff < 0 {
					diff += 65536 // uint16 wraparound
				}
				if diff > 0 {
					t.expectedPackets.Add(uint64(diff))
					if diff > 1 {
						t.lostPackets.Add(uint64(diff - 1))
					}
				}
			}
			lastSeq[userID] = seq

			// Speaking notification, throttled per user to ~80 ms.
			t.cbMu.RLock()
			onAudio := t.onAudioReceived
			t.cbMu.RUnlock()
			if onAudio != nil {
				if last, ok := speakTimers[userID]; !ok || time.Since(last) > 80*time.Millisecond {
					speakTimers[userID] = time.Now()
					onAudio(userID)
				}
			}

			select {
			case playbackCh <- opusData:
			default:
			}
		}
	}()
}

// GetMetrics returns current connection quality metrics and resets interval counters.
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

// pongTimeout is the maximum time allowed between pongs before the connection
// is considered dead and the client disconnects. 5 missed pings at 2 s each.
const pongTimeout = 10 * time.Second

// pingLoop sends a ping every 2 s for RTT measurement and enforces a pong
// deadline. If no pong arrives within pongTimeout, the session is closed.
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

			// Check pong deadline. lastPongTime is set to connection-start in
			// Connect(), so this is only a timeout if the server stops responding.
			lastPong := t.lastPongTime.Load()
			if lastPong > 0 && time.Since(time.Unix(0, lastPong)) > pongTimeout {
				log.Printf("[transport] pong timeout — server unreachable, disconnecting")
				t.Disconnect()
				return
			}
		}
	}
}

// readControl reads newline-delimited JSON control messages from the server.
// It fires the registered callbacks and updates metrics. When the stream
// closes (server disconnect), it calls onDisconnected.
func (t *Transport) readControl(ctx context.Context, stream *webtransport.Stream) {
	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		var msg ControlMsg
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Printf("[transport] invalid control msg: %v", err)
			continue
		}

		t.cbMu.RLock()
		onUserList := t.onUserList
		onUserJoined := t.onUserJoined
		onUserLeft := t.onUserLeft
		t.cbMu.RUnlock()

		switch msg.Type {
		case "user_list":
			// The server appends the joining user last in the list; that entry
			// carries our assigned ID.
			if len(msg.Users) > 0 {
				t.mu.Lock()
				t.myID = msg.Users[len(msg.Users)-1].ID
				t.mu.Unlock()
			}
			if onUserList != nil {
				onUserList(msg.Users)
			}
		case "user_joined":
			if onUserJoined != nil {
				onUserJoined(msg.ID, msg.Username)
			}
		case "user_left":
			if onUserLeft != nil {
				onUserLeft(msg.ID)
			}
		case "pong":
			t.lastPongTime.Store(time.Now().UnixNano())
			sent := t.lastPingTs.Load()
			if sent != 0 {
				sample := float64(time.Now().UnixMilli() - sent)
				old := math.Float64frombits(t.smoothedRTT.Load())
				var next float64
				if old == 0 {
					next = sample
				} else {
					next = 0.125*sample + 0.875*old // EWMA α=0.125 (RFC 6298)
				}
				t.smoothedRTT.Store(math.Float64bits(next))
			}
		}
	}

	t.cbMu.RLock()
	onDisconnected := t.onDisconnected
	t.cbMu.RUnlock()
	if onDisconnected != nil {
		onDisconnected()
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

// ParseDatagram parses a voice datagram header. Exported for testing.
// The returned opus slice aliases data — copy if you need to retain it.
func ParseDatagram(data []byte) (userID, seq uint16, opus []byte, ok bool) {
	if len(data) < 4 {
		return 0, 0, nil, false
	}
	userID = binary.BigEndian.Uint16(data[0:2])
	seq = binary.BigEndian.Uint16(data[2:4])
	opus = data[4:]
	return userID, seq, opus, true
}
