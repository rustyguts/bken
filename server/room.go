package main

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DatagramSender is the minimal interface needed to send a datagram.
// Using an interface here lets tests inject a mock sender.
type DatagramSender interface {
	SendDatagram([]byte) error
}

// maxMsgOwners is the maximum number of message→sender mappings to retain.
// Once exceeded, the oldest entries are evicted. 10 000 messages ≈ a few hours
// of active chat, which is more than enough for an ephemeral in-memory server.
const maxMsgOwners = 10000

// maxPinnedPerChannel is the maximum number of pinned messages per channel.
const maxPinnedPerChannel = 25

// storedMsg holds a message in memory for search, reply preview, and pin support.
type storedMsg struct {
	MsgID     uint64
	SenderID  uint16
	Username  string
	Message   string
	ChannelID int64
	Timestamp int64
	Deleted   bool
}

// reaction tracks a single user's reaction on a message.
type reaction struct {
	UserID uint16
	Emoji  string
}

// pinnedEntry tracks a pinned message.
type pinnedEntry struct {
	MsgID     uint64
	ChannelID int64
	PinnedBy  uint16
}

// maxMsgBuffer is the max messages buffered per channel for replay on reconnect.
const maxMsgBuffer = 500

// Room holds all connected clients and handles voice datagram fan-out.
type Room struct {
	mu         sync.RWMutex
	clients    map[uint16]*Client
	serverName string             // protected by mu
	ownerID    uint16             // ID of the current room owner; 0 = no owner; protected by mu
	onRename   func(string) error // optional persistence callback, fired after Rename; protected by mu
	channels   []ChannelInfo      // cached channel list sent to newly-connecting clients; protected by mu
	apiPort    int                // HTTP API port communicated to clients in user_list; protected by mu
	iceServers []ICEServerInfo    // ICE servers (STUN/TURN) sent to clients in user_list; protected by mu
	nextID     atomic.Uint32
	nextMsgID  atomic.Uint64

	// msgOwners maps server-assigned message IDs to the sender's client ID.
	// Used to authorise edit_message/delete_message requests. Protected by mu.
	msgOwners    map[uint64]uint16
	msgOwnerKeys []uint64 // insertion order for bounded eviction

	// msgStore holds recent messages for search, reply preview, and pin support. Protected by mu.
	msgStore    map[uint64]*storedMsg
	msgStoreKeys []uint64 // insertion order for bounded eviction

	// reactions tracks emoji reactions per message. Protected by mu.
	reactions map[uint64][]reaction

	// pinnedMsgs tracks pinned messages per channel. Protected by mu.
	pinnedMsgs []pinnedEntry

	// Channel CRUD persistence callbacks — set via setters; called outside the mutex.
	onCreateChannel   func(name string) (int64, error)
	onRenameChannel   func(id int64, name string) error
	onDeleteChannel   func(id int64) error
	onRefreshChannels func() ([]ChannelInfo, error)

	// Phase 8: Administration
	onAuditLog     func(actorID int, actorName, action, target, details string) // audit log callback
	onBan          func(pubkey, ip, reason, bannedBy string, durationS int)     // ban callback
	onUnban        func(banID int64)                                             // unban callback
	announcement   string                                                        // current announcement content; protected by mu
	announceUser   string                                                        // who posted the announcement; protected by mu
	slowModes      map[int64]int                                                 // channelID -> cooldown seconds; protected by mu

	// Phase 10: Performance & Reliability
	maxConnections int                 // max concurrent connections (0=unlimited)
	perIPLimit     int                 // max connections per IP (0=unlimited)
	ipConnections  map[string]int      // IP -> current connection count; protected by mu
	controlRateLimit int              // max control messages per second per client (0=unlimited)
	channelSeqs    map[int64]uint64    // channel -> last sequence number; protected by mu
	msgBuffer      map[int64][]ControlMsg // channel -> recent messages for replay; protected by mu

	// Phase 7: Server-side recording
	recordings map[int64]*ChannelRecorder // channelID -> active recorder; protected by mu
	doneRecs   []RecordingInfo            // completed recordings; protected by mu
	dataDir    string                     // base data directory for recording files

	// Metrics (reset on each Stats call).
	totalDatagrams   atomic.Uint64
	totalBytes       atomic.Uint64
	skippedDatagrams atomic.Uint64 // sends skipped by per-client circuit breakers
}

func NewRoom() *Room {
	return &Room{
		clients:       make(map[uint16]*Client),
		msgOwners:     make(map[uint64]uint16),
		msgStore:      make(map[uint64]*storedMsg),
		reactions:     make(map[uint64][]reaction),
		slowModes:     make(map[int64]int),
		ipConnections: make(map[string]int),
		channelSeqs:   make(map[int64]uint64),
		msgBuffer:     make(map[int64][]ControlMsg),
		recordings:    make(map[int64]*ChannelRecorder),
	}
}

