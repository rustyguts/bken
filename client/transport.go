package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/webtransport-go"
)

// mutedSet is a concurrent set of uint16 user IDs.
type mutedSet struct{ m sync.Map }

func (ms *mutedSet) Add(id uint16)    { ms.m.Store(id, struct{}{}) }
func (ms *mutedSet) Remove(id uint16) { ms.m.Delete(id) }
func (ms *mutedSet) Has(id uint16) bool {
	_, ok := ms.m.Load(id)
	return ok
}
func (ms *mutedSet) Clear() {
	ms.m.Range(func(k, _ any) bool { ms.m.Delete(k); return true })
}
func (ms *mutedSet) Slice() []uint16 {
	var out []uint16
	ms.m.Range(func(k, _ any) bool { out = append(out, k.(uint16)); return true })
	return out
}

// ControlMsg mirrors the server's control message format.
type ControlMsg struct {
	Type       string        `json:"type"`
	Username   string        `json:"username,omitempty"`
	ID         uint16        `json:"id,omitempty"`
	Users      []UserInfo    `json:"users,omitempty"`
	Ts         int64         `json:"ts,omitempty"`          // ping/pong timestamp (Unix ms)
	Message    string        `json:"message,omitempty"`     // chat: body text
	ServerName string        `json:"server_name,omitempty"` // user_list: human-readable server name
	OwnerID    uint16        `json:"owner_id,omitempty"`    // user_list/owner_changed: current room owner
	ChannelID  int64         `json:"channel_id,omitempty"`  // join_channel/user_channel: target channel
	Channels   []ChannelInfo `json:"channels,omitempty"`    // channel_list: full list of channels
	APIPort    int           `json:"api_port,omitempty"`    // user_list: HTTP API port for file uploads
	FileID     int64         `json:"file_id,omitempty"`     // chat: uploaded file DB id
	FileName   string        `json:"file_name,omitempty"`   // chat: original filename
	FileSize   int64         `json:"file_size,omitempty"`   // chat: file size in bytes
	MsgID      uint64        `json:"msg_id,omitempty"`      // chat/link_preview: server-assigned message ID
	LinkURL    string        `json:"link_url,omitempty"`    // link_preview: the URL that was fetched
	LinkTitle  string        `json:"link_title,omitempty"`  // link_preview: page title
	LinkDesc   string        `json:"link_desc,omitempty"`   // link_preview: page description
	LinkImage  string        `json:"link_image,omitempty"`  // link_preview: preview image URL
	LinkSite   string        `json:"link_site,omitempty"`   // link_preview: site name
	Seqs       []uint16      `json:"seqs,omitempty"`        // nack: missing sequence numbers for retransmission
}

// UserInfo describes a connected peer.
type UserInfo struct {
	ID        uint16 `json:"id"`
	Username  string `json:"username"`
	ChannelID int64  `json:"channel_id,omitempty"` // 0 = not in any channel
}

// ChannelInfo describes a voice channel.
type ChannelInfo struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Metrics holds connection quality metrics shown in the UI.
type Metrics struct {
	RTTMs           float64 `json:"rtt_ms"`
	PacketLoss      float64 `json:"packet_loss"`       // 0.0–1.0
	JitterMs        float64 `json:"jitter_ms"`         // inter-arrival jitter (smoothed)
	BitrateKbps     float64 `json:"bitrate_kbps"`      // measured outgoing audio
	OpusTargetKbps  int     `json:"opus_target_kbps"`  // current encoder target
	QualityLevel    string  `json:"quality_level"`     // "good", "moderate", or "poor"
	CaptureDropped  uint64  `json:"capture_dropped"`   // frames dropped on send side since last tick
	PlaybackDropped uint64  `json:"playback_dropped"`  // frames dropped on recv side since last tick
}