// SetDataDir sets the base data directory for recordings and other file storage.
func (r *Room) SetDataDir(dir string) {
	r.mu.Lock()
	r.dataDir = dir
	r.mu.Unlock()
}

// DataDir returns the base data directory.
func (r *Room) DataDir() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dataDir
}

// SetServerName updates the human-readable server name sent to connecting clients.
func (r *Room) SetServerName(name string) {
	r.mu.Lock()
	r.serverName = name
	r.mu.Unlock()
}

// ServerName returns the current server name.
func (r *Room) ServerName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.serverName
}

// SetAPIPort sets the HTTP API port communicated to clients in the welcome message.
func (r *Room) SetAPIPort(port int) {
	r.mu.Lock()
	r.apiPort = port
	r.mu.Unlock()
}

// APIPort returns the configured HTTP API port.
func (r *Room) APIPort() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.apiPort
}

// SetICEServers sets the STUN/TURN ICE servers communicated to clients.
func (r *Room) SetICEServers(servers []ICEServerInfo) {
	r.mu.Lock()
	r.iceServers = servers
	r.mu.Unlock()
}

// ICEServers returns the configured ICE servers.
func (r *Room) ICEServers() []ICEServerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.iceServers
}

// SetOnRename registers a callback invoked after a successful Rename.
// Intended for persisting the name to a store; called outside the mutex.
func (r *Room) SetOnRename(fn func(string) error) {
	r.mu.Lock()
	r.onRename = fn
	r.mu.Unlock()
}

// Rename updates the server name and fires the onRename callback if set.
// Returns an error if the persistence callback fails; in that case the
// in-memory name is still updated.
func (r *Room) Rename(name string) error {
	r.mu.Lock()
	r.serverName = name
	cb := r.onRename
	r.mu.Unlock()
	if cb != nil {
		return cb(name)
	}
	return nil
}

// ClaimOwnership sets id as the room owner if no owner is currently set.
// Returns true if this call made the client the owner.
func (r *Room) ClaimOwnership(id uint16) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ownerID == 0 {
		r.ownerID = id
		return true
	}
	return false
}

// OwnerID returns the ID of the current room owner (0 if none).
func (r *Room) OwnerID() uint16 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ownerID
}

// TransferOwnership removes ownership from leavingID and assigns it to the
// remaining client with the lowest ID. Returns the new owner ID and whether
// ownership actually changed. Call after RemoveClient so the leaving client
// is no longer in the clients map.
func (r *Room) TransferOwnership(leavingID uint16) (newOwnerID uint16, changed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ownerID != leavingID {
		return r.ownerID, false // leaving client was not the owner
	}
	r.ownerID = 0
	for id := range r.clients {
		if r.ownerID == 0 || id < r.ownerID {
			r.ownerID = id
		}
	}
	return r.ownerID, true
}

// GetClient returns the client with the given ID, or nil if not found.
func (r *Room) GetClient(id uint16) *Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.clients[id]
}

// AddClient registers a client, assigns it a unique ID, and returns that ID.
func (r *Room) AddClient(c *Client) uint16 {
	id := uint16(r.nextID.Add(1))
	c.ID = id

	r.mu.Lock()
	r.clients[id] = c
	r.mu.Unlock()

	log.Printf("[room] client %d (%s) joined, total=%d", id, c.Username, r.ClientCount())
	return id
}

// AddOrReplaceClient registers c and atomically removes any existing client
// with the same username (case-insensitive). It always assigns a fresh ID to c.
// Returns the new ID, plus the replaced client and ID if a replacement occurred.
func (r *Room) AddOrReplaceClient(c *Client) (id uint16, replaced *Client, replacedID uint16) {
	id = uint16(r.nextID.Add(1))
	c.ID = id

	r.mu.Lock()
	for existingID, existing := range r.clients {
		if strings.EqualFold(existing.Username, c.Username) {
			replaced = existing
			replacedID = existingID
			delete(r.clients, existingID)
			break
		}
	}
	r.clients[id] = c
	total := len(r.clients)
	r.mu.Unlock()

	if replaced != nil {
		log.Printf("[room] replaced client %d (%s) with %d (%s), total=%d",
			replacedID, replaced.Username, id, c.Username, total)
	} else {
		log.Printf("[room] client %d (%s) joined, total=%d", id, c.Username, total)
	}

	return id, replaced, replacedID
}

// RemoveClient unregisters a client by ID.
func (r *Room) RemoveClient(id uint16) bool {
	r.mu.Lock()
	_, existed := r.clients[id]
	if existed {
		delete(r.clients, id)
	}
	total := len(r.clients)
	r.mu.Unlock()

	if existed {
		log.Printf("[room] client %d left, total=%d", id, total)
	}
	return existed
}

// broadcastTarget is a snapshot of a client's session for datagram fan-out.
// Capturing these under the read lock lets us release the lock before calling
// SendDatagram, preventing one slow client from blocking all others.
type broadcastTarget struct {
	id      uint16
	session DatagramSender
	health  *sendHealth
}

// targetPool provides per-goroutine []broadcastTarget slices for Broadcast.
// Using a pool instead of a shared field on Room avoids a data race: RLock
// allows multiple concurrent Broadcast calls, which would otherwise share
// and concurrently append to the same backing array.
var targetPool = sync.Pool{
	New: func() any {
		s := make([]broadcastTarget, 0, 8)
		return &s
	},
}

// Broadcast sends a datagram to every client in the same channel as the sender,
// excluding the sender itself. Clients in channel 0 (lobby) never send or
// receive voice datagrams.
// It overwrites bytes [0:2] with senderID before fan-out (anti-spoofing is done
// by the caller in readDatagrams, so the slice is already stamped here).
func (r *Room) Broadcast(senderID uint16, data []byte) {
	r.totalDatagrams.Add(1)
	r.totalBytes.Add(uint64(len(data)))

	// Snapshot targets under the read lock, then release before sending.
	r.mu.RLock()
	sender := r.clients[senderID]
	if sender == nil {
		r.mu.RUnlock()
		return
	}
	senderChannel := sender.channelID.Load()
	if senderChannel == 0 {
		r.mu.RUnlock()
		return // lobby users don't transmit voice
	}
	// Server-side mute: block audio from muted users.
	if sender.muted && (sender.muteExpiry == 0 || time.Now().UnixMilli() < sender.muteExpiry) {
		r.mu.RUnlock()
		return
	}

	sp := targetPool.Get().(*[]broadcastTarget)
	targets := (*sp)[:0]
	for id, c := range r.clients {
		if id == senderID {
			continue
		}
		if c.channelID.Load() != senderChannel {
			continue
		}
		if c.session == nil {
			continue
		}
		targets = append(targets, broadcastTarget{id: id, session: c.session, health: &c.health})
	}
	r.mu.RUnlock()

	// Feed active recording for this channel (before fan-out).
	r.FeedRecording(senderChannel, data)

	for _, t := range targets {
		if t.health.shouldSkip() {
			r.skippedDatagrams.Add(1)
			continue
		}
		if err := t.session.SendDatagram(data); err != nil {
			n := t.health.recordFailure()
			if n == circuitBreakerThreshold {
				log.Printf("[room] circuit breaker open for client %d — %d consecutive send failures", t.id, n)
			}
		} else if t.health.failures.Load() > 0 {
			if t.health.recordSuccess() {
				log.Printf("[room] circuit breaker closed for client %d — send recovered", t.id)
			}
		}
	}

	*sp = targets // preserve grown backing array for reuse
	targetPool.Put(sp)
}

// BroadcastControl sends a control message to all clients except the one with excludeID.
// Pass excludeID=0 to send to all clients.
// JSON is marshaled once before acquiring the lock to minimise lock hold time.
func (r *Room) BroadcastControl(msg ControlMsg, excludeID uint16) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[room] BroadcastControl marshal error: %v", err)
		return
	}
	data = append(data, '\n')

	r.mu.RLock()
	defer r.mu.RUnlock()

	for id, c := range r.clients {
		if id == excludeID {
			continue
		}
		c.sendRaw(data)
	}
}

// SendControlTo sends a control message to a specific client by ID.
func (r *Room) SendControlTo(targetID uint16, msg ControlMsg) {
	r.mu.RLock()
	c, ok := r.clients[targetID]
	r.mu.RUnlock()
	if ok {
		c.SendControl(msg)
	}
}

// BroadcastToChannel sends a control message to all clients currently in channelID.
// Sends to every matching client including the sender (excludeID=0 semantics).
// JSON is marshaled once before acquiring the lock to minimise lock hold time.
func (r *Room) BroadcastToChannel(channelID int64, msg ControlMsg) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[room] BroadcastToChannel marshal error: %v", err)
		return
	}
	data = append(data, '\n')

	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, c := range r.clients {
		if c.channelID.Load() == channelID {
			c.sendRaw(data)
		}
	}
}

// Clients returns a snapshot of all connected clients (safe to use after releasing the lock).
func (r *Room) Clients() []UserInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]UserInfo, 0, len(r.clients))
	for _, c := range r.clients {
		out = append(out, UserInfo{
			ID:        c.ID,
			Username:  c.Username,
			ChannelID: c.channelID.Load(),
			Role:      c.role,
			Muted:     c.muted,
		})
	}
	return out
}

// SetChannels replaces the cached channel list and broadcasts a channel_list
// message to all currently-connected clients.
func (r *Room) SetChannels(channels []ChannelInfo) {
	r.mu.Lock()
	r.channels = channels
	r.mu.Unlock()
	r.BroadcastControl(ControlMsg{Type: "channel_list", Channels: channels}, 0)
}

// GetChannelList returns the cached channel list.
func (r *Room) GetChannelList() []ChannelInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.channels
}

// SetOnCreateChannel registers a callback for creating a channel in the store.
func (r *Room) SetOnCreateChannel(fn func(name string) (int64, error)) {
	r.mu.Lock()
	r.onCreateChannel = fn
	r.mu.Unlock()
}