// qualityLevel classifies connection quality from metrics.
// Thresholds: good (loss<2%, RTT<100ms, jitter<20ms, drops<1/s),
// moderate (loss<10%, RTT<300ms, jitter<50ms, drops<5/s), poor (everything else).
// dropRate is the combined capture+playback drops per second.
func qualityLevel(loss, rttMs, jitterMs, dropRate float64) string {
	if loss >= 0.10 || rttMs >= 300 || jitterMs >= 50 || dropRate >= 5 {
		return "poor"
	}
	if loss >= 0.02 || rttMs >= 100 || jitterMs >= 20 || dropRate >= 1 {
		return "moderate"
	}
	return "good"
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

	// Inter-arrival jitter: EWMA of |actual_gap - 20ms| across all senders,
	// stored as float64 bits for atomic access. Units: milliseconds.
	smoothedJitter atomic.Uint64

	// Dropped frame counters: incremented when the playback channel is full
	// and a received frame cannot be delivered.
	playbackDropped atomic.Uint64

	// muted holds the set of remote user IDs whose audio is suppressed locally.
	muted mutedSet

	// recvCancel cancels the current StartReceiving goroutine (if any).
	// Protected by mu; set in StartReceiving, called in Disconnect and
	// before spawning a replacement goroutine.
	recvCancel context.CancelFunc

	// disconnectReason is set before Disconnect is called to communicate the
	// cause to the onDisconnected callback. Protected by mu.
	disconnectReason string

	// lastMetricsTime is the timestamp of the previous GetMetrics call.
	metricsMu       sync.Mutex
	lastMetricsTime time.Time

	// serverAddr is the WebTransport address passed to Connect (e.g. "192.168.1.5:4433").
	// Used to derive the HTTP API base URL from the api_port in user_list.
	serverAddr string // protected by mu

	// apiBaseURL is the HTTP base URL for the server's REST API (e.g. "http://host:8080").
	// Set from the api_port field in the user_list welcome message.
	apiBaseURL string // protected by mu

	// Callbacks — set via setters before calling Connect.
	cbMu                 sync.RWMutex
	onUserList           func([]UserInfo)
	onUserJoined         func(uint16, string)
	onUserLeft           func(uint16)
	onAudioReceived      func(uint16)
	onDisconnected       func(reason string)
	onChatMessage        func(msgID uint64, username, message string, ts int64, fileID int64, fileName string, fileSize int64)
	onChannelChatMessage func(msgID uint64, channelID int64, username, message string, ts int64, fileID int64, fileName string, fileSize int64)
	onServerInfo         func(name string)
	onKicked             func()
	onOwnerChanged       func(ownerID uint16)
	onChannelList        func([]ChannelInfo)
	onUserChannel        func(userID uint16, channelID int64)
	onLinkPreview        func(msgID uint64, channelID int64, url, title, desc, image, siteName string)
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

func (t *Transport) SetOnDisconnected(fn func(reason string)) {
	t.cbMu.Lock()
	t.onDisconnected = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnChatMessage(fn func(msgID uint64, username, message string, ts int64, fileID int64, fileName string, fileSize int64)) {
	t.cbMu.Lock()
	t.onChatMessage = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnChannelChatMessage(fn func(msgID uint64, channelID int64, username, message string, ts int64, fileID int64, fileName string, fileSize int64)) {
	t.cbMu.Lock()
	t.onChannelChatMessage = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnServerInfo(fn func(name string)) {
	t.cbMu.Lock()
	t.onServerInfo = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnKicked(fn func()) {
	t.cbMu.Lock()
	t.onKicked = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnOwnerChanged(fn func(ownerID uint16)) {
	t.cbMu.Lock()
	t.onOwnerChanged = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnChannelList(fn func([]ChannelInfo)) {
	t.cbMu.Lock()
	t.onChannelList = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnUserChannel(fn func(userID uint16, channelID int64)) {
	t.cbMu.Lock()
	t.onUserChannel = fn
	t.cbMu.Unlock()
}

func (t *Transport) SetOnLinkPreview(fn func(msgID uint64, channelID int64, url, title, desc, image, siteName string)) {
	t.cbMu.Lock()
	t.onLinkPreview = fn
	t.cbMu.Unlock()
}

// --- Per-user local muting ---

// MuteUser suppresses incoming audio from the given remote user ID.
func (t *Transport) MuteUser(id uint16) { t.muted.Add(id) }

// UnmuteUser re-enables incoming audio from the given remote user ID.
func (t *Transport) UnmuteUser(id uint16) { t.muted.Remove(id) }

// IsUserMuted reports whether audio from id is currently suppressed.
func (t *Transport) IsUserMuted(id uint16) bool { return t.muted.Has(id) }

// MutedUsers returns the IDs of all currently muted remote users.
func (t *Transport) MutedUsers() []uint16 { return t.muted.Slice() }

// KickUser sends a kick request to the server. Only succeeds if the caller is
// the room owner; the server enforces the authorisation check.
func (t *Transport) KickUser(id uint16) error {
	return t.writeCtrl(ControlMsg{Type: "kick", ID: id})
}

// RenameServer sends a rename request to the server. Only succeeds if the
// caller is the room owner; the server enforces the authorisation check.
func (t *Transport) RenameServer(name string) error {
	return t.writeCtrl(ControlMsg{Type: "rename", ServerName: name})
}

// JoinChannel sends a join_channel request to the server.
// Pass channelID=0 to leave all channels (return to lobby).
func (t *Transport) JoinChannel(id int64) error {
	return t.writeCtrl(ControlMsg{Type: "join_channel", ChannelID: id})
}

// CreateChannel asks the server to create a new channel with the given name.
// Only succeeds if the caller is the room owner; the server enforces the check.
func (t *Transport) CreateChannel(name string) error {
	return t.writeCtrl(ControlMsg{Type: "create_channel", Message: name})
}

// RenameChannel asks the server to rename a channel.
// Only succeeds if the caller is the room owner; the server enforces the check.
func (t *Transport) RenameChannel(id int64, name string) error {
	return t.writeCtrl(ControlMsg{Type: "rename_channel", ChannelID: id, Message: name})
}

// DeleteChannel asks the server to delete a channel.
// Only succeeds if the caller is the room owner; the server enforces the check.
func (t *Transport) DeleteChannel(id int64) error {
	return t.writeCtrl(ControlMsg{Type: "delete_channel", ChannelID: id})
}

// MoveUser asks the server to move a user to a different channel.
// Only succeeds if the caller is the room owner; the server enforces the check.
func (t *Transport) MoveUser(userID uint16, channelID int64) error {
	return t.writeCtrl(ControlMsg{Type: "move_user", ID: userID, ChannelID: channelID})
}

// APIBaseURL returns the HTTP base URL for the server's REST API, or "" if not yet known.
func (t *Transport) APIBaseURL() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.apiBaseURL
}

// SendFileChat sends a chat message with a file attachment.
// The file metadata must come from a prior upload to the server's API.
func (t *Transport) SendFileChat(channelID, fileID, fileSize int64, fileName, message string) error {
	if len(message) > 500 {
		return fmt.Errorf("message must not exceed 500 characters")
	}
	return t.writeCtrl(ControlMsg{
		Type:      "chat",
		Message:   message,
		ChannelID: channelID,
		FileID:    fileID,
		FileName:  fileName,
		FileSize:  fileSize,
	})
}

// SendChannelChat sends a channel-scoped chat message. The server routes it
// only to users currently in the sender's channel. If the caller is not in a
// channel, the server falls back to global broadcast.
func (t *Transport) SendChannelChat(channelID int64, message string) error {
	if err := validateChat(message); err != nil {
		return err
	}
	return t.writeCtrl(ControlMsg{Type: "chat", Message: message, ChannelID: channelID})
}

// SendChat sends a chat message to the server for fan-out to all participants.
func (t *Transport) SendChat(message string) error {
	if err := validateChat(message); err != nil {
		return err
	}
	return t.writeCtrl(ControlMsg{Type: "chat", Message: message})
}

// validateChat returns an error if the message is empty or too long.
func validateChat(message string) error {
	if message == "" {
		return fmt.Errorf("message must not be empty")
	}
	if len(message) > 500 {
		return fmt.Errorf("message must not exceed 500 characters")
	}
	return nil
}

// writeCtrl serialises a control message write; safe for concurrent callers.
func (t *Transport) writeCtrl(msg ControlMsg) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	t.ctrlMu.Lock()
	defer t.ctrlMu.Unlock()
	if t.ctrl == nil {
		return fmt.Errorf("control stream not connected")
	}
	_, err = t.ctrl.Write(data)
	return err
}

// writeCtrlBestEffort sends a control message without returning errors.
// Used for non-critical messages (pings) where failure is handled elsewhere.
func (t *Transport) writeCtrlBestEffort(msg ControlMsg) {
	if err := t.writeCtrl(msg); err != nil {
		log.Printf("[transport] best-effort write (%s): %v", msg.Type, err)
	}
}

// connectTimeout is the maximum time allowed for the initial WebTransport
// dial + control stream open + join handshake.
const connectTimeout = 10 * time.Second

// Connect establishes a WebTransport session and sends the join message.
// Callbacks must be registered via Set* methods before calling Connect.
func (t *Transport) Connect(ctx context.Context, addr, username string) error {
	// Reset per-session state.
	t.muted.Clear()
	t.mu.Lock()
	t.disconnectReason = ""
	t.serverAddr = addr
	t.apiBaseURL = ""
	t.mu.Unlock()

	// Apply a dial timeout so the caller isn't blocked indefinitely when the
	// server is unreachable. The timeout only covers the handshake; once
	// connected the session-scoped context takes over.
	dialCtx, dialCancel := context.WithTimeout(ctx, connectTimeout)
	defer dialCancel()

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

	_, sess, err := d.Dial(dialCtx, "https://"+addr, http.Header{})
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
	t.smoothedJitter.Store(0)
	t.bytesSent.Store(0)
	t.lostPackets.Store(0)
	t.expectedPackets.Store(0)
	t.lastPongTime.Store(time.Now().UnixNano()) // baseline: treat connection start as a pong
	t.metricsMu.Lock()
	t.lastMetricsTime = time.Now()
	t.metricsMu.Unlock()

	if err := t.writeCtrl(ControlMsg{Type: "join", Username: username}); err != nil {
		cancel()
		sess.CloseWithError(0, "failed to send join")
		return fmt.Errorf("send join: %w", err)
	}

	go t.readControl(ctx, stream)
	go t.pingLoop(ctx)

	return nil
}

// Disconnect closes the WebTransport session.
func (t *Transport) Disconnect() {
	t.ctrlMu.Lock()
	if t.ctrl != nil {
		t.ctrl.Close() //nolint:errcheck // best-effort close for fast server-side teardown
		t.ctrl = nil
	}
	t.ctrlMu.Unlock()

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.recvCancel != nil {
		t.recvCancel()
		t.recvCancel = nil
	}
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	if t.session != nil {
		t.session.CloseWithError(0, "disconnect")
		t.session = nil
	}
	t.myID = 0
}

// dgramPool reuses datagram buffers on the voice send hot path.
// Each buffer is pre-allocated to the maximum datagram size (4-byte header +
// 1275-byte max Opus packet). quic-go's SendDatagram copies the data
// internally, so the buffer can be returned to the pool immediately after
// the call returns.
//
// Stored as *[]byte (not []byte) so the pointer fits in the interface word
// and Get/Put avoid the per-call allocation from boxing a 3-word slice header.
var dgramPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 4+opusMaxPacketBytes)
		return &buf
	},
}

// SendAudio sends an encoded Opus frame as an unreliable datagram.
// The datagram header is: [userID:2][seq:2][opus_payload].
// Buffers are recycled via sync.Pool to avoid per-frame allocations (50/s).
func (t *Transport) SendAudio(opusData []byte) error {
	t.mu.Lock()
	sess := t.session
	myID := t.myID
	t.mu.Unlock()

	if sess == nil {
		return nil
	}

	seq := uint16(t.seq.Add(1))
	dgramLen := 4 + len(opusData)

	bp := dgramPool.Get().(*[]byte)
	dgram := (*bp)[:dgramLen]
	binary.BigEndian.PutUint16(dgram[0:2], myID)
	binary.BigEndian.PutUint16(dgram[2:4], seq)
	copy(dgram[4:], opusData)

	t.bytesSent.Add(uint64(dgramLen))
	err := sess.SendDatagram(dgram)
	dgramPool.Put(bp)
	return err
}

// MyID returns the local client's server-assigned user ID (0 before join ack).
func (t *Transport) MyID() uint16 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.myID
}

// StartReceiving pumps incoming datagrams to playbackCh in a background goroutine.
// Each TaggedAudio carries the sender ID, sequence number, and raw Opus payload
// so the audio engine can feed its per-sender jitter buffer.
// Calling StartReceiving again cancels the previous goroutine before spawning a
// new one, preventing duplicate readers on the same session.
func (t *Transport) StartReceiving(ctx context.Context, playbackCh chan<- TaggedAudio) {
	t.mu.Lock()
	// Cancel any existing receive goroutine so we never have two readers.
	if t.recvCancel != nil {
		t.recvCancel()
	}
	sess := t.session
	t.mu.Unlock()
	if sess == nil {
		return
	}

	rctx, cancel := context.WithCancel(ctx)
	t.mu.Lock()
	t.recvCancel = cancel
	t.mu.Unlock()

	go func() {
		defer cancel()
		speakTimers := make(map[uint16]time.Time)
		lastSeq := make(map[uint16]uint16)      // senderID → last received seq
		hasSeq := make(map[uint16]bool)          // whether lastSeq contains a valid entry
		lastSeen := make(map[uint16]time.Time)   // senderID → last packet time
		lastArrival := make(map[uint16]time.Time) // senderID → arrival time of previous packet
		var pruneCounter int

		const expectedGapMs = 20.0 // one Opus frame = 20 ms
		const jitterAlpha = 1.0 / 16.0 // RFC 3550 jitter gain
		const maxNACKGap = 5 // only NACK small gaps; larger = sustained loss, let FEC/PLC handle

		for {
			data, err := sess.ReceiveDatagram(rctx)
			if err != nil {
				return
			}

			userID, seq, opusData, ok := ParseDatagram(data)
			if !ok {
				continue
			}

			// Drop audio from locally muted users before any further processing.
			if t.muted.Has(userID) {
				continue
			}

			now := time.Now()
			lastSeen[userID] = now

			// Sequence-gap packet loss accounting. Only count forward progress
			// (diff in [1, 1000)) to avoid corrupting metrics when retransmitted
			// or reordered packets arrive with older sequence numbers.
			forwardProgress := false
			if prev, has := lastSeq[userID]; has && hasSeq[userID] {
				diff := int(seq) - int(prev)
				if diff < 0 {
					diff += 65536 // uint16 wraparound
				}
				if diff > 0 && diff < 1000 {
					forwardProgress = true
					lastSeq[userID] = seq
					t.expectedPackets.Add(uint64(diff))
					if diff > 1 {
						t.lostPackets.Add(uint64(diff - 1))
						// NACK missing packets for small gaps. On LAN with
						// <1ms RTT, retransmissions arrive well within the
						// jitter buffer window (20ms+), giving 100% recovery
						// vs the ~80% quality of FEC/PLC.
						if diff <= maxNACKGap+1 {
							seqs := make([]uint16, 0, diff-1)
							for i := 1; i < diff; i++ {
								seqs = append(seqs, prev+uint16(i))
							}
							go t.writeCtrlBestEffort(ControlMsg{Type: "nack", ID: userID, Seqs: seqs})
						}
					}
				}
				// else: retransmitted/reordered packet — deliver to jitter
				// buffer without updating lastSeq or loss counters.
			} else {
				forwardProgress = true
				lastSeq[userID] = seq
				hasSeq[userID] = true
			}

			// Inter-arrival jitter: only measure on forward-progress packets.
			// Retransmissions would have artificial inter-arrival times that
			// inflate the jitter estimate.
			if forwardProgress {
				if prev, ok := lastArrival[userID]; ok {
					gapMs := float64(now.Sub(prev).Microseconds()) / 1000.0
					if gapMs < 100.0 {
						d := gapMs - expectedGapMs
						if d < 0 {
							d = -d
						}
						old := math.Float64frombits(t.smoothedJitter.Load())
						next := old + jitterAlpha*(d-old)
						t.smoothedJitter.Store(math.Float64bits(next))
					}
				}
				lastArrival[userID] = now
			}

			// Speaking notification, throttled per user to ~80 ms.
			t.cbMu.RLock()
			onAudio := t.onAudioReceived
			t.cbMu.RUnlock()
			if onAudio != nil {
				if last, ok := speakTimers[userID]; !ok || now.Sub(last) > 80*time.Millisecond {
					speakTimers[userID] = now
					onAudio(userID)
				}
			}

			// Prune stale entries every ~500 packets (~10 s of audio) to prevent
			// unbounded map growth from disconnected speakers.
			pruneCounter++
			if pruneCounter >= 500 {
				pruneCounter = 0
				for id, seen := range lastSeen {
					if now.Sub(seen) > 30*time.Second {
						delete(lastSeen, id)
						delete(lastSeq, id)
						delete(hasSeq, id)
						delete(speakTimers, id)
						delete(lastArrival, id)
					}
				}
			}

			select {
			case playbackCh <- TaggedAudio{SenderID: userID, Seq: seq, OpusData: opusData}:
			default:
				t.playbackDropped.Add(1)
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
	jitterMs := math.Float64frombits(t.smoothedJitter.Load())
	playbackDrops := t.playbackDropped.Swap(0)

	return Metrics{
		RTTMs:           rtt,
		PacketLoss:      loss,
		JitterMs:        jitterMs,
		BitrateKbps:     bitrate,
		PlaybackDropped: playbackDrops,
		// QualityLevel is set by adaptBitrateLoop after merging drop counters.
		// When called outside the loop (e.g. polling), use network metrics only.
		QualityLevel: qualityLevel(loss, rtt, jitterMs, 0),
	}
}

// pongTimeout is the maximum time allowed between pongs before the connection
// is considered dead and the client disconnects. 3 missed pings at 2 s each.
const pongTimeout = 6 * time.Second

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
			t.writeCtrlBestEffort(ControlMsg{Type: "ping", Ts: ts})

			// Check pong deadline. lastPongTime is set to connection-start in
			// Connect(), so this is only a timeout if the server stops responding.
			lastPong := t.lastPongTime.Load()
			if lastPong > 0 && time.Since(time.Unix(0, lastPong)) > pongTimeout {
				log.Printf("[transport] pong timeout — server unreachable, disconnecting")
				t.mu.Lock()
				t.disconnectReason = "Server unreachable (ping timeout)"
				t.mu.Unlock()
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
		onChat := t.onChatMessage
		onChannelChat := t.onChannelChatMessage
		onServerInfo := t.onServerInfo
		onKicked := t.onKicked
		onOwnerChanged := t.onOwnerChanged
		onChannelList := t.onChannelList
		onUserChannel := t.onUserChannel
		onLinkPreview := t.onLinkPreview
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
			// Build the API base URL from the server address host + the advertised API port.
			if msg.APIPort != 0 {
				t.mu.Lock()
				host, _, err := net.SplitHostPort(t.serverAddr)
				if err != nil {
					host = t.serverAddr
				}
				t.apiBaseURL = fmt.Sprintf("http://%s:%d", host, msg.APIPort)
				t.mu.Unlock()
			}
			if onUserList != nil {
				onUserList(msg.Users)
			}
			if msg.ServerName != "" && onServerInfo != nil {
				onServerInfo(msg.ServerName)
			}
			if onOwnerChanged != nil {
				onOwnerChanged(msg.OwnerID)
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
		case "chat":
			if msg.ChannelID != 0 {
				if onChannelChat != nil {
					onChannelChat(msg.MsgID, msg.ChannelID, msg.Username, msg.Message, msg.Ts, msg.FileID, msg.FileName, msg.FileSize)
				}
			} else {
				if onChat != nil {
					onChat(msg.MsgID, msg.Username, msg.Message, msg.Ts, msg.FileID, msg.FileName, msg.FileSize)
				}
			}
		case "link_preview":
			if onLinkPreview != nil {
				onLinkPreview(msg.MsgID, msg.ChannelID, msg.LinkURL, msg.LinkTitle, msg.LinkDesc, msg.LinkImage, msg.LinkSite)
			}
		case "server_info":
			if msg.ServerName != "" && onServerInfo != nil {
				onServerInfo(msg.ServerName)
			}
		case "owner_changed":
			if onOwnerChanged != nil {
				onOwnerChanged(msg.OwnerID)
			}
		case "kicked":
			if onKicked != nil {
				onKicked()
			}
		case "channel_list":
			if onChannelList != nil {
				onChannelList(msg.Channels)
			}
		case "user_channel":
			if onUserChannel != nil {
				onUserChannel(msg.ID, msg.ChannelID)
			}
		}
	}

	// Determine disconnect reason: if one was set (e.g. by pingLoop), use it;
	// otherwise default to a generic message.
	t.mu.Lock()
	reason := t.disconnectReason
	t.disconnectReason = ""
	t.mu.Unlock()
	if reason == "" {
		reason = "Connection closed by server"
	}

	t.cbMu.RLock()
	onDisconnected := t.onDisconnected
	t.cbMu.RUnlock()
	if onDisconnected != nil {
		onDisconnected(reason)
	}
}

// TaggedAudio is a voice frame tagged with the sender's ID and sequence number.
// Used to feed the per-sender jitter buffer in the audio engine.
type TaggedAudio struct {
	SenderID uint16
	Seq      uint16
	OpusData []byte
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