// SetOnRenameChannel registers a callback for renaming a channel in the store.
func (r *Room) SetOnRenameChannel(fn func(id int64, name string) error) {
	r.mu.Lock()
	r.onRenameChannel = fn
	r.mu.Unlock()
}

// SetOnDeleteChannel registers a callback for deleting a channel from the store.
func (r *Room) SetOnDeleteChannel(fn func(id int64) error) {
	r.mu.Lock()
	r.onDeleteChannel = fn
	r.mu.Unlock()
}

// SetOnRefreshChannels registers a callback that reloads the channel list from the store.
func (r *Room) SetOnRefreshChannels(fn func() ([]ChannelInfo, error)) {
	r.mu.Lock()
	r.onRefreshChannels = fn
	r.mu.Unlock()
}

// CreateChannel creates a channel in the store and broadcasts the updated list.
func (r *Room) CreateChannel(name string) (int64, error) {
	r.mu.RLock()
	cb := r.onCreateChannel
	r.mu.RUnlock()
	if cb == nil {
		return 0, fmt.Errorf("channel creation not configured")
	}
	id, err := cb(name)
	if err != nil {
		return 0, err
	}
	r.refreshChannels()
	return id, nil
}

// RenameChannel renames a channel in the store and broadcasts the updated list.
func (r *Room) RenameChannel(id int64, name string) error {
	r.mu.RLock()
	cb := r.onRenameChannel
	r.mu.RUnlock()
	if cb == nil {
		return fmt.Errorf("channel rename not configured")
	}
	if err := cb(id, name); err != nil {
		return err
	}
	r.refreshChannels()
	return nil
}

// DeleteChannel deletes a channel from the store and broadcasts the updated list.
func (r *Room) DeleteChannel(id int64) error {
	r.mu.RLock()
	cb := r.onDeleteChannel
	r.mu.RUnlock()
	if cb == nil {
		return fmt.Errorf("channel deletion not configured")
	}
	if err := cb(id); err != nil {
		return err
	}
	r.refreshChannels()
	return nil
}

// refreshChannels reloads the channel list from the store and broadcasts it.
func (r *Room) refreshChannels() {
	r.mu.RLock()
	cb := r.onRefreshChannels
	r.mu.RUnlock()
	if cb == nil {
		return
	}
	channels, err := cb()
	if err != nil {
		log.Printf("[room] refresh channels: %v", err)
		return
	}
	r.SetChannels(channels)
}

// MoveChannelUsersToLobby moves all users currently in channelID to channel 0
// (lobby) and broadcasts a user_channel update for each moved user.
func (r *Room) MoveChannelUsersToLobby(channelID int64) {
	r.mu.RLock()
	var moved []uint16
	for _, c := range r.clients {
		if c.channelID.Load() == channelID {
			c.channelID.Store(0)
			moved = append(moved, c.ID)
		}
	}
	r.mu.RUnlock()
	for _, id := range moved {
		r.BroadcastControl(ControlMsg{Type: "user_channel", ID: id, ChannelID: 0}, 0)
	}
}

// ChannelCount returns the number of cached channels.
func (r *Room) ChannelCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.channels)
}

// NextMsgID returns a monotonically increasing message ID for chat messages.
func (r *Room) NextMsgID() uint64 {
	return r.nextMsgID.Add(1)
}

// RecordMsgOwner associates a server-assigned message ID with the sender's
// client ID. This mapping is used to authorise edit/delete requests.
// The map is bounded to maxMsgOwners entries; oldest entries are evicted first.
func (r *Room) RecordMsgOwner(msgID uint64, senderID uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgOwners[msgID] = senderID
	r.msgOwnerKeys = append(r.msgOwnerKeys, msgID)
	for len(r.msgOwnerKeys) > maxMsgOwners {
		delete(r.msgOwners, r.msgOwnerKeys[0])
		r.msgOwnerKeys = r.msgOwnerKeys[1:]
	}
}

// GetMsgOwner returns the sender ID for a message, or 0 and false if unknown.
func (r *Room) GetMsgOwner(msgID uint64) (uint16, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.msgOwners[msgID]
	return id, ok
}

// RecordMsg stores a message in the in-memory message store for search, reply
// preview, and pin support. Also records the owner mapping.
func (r *Room) RecordMsg(msgID uint64, senderID uint16, username, message string, channelID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Record owner
	r.msgOwners[msgID] = senderID
	r.msgOwnerKeys = append(r.msgOwnerKeys, msgID)
	for len(r.msgOwnerKeys) > maxMsgOwners {
		evictID := r.msgOwnerKeys[0]
		delete(r.msgOwners, evictID)
		r.msgOwnerKeys = r.msgOwnerKeys[1:]
	}

	// Store message
	r.msgStore[msgID] = &storedMsg{
		MsgID:     msgID,
		SenderID:  senderID,
		Username:  username,
		Message:   message,
		ChannelID: channelID,
		Timestamp: time.Now().UnixMilli(),
	}
	r.msgStoreKeys = append(r.msgStoreKeys, msgID)
	for len(r.msgStoreKeys) > maxMsgOwners {
		evictID := r.msgStoreKeys[0]
		delete(r.msgStore, evictID)
		r.msgStoreKeys = r.msgStoreKeys[1:]
	}
}

// GetMsgPreview returns a reply preview for a message, or nil if not found.
func (r *Room) GetMsgPreview(msgID uint64) *ReplyPreview {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.msgStore[msgID]
	if !ok {
		return nil
	}
	preview := &ReplyPreview{
		MsgID:    m.MsgID,
		Username: m.Username,
		Message:  m.Message,
		Deleted:  m.Deleted,
	}
	// Truncate preview message
	if len(preview.Message) > 100 {
		preview.Message = preview.Message[:100] + "..."
	}
	return preview
}

// MarkMsgDeleted marks a message as deleted in the store (for reply previews).
func (r *Room) MarkMsgDeleted(msgID uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.msgStore[msgID]; ok {
		m.Deleted = true
		m.Message = ""
	}
}

// UpdateMsgContent updates the message content in the store (for edited messages).
func (r *Room) UpdateMsgContent(msgID uint64, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.msgStore[msgID]; ok {
		m.Message = message
	}
}

// AddReaction adds a reaction from a user on a message. Returns false if the
// user already reacted with this emoji (duplicate prevention).
func (r *Room) AddReaction(msgID uint64, userID uint16, emoji string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rx := range r.reactions[msgID] {
		if rx.UserID == userID && rx.Emoji == emoji {
			return false
		}
	}
	r.reactions[msgID] = append(r.reactions[msgID], reaction{UserID: userID, Emoji: emoji})
	return true
}

// RemoveReaction removes a reaction from a user on a message. Returns false if
// the reaction didn't exist.
func (r *Room) RemoveReaction(msgID uint64, userID uint16, emoji string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	rxs := r.reactions[msgID]
	for i, rx := range rxs {
		if rx.UserID == userID && rx.Emoji == emoji {
			r.reactions[msgID] = append(rxs[:i], rxs[i+1:]...)
			if len(r.reactions[msgID]) == 0 {
				delete(r.reactions, msgID)
			}
			return true
		}
	}
	return false
}

// GetReactions returns aggregated reaction info for a message.
func (r *Room) GetReactions(msgID uint64) []ReactionInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rxs := r.reactions[msgID]
	if len(rxs) == 0 {
		return nil
	}
	// Aggregate by emoji
	emojiMap := make(map[string][]uint16)
	emojiOrder := make([]string, 0)
	for _, rx := range rxs {
		if _, exists := emojiMap[rx.Emoji]; !exists {
			emojiOrder = append(emojiOrder, rx.Emoji)
		}
		emojiMap[rx.Emoji] = append(emojiMap[rx.Emoji], rx.UserID)
	}
	result := make([]ReactionInfo, 0, len(emojiOrder))
	for _, emoji := range emojiOrder {
		users := emojiMap[emoji]
		result = append(result, ReactionInfo{
			Emoji:   emoji,
			UserIDs: users,
			Count:   len(users),
		})
	}
	return result
}

// SearchMessages searches in-memory messages for a channel matching a query string.
func (r *Room) SearchMessages(channelID int64, query string, before uint64, limit int) []SearchResult {
	r.mu.RLock()
	defer r.mu.RUnlock()

	queryLower := strings.ToLower(query)
	var results []SearchResult

	// Iterate in reverse order (newest first) for more useful results
	for i := len(r.msgStoreKeys) - 1; i >= 0 && len(results) < limit; i-- {
		id := r.msgStoreKeys[i]
		if before > 0 && id >= before {
			continue
		}
		m, ok := r.msgStore[id]
		if !ok || m.Deleted || m.ChannelID != channelID {
			continue
		}
		if strings.Contains(strings.ToLower(m.Message), queryLower) {
			results = append(results, SearchResult{
				MsgID:     m.MsgID,
				Username:  m.Username,
				Message:   m.Message,
				Timestamp: m.Timestamp,
				ChannelID: m.ChannelID,
			})
		}
	}
	return results
}

// PinMessage pins a message in a channel. Returns false if already pinned or
// the max pin limit is reached.
func (r *Room) PinMessage(msgID uint64, channelID int64, pinnedBy uint16) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if already pinned
	for _, p := range r.pinnedMsgs {
		if p.MsgID == msgID {
			return false
		}
	}

	// Check per-channel limit
	count := 0
	for _, p := range r.pinnedMsgs {
		if p.ChannelID == channelID {
			count++
		}
	}
	if count >= maxPinnedPerChannel {
		return false
	}

	r.pinnedMsgs = append(r.pinnedMsgs, pinnedEntry{
		MsgID:     msgID,
		ChannelID: channelID,
		PinnedBy:  pinnedBy,
	})
	return true
}

// UnpinMessage removes a pin. Returns false if the message wasn't pinned.
func (r *Room) UnpinMessage(msgID uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, p := range r.pinnedMsgs {
		if p.MsgID == msgID {
			r.pinnedMsgs = append(r.pinnedMsgs[:i], r.pinnedMsgs[i+1:]...)
			return true
		}
	}
	return false
}

// GetPinnedMessages returns all pinned messages for a channel.
func (r *Room) GetPinnedMessages(channelID int64) []PinnedMsg {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []PinnedMsg
	for _, p := range r.pinnedMsgs {
		if p.ChannelID != channelID {
			continue
		}
		m, ok := r.msgStore[p.MsgID]
		if !ok {
			continue
		}
		result = append(result, PinnedMsg{
			MsgID:     m.MsgID,
			Username:  m.Username,
			Message:   m.Message,
			Timestamp: m.Timestamp,
			PinnedBy:  p.PinnedBy,
		})
	}
	return result
}

// ClientCount returns the current number of connected clients.
func (r *Room) ClientCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}

// Stats returns accumulated datagram/byte/skipped counts since the last call and resets them.
func (r *Room) Stats() (datagrams, bytes, skipped uint64, clients int) {
	datagrams = r.totalDatagrams.Swap(0)
	bytes = r.totalBytes.Swap(0)
	skipped = r.skippedDatagrams.Swap(0)
	clients = r.ClientCount()
	return
}

// ---------------------------------------------------------------------------
// Phase 8: Server Administration
// ---------------------------------------------------------------------------

// SetOnAuditLog registers a callback for audit log persistence.
func (r *Room) SetOnAuditLog(fn func(actorID int, actorName, action, target, details string)) {
	r.mu.Lock()
	r.onAuditLog = fn
	r.mu.Unlock()
}

// AuditLog records an action to the audit log callback.
func (r *Room) AuditLog(actorID int, actorName, action, target, details string) {
	r.mu.RLock()
	cb := r.onAuditLog
	r.mu.RUnlock()
	if cb != nil {
		cb(actorID, actorName, action, target, details)
	}
}

// SetOnBan registers a callback for ban persistence.
func (r *Room) SetOnBan(fn func(pubkey, ip, reason, bannedBy string, durationS int)) {
	r.mu.Lock()
	r.onBan = fn
	r.mu.Unlock()
}

// RecordBan records a ban through the persistence callback.
func (r *Room) RecordBan(pubkey, ip, reason, bannedBy string, durationS int) {
	r.mu.RLock()
	cb := r.onBan
	r.mu.RUnlock()
	if cb != nil {
		cb(pubkey, ip, reason, bannedBy, durationS)
	}
	r.AuditLog(0, bannedBy, "ban", pubkey, fmt.Sprintf(`{"reason":%q,"ip":%q,"duration":%d}`, reason, ip, durationS))
}

// SetOnUnban registers a callback for unban persistence.
func (r *Room) SetOnUnban(fn func(banID int64)) {
	r.mu.Lock()
	r.onUnban = fn
	r.mu.Unlock()
}

// RemoveBan removes a ban through the persistence callback.
func (r *Room) RemoveBan(banID int64) {
	r.mu.RLock()
	cb := r.onUnban
	r.mu.RUnlock()
	if cb != nil {
		cb(banID)
	}
}

// SetClientRole sets the role of a connected client.
func (r *Room) SetClientRole(id uint16, role string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.clients[id]; ok {
		c.role = role
	}
}

// GetClientRole returns the role of a connected client.
func (r *Room) GetClientRole(id uint16) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if c, ok := r.clients[id]; ok {
		return c.role
	}
	return RoleUser
}

// SetClientMute sets the mute state of a connected client.
func (r *Room) SetClientMute(id uint16, muted bool, expiry int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.clients[id]; ok {
		c.muted = muted
		c.muteExpiry = expiry
	}
}

// IsClientMuted returns true if the client is currently muted (checking expiry).
func (r *Room) IsClientMuted(id uint16) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clients[id]
	if !ok {
		return false
	}
	if !c.muted {
		return false
	}
	if c.muteExpiry > 0 && time.Now().UnixMilli() >= c.muteExpiry {
		return false
	}
	return true
}

// CheckMuteExpiry checks all clients and auto-unmutes expired timed mutes.
func (r *Room) CheckMuteExpiry() {
	now := time.Now().UnixMilli()
	r.mu.Lock()
	var expired []uint16
	for id, c := range r.clients {
		if c.muted && c.muteExpiry > 0 && now >= c.muteExpiry {
			c.muted = false
			c.muteExpiry = 0
			expired = append(expired, id)
		}
	}
	r.mu.Unlock()
	for _, id := range expired {
		r.BroadcastControl(ControlMsg{
			Type:  "user_muted",
			ID:    id,
			Muted: false,
		}, 0)
		log.Printf("[room] auto-unmuted client %d (mute expired)", id)
	}
}

// SetAnnouncement sets the current server-wide announcement.
func (r *Room) SetAnnouncement(content, createdBy string) {
	r.mu.Lock()
	r.announcement = content
	r.announceUser = createdBy
	r.mu.Unlock()
}

// GetAnnouncement returns the current announcement content and creator.
func (r *Room) GetAnnouncement() (string, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.announcement, r.announceUser
}

// SetSlowMode sets the slow mode cooldown for a channel.
func (r *Room) SetSlowMode(channelID int64, seconds int) {
	r.mu.Lock()
	if seconds <= 0 {
		delete(r.slowModes, channelID)
	} else {
		r.slowModes[channelID] = seconds
	}
	r.mu.Unlock()
}

// GetSlowMode returns the slow mode cooldown for a channel.
func (r *Room) GetSlowMode(channelID int64) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.slowModes[channelID]
}

// CheckSlowMode returns true if the client is allowed to send a chat message
// in the given channel (respecting slow mode). Admins and above are exempt.
func (r *Room) CheckSlowMode(clientID uint16, channelID int64) bool {
	r.mu.RLock()
	cooldown := r.slowModes[channelID]
	c := r.clients[clientID]
	r.mu.RUnlock()

	if cooldown <= 0 || c == nil {
		return true
	}
	if roleLevel(c.role) >= roleLevel(RoleAdmin) {
		return true
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if c.lastChatTime == nil {
		c.lastChatTime = make(map[int64]time.Time)
	}
	last := c.lastChatTime[channelID]
	if now.Sub(last) < time.Duration(cooldown)*time.Second {
		return false
	}
	c.lastChatTime[channelID] = now
	return true
}

// ---------------------------------------------------------------------------
// Phase 10: Performance & Reliability
// ---------------------------------------------------------------------------

// SetMaxConnections sets the maximum number of concurrent connections.
func (r *Room) SetMaxConnections(max int) {
	r.mu.Lock()
	r.maxConnections = max
	r.mu.Unlock()
}

// SetPerIPLimit sets the maximum connections per IP address.
func (r *Room) SetPerIPLimit(max int) {
	r.mu.Lock()
	r.perIPLimit = max
	r.mu.Unlock()
}

// SetControlRateLimit sets the max control messages per second per client.
func (r *Room) SetControlRateLimit(max int) {
	r.mu.Lock()
	r.controlRateLimit = max
	r.mu.Unlock()
}

// CanConnect checks if a new connection from the given IP is allowed.
func (r *Room) CanConnect(ip string) bool {
	r.mu.RLock()
	maxConn := r.maxConnections
	perIP := r.perIPLimit
	currentTotal := len(r.clients)
	currentIP := r.ipConnections[ip]
	r.mu.RUnlock()

	if maxConn > 0 && currentTotal >= maxConn {
		return false
	}
	if perIP > 0 && currentIP >= perIP {
		return false
	}
	return true
}

// TrackIPConnect increments the IP connection count.
func (r *Room) TrackIPConnect(ip string) {
	if ip == "" {
		return
	}
	r.mu.Lock()
	r.ipConnections[ip]++
	r.mu.Unlock()
}

// TrackIPDisconnect decrements the IP connection count.
func (r *Room) TrackIPDisconnect(ip string) {
	if ip == "" {
		return
	}
	r.mu.Lock()
	r.ipConnections[ip]--
	if r.ipConnections[ip] <= 0 {
		delete(r.ipConnections, ip)
	}
	r.mu.Unlock()
}

// CheckControlRate returns true if the client is within the rate limit.
func (r *Room) CheckControlRate(clientID uint16) bool {
	r.mu.RLock()
	limit := r.controlRateLimit
	r.mu.RUnlock()
	if limit <= 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.clients[clientID]
	if !ok {
		return false
	}
	now := time.Now()
	if now.Sub(c.lastControlMsg) >= time.Second {
		c.lastControlMsg = now
		c.controlMsgCount = 1
		return true
	}
	c.controlMsgCount++
	return c.controlMsgCount <= limit
}

// BufferMessage adds a message to the per-channel replay buffer.
func (r *Room) BufferMessage(channelID int64, msg ControlMsg) {
	if channelID == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channelSeqs[channelID]++
	msg.SeqNum = r.channelSeqs[channelID]
	buf := r.msgBuffer[channelID]
	buf = append(buf, msg)
	if len(buf) > maxMsgBuffer {
		buf = buf[len(buf)-maxMsgBuffer:]
	}
	r.msgBuffer[channelID] = buf
}

// GetMessagesSince returns buffered messages for a channel with seq > lastSeq.
func (r *Room) GetMessagesSince(channelID int64, lastSeq uint64) []ControlMsg {
	r.mu.RLock()
	defer r.mu.RUnlock()
	buf := r.msgBuffer[channelID]
	var result []ControlMsg
	for _, m := range buf {
		if m.SeqNum > lastSeq {
			result = append(result, m)
		}
	}
	return result
}

// GetChannelSeq returns the current sequence number for a channel.
func (r *Room) GetChannelSeq(channelID int64) uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.channelSeqs[channelID]
}

// IsBroadcastMuted checks if a client's audio should be blocked (server-side mute).
func (r *Room) IsBroadcastMuted(senderID uint16) bool {
	return r.IsClientMuted(senderID)
}

// ---------------------------------------------------------------------------
// Phase 7: Voice Channel User Limit
// ---------------------------------------------------------------------------

// SetChannelMaxUsers sets the maximum user limit for a channel.
// maxUsers=0 means unlimited.
func (r *Room) SetChannelMaxUsers(channelID int64, maxUsers int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, ch := range r.channels {
		if ch.ID == channelID {
			r.channels[i].MaxUsers = maxUsers
			break
		}
	}
}

// GetChannelMaxUsers returns the max user limit for a channel (0=unlimited).
func (r *Room) GetChannelMaxUsers(channelID int64) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, ch := range r.channels {
		if ch.ID == channelID {
			return ch.MaxUsers
		}
	}
	return 0
}

// ChannelUserCount returns the number of users currently in a channel.
func (r *Room) ChannelUserCount(channelID int64) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, c := range r.clients {
		if c.channelID.Load() == channelID {
			count++
		}
	}
	return count
}

// CanJoinChannel checks whether a user can join a channel (respecting max_users).
// Returns true if there is room or no limit is set.
func (r *Room) CanJoinChannel(channelID int64) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var maxUsers int
	for _, ch := range r.channels {
		if ch.ID == channelID {
			maxUsers = ch.MaxUsers
			break
		}
	}
	if maxUsers <= 0 {
		return true // unlimited
	}
	count := 0
	for _, c := range r.clients {
		if c.channelID.Load() == channelID {
			count++
		}
	}
	return count < maxUsers
}

// ---------------------------------------------------------------------------
// Phase 7: Server-Side Recording
// ---------------------------------------------------------------------------

// StartRecordingChannel begins recording voice for a channel.
// Returns an error if a recording is already active for the channel.
func (r *Room) StartRecordingChannel(channelID int64, startedBy string) error {
	r.mu.Lock()
	if _, active := r.recordings[channelID]; active {
		r.mu.Unlock()
		return fmt.Errorf("recording already active for channel %d", channelID)
	}
	dataDir := r.dataDir
	if dataDir == "" {
		dataDir = "."
	}
	r.mu.Unlock()

	rec, err := StartRecording(channelID, startedBy, dataDir, func() {
		// Auto-stop callback: remove from active recordings and broadcast.
		r.mu.Lock()
		if active, ok := r.recordings[channelID]; ok {
			r.doneRecs = append(r.doneRecs, active.Info())
			delete(r.recordings, channelID)
		}
		r.mu.Unlock()
		r.BroadcastToChannel(channelID, ControlMsg{
			Type:      "recording_stopped",
			ChannelID: channelID,
		})
	})
	if err != nil {
		return err
	}

	r.mu.Lock()
	r.recordings[channelID] = rec
	r.mu.Unlock()
	return nil
}

// StopRecordingChannel stops an active recording for a channel.
func (r *Room) StopRecordingChannel(channelID int64) error {
	r.mu.Lock()
	rec, ok := r.recordings[channelID]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("no active recording for channel %d", channelID)
	}
	delete(r.recordings, channelID)
	r.mu.Unlock()

	rec.Stop()

	r.mu.Lock()
	r.doneRecs = append(r.doneRecs, rec.Info())
	r.mu.Unlock()

	return nil
}

// IsRecording returns true if the given channel has an active recording.
func (r *Room) IsRecording(channelID int64) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.recordings[channelID]
	return ok
}

// FeedRecording passes a voice datagram to the active recorder for the channel.
// Called from Broadcast; safe to call when no recording is active (no-op).
func (r *Room) FeedRecording(channelID int64, data []byte) {
	r.mu.RLock()
	rec := r.recordings[channelID]
	r.mu.RUnlock()
	if rec != nil {
		rec.FeedDatagram(data)
	}
}

// ListRecordings returns metadata for all completed recordings.
func (r *Room) ListRecordings() []RecordingInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RecordingInfo, len(r.doneRecs))
	copy(out, r.doneRecs)
	return out
}

// GetRecordingFilePath returns the file path for a completed recording by filename.
func (r *Room) GetRecordingFilePath(filename string) string {
	r.mu.RLock()
	dataDir := r.dataDir
	if dataDir == "" {
		dataDir = "."
	}
	r.mu.RUnlock()
	return filepath.Join(dataDir, recordingsDir, filename)
}

// StopAllRecordings stops all active recordings. Called during shutdown.
func (r *Room) StopAllRecordings() {
	r.mu.Lock()
	recs := make(map[int64]*ChannelRecorder)
	for k, v := range r.recordings {
		recs[k] = v
	}
	r.recordings = make(map[int64]*ChannelRecorder)
	r.mu.Unlock()
	for _, rec := range recs {
		rec.Stop()
	}
}
