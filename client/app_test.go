package main

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Mock Transporter
// ---------------------------------------------------------------------------

type mockTransport struct {
	mu sync.Mutex

	// Connect / Disconnect
	connectCalled bool
	connectAddr   string
	connectUser   string
	connectErr    error
	disconnected  int // count

	// Muting
	mutedUsers map[uint16]bool

	// Per-user volume
	userVolumes map[uint16]float64

	// Control messages sent
	chatsSent    []string
	channelChats []struct {
		channelID int64
		msg       string
	}
	editedMessages []struct {
		msgID uint64
		msg   string
	}
	deletedMessages []uint64
	reactions       []struct {
		msgID uint64
		emoji string
	}
	removedReactions []struct {
		msgID uint64
		emoji string
	}
	typingSent      []int64
	kickedUsers     []uint16
	renamedUsers    []string
	renamedServers  []string
	channelsJoined  []int64
	channelsCreated []string
	channelsRenamed []struct {
		id   int64
		name string
	}
	channelsDeleted []int64
	usersMovedTo    []struct {
		userID    uint16
		channelID int64
	}
	videoStates      []struct{ active, screenShare bool }
	recordingsStart  []int64
	recordingsStop   []int64
	videoQualityReqs []struct {
		targetID uint16
		quality  string
	}
	fileChatsSent []struct {
		channelID, fileID, fileSize int64
		fileName, message           string
	}

	// Configurable error returns
	sendChatErr         error
	sendChannelChatErr  error
	editMessageErr      error
	deleteMessageErr    error
	addReactionErr      error
	removeReactionErr   error
	sendTypingErr       error
	kickUserErr         error
	renameUserErr       error
	renameServerErr     error
	joinChannelErr      error
	createChannelErr    error
	renameChannelErr    error
	deleteChannelErr    error
	moveUserErr         error
	sendVideoStateErr   error
	startRecordingErr   error
	stopRecordingErr    error
	requestVideoQualErr error
	sendFileChatErr     error

	// Callback storage
	onUserList           func([]UserInfo)
	onUserJoined         func(uint16, string)
	onUserLeft           func(uint16)
	onAudioReceived      func(uint16)
	onDisconnected       func(reason string)
	onChatMessage        func(uint64, uint16, string, string, int64, int64, string, int64, []uint16, uint64, *ReplyPreview)
	onChannelChatMessage func(uint64, uint16, int64, string, string, int64, int64, string, int64, []uint16, uint64, *ReplyPreview)
	onLinkPreview        func(uint64, int64, string, string, string, string, string)
	onServerInfo         func(string)
	onKicked             func()
	onOwnerChanged       func(uint16)
	onChannelList        func([]ChannelInfo)
	onUserChannel        func(uint16, int64)
	onUserRenamed        func(uint16, string)
	onMessageEdited      func(uint64, string, int64)
	onMessageDeleted     func(uint64)
	onVideoState         func(uint16, bool, bool)
	onReactionAdded      func(uint64, string, uint16)
	onReactionRemoved    func(uint64, string, uint16)
	onUserTyping         func(uint16, string, int64)
	onMessagePinned      func(uint64, int64, uint16)
	onMessageUnpinned    func(uint64)
	onRecordingState     func(int64, bool, string)
	onVideoLayers        func(uint16, []VideoLayer)
	onVideoQualityReq    func(uint16, string)

	// Return values
	myIDValue     uint16
	metricsValue  Metrics
	apiBaseURLVal string
}

func newMockTransport() *mockTransport {
	return &mockTransport{
		mutedUsers:  make(map[uint16]bool),
		userVolumes: make(map[uint16]float64),
	}
}

func (m *mockTransport) Connect(_ context.Context, addr, username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectCalled = true
	m.connectAddr = addr
	m.connectUser = username
	return m.connectErr
}

func (m *mockTransport) Disconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disconnected++
}

func (m *mockTransport) SendAudio(_ []byte) error                               { return nil }
func (m *mockTransport) StartReceiving(_ context.Context, _ chan<- TaggedAudio) {}
func (m *mockTransport) MyID() uint16 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.myIDValue
}
func (m *mockTransport) GetMetrics() Metrics {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.metricsValue
}

func (m *mockTransport) MuteUser(id uint16)   { m.mu.Lock(); m.mutedUsers[id] = true; m.mu.Unlock() }
func (m *mockTransport) UnmuteUser(id uint16) { m.mu.Lock(); delete(m.mutedUsers, id); m.mu.Unlock() }
func (m *mockTransport) IsUserMuted(id uint16) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mutedUsers[id]
}
func (m *mockTransport) MutedUsers() []uint16 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]uint16, 0, len(m.mutedUsers))
	for id := range m.mutedUsers {
		out = append(out, id)
	}
	return out
}

func (m *mockTransport) SetUserVolume(id uint16, vol float64) {
	m.mu.Lock()
	m.userVolumes[id] = vol
	m.mu.Unlock()
}
func (m *mockTransport) GetUserVolume(id uint16) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.userVolumes[id]
	if !ok {
		return 1.0
	}
	return v
}

// Callback setters — store the callback for later inspection.
func (m *mockTransport) SetOnUserList(fn func([]UserInfo))        { m.onUserList = fn }
func (m *mockTransport) SetOnUserJoined(fn func(uint16, string))  { m.onUserJoined = fn }
func (m *mockTransport) SetOnUserLeft(fn func(uint16))            { m.onUserLeft = fn }
func (m *mockTransport) SetOnAudioReceived(fn func(uint16))       { m.onAudioReceived = fn }
func (m *mockTransport) SetOnDisconnected(fn func(reason string)) { m.onDisconnected = fn }
func (m *mockTransport) SetOnChatMessage(fn func(uint64, uint16, string, string, int64, int64, string, int64, []uint16, uint64, *ReplyPreview)) {
	m.onChatMessage = fn
}
func (m *mockTransport) SetOnChannelChatMessage(fn func(uint64, uint16, int64, string, string, int64, int64, string, int64, []uint16, uint64, *ReplyPreview)) {
	m.onChannelChatMessage = fn
}
func (m *mockTransport) SetOnLinkPreview(fn func(uint64, int64, string, string, string, string, string)) {
	m.onLinkPreview = fn
}
func (m *mockTransport) SetOnServerInfo(fn func(string))          { m.onServerInfo = fn }
func (m *mockTransport) SetOnKicked(fn func())                    { m.onKicked = fn }
func (m *mockTransport) SetOnOwnerChanged(fn func(uint16))        { m.onOwnerChanged = fn }
func (m *mockTransport) SetOnChannelList(fn func([]ChannelInfo))  { m.onChannelList = fn }
func (m *mockTransport) SetOnUserChannel(fn func(uint16, int64))  { m.onUserChannel = fn }
func (m *mockTransport) SetOnUserRenamed(fn func(uint16, string)) { m.onUserRenamed = fn }
func (m *mockTransport) SetOnMessageEdited(fn func(uint64, string, int64)) {
	m.onMessageEdited = fn
}
func (m *mockTransport) SetOnMessageDeleted(fn func(uint64))                { m.onMessageDeleted = fn }
func (m *mockTransport) SetOnVideoState(fn func(uint16, bool, bool))        { m.onVideoState = fn }
func (m *mockTransport) SetOnReactionAdded(fn func(uint64, string, uint16)) { m.onReactionAdded = fn }
func (m *mockTransport) SetOnReactionRemoved(fn func(uint64, string, uint16)) {
	m.onReactionRemoved = fn
}
func (m *mockTransport) SetOnUserTyping(fn func(uint16, string, int64))    { m.onUserTyping = fn }
func (m *mockTransport) SetOnMessagePinned(fn func(uint64, int64, uint16)) { m.onMessagePinned = fn }
func (m *mockTransport) SetOnMessageUnpinned(fn func(uint64))              { m.onMessageUnpinned = fn }
func (m *mockTransport) SetOnRecordingState(fn func(int64, bool, string))  { m.onRecordingState = fn }
func (m *mockTransport) SetOnVideoLayers(fn func(uint16, []VideoLayer))    { m.onVideoLayers = fn }
func (m *mockTransport) SetOnVideoQualityRequest(fn func(uint16, string))  { m.onVideoQualityReq = fn }
func (m *mockTransport) SetOnMessageHistory(fn func(int64, []ChatHistoryMessage)) {}
func (m *mockTransport) SetOnUserVoiceFlags(fn func(uint16, bool, bool))          {}
func (m *mockTransport) SendVoiceFlags(muted, deafened bool) error                { return nil }

// Chat operations
func (m *mockTransport) SendChat(message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendChatErr != nil {
		return m.sendChatErr
	}
	m.chatsSent = append(m.chatsSent, message)
	return nil
}
func (m *mockTransport) SendChannelChat(channelID int64, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendChannelChatErr != nil {
		return m.sendChannelChatErr
	}
	m.channelChats = append(m.channelChats, struct {
		channelID int64
		msg       string
	}{channelID, message})
	return nil
}
func (m *mockTransport) SendFileChat(channelID, fileID, fileSize int64, fileName, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendFileChatErr != nil {
		return m.sendFileChatErr
	}
	m.fileChatsSent = append(m.fileChatsSent, struct {
		channelID, fileID, fileSize int64
		fileName, message           string
	}{channelID, fileID, fileSize, fileName, message})
	return nil
}
func (m *mockTransport) EditMessage(msgID uint64, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.editMessageErr != nil {
		return m.editMessageErr
	}
	m.editedMessages = append(m.editedMessages, struct {
		msgID uint64
		msg   string
	}{msgID, message})
	return nil
}
func (m *mockTransport) DeleteMessage(msgID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteMessageErr != nil {
		return m.deleteMessageErr
	}
	m.deletedMessages = append(m.deletedMessages, msgID)
	return nil
}
func (m *mockTransport) AddReaction(msgID uint64, emoji string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.addReactionErr != nil {
		return m.addReactionErr
	}
	m.reactions = append(m.reactions, struct {
		msgID uint64
		emoji string
	}{msgID, emoji})
	return nil
}
func (m *mockTransport) RemoveReaction(msgID uint64, emoji string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.removeReactionErr != nil {
		return m.removeReactionErr
	}
	m.removedReactions = append(m.removedReactions, struct {
		msgID uint64
		emoji string
	}{msgID, emoji})
	return nil
}
func (m *mockTransport) SendTyping(channelID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendTypingErr != nil {
		return m.sendTypingErr
	}
	m.typingSent = append(m.typingSent, channelID)
	return nil
}
func (m *mockTransport) KickUser(id uint16) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.kickUserErr != nil {
		return m.kickUserErr
	}
	m.kickedUsers = append(m.kickedUsers, id)
	return nil
}
func (m *mockTransport) RenameUser(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.renameUserErr != nil {
		return m.renameUserErr
	}
	m.renamedUsers = append(m.renamedUsers, name)
	return nil
}
func (m *mockTransport) RenameServer(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.renameServerErr != nil {
		return m.renameServerErr
	}
	m.renamedServers = append(m.renamedServers, name)
	return nil
}
func (m *mockTransport) JoinChannel(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.joinChannelErr != nil {
		return m.joinChannelErr
	}
	m.channelsJoined = append(m.channelsJoined, id)
	return nil
}
func (m *mockTransport) CreateChannel(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createChannelErr != nil {
		return m.createChannelErr
	}
	m.channelsCreated = append(m.channelsCreated, name)
	return nil
}
func (m *mockTransport) RenameChannel(id int64, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.renameChannelErr != nil {
		return m.renameChannelErr
	}
	m.channelsRenamed = append(m.channelsRenamed, struct {
		id   int64
		name string
	}{id, name})
	return nil
}
func (m *mockTransport) DeleteChannel(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteChannelErr != nil {
		return m.deleteChannelErr
	}
	m.channelsDeleted = append(m.channelsDeleted, id)
	return nil
}
func (m *mockTransport) MoveUser(userID uint16, channelID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.moveUserErr != nil {
		return m.moveUserErr
	}
	m.usersMovedTo = append(m.usersMovedTo, struct {
		userID    uint16
		channelID int64
	}{userID, channelID})
	return nil
}
func (m *mockTransport) SendVideoState(active, screenShare bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendVideoStateErr != nil {
		return m.sendVideoStateErr
	}
	m.videoStates = append(m.videoStates, struct{ active, screenShare bool }{active, screenShare})
	return nil
}
func (m *mockTransport) StartRecording(channelID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startRecordingErr != nil {
		return m.startRecordingErr
	}
	m.recordingsStart = append(m.recordingsStart, channelID)
	return nil
}
func (m *mockTransport) StopRecording(channelID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopRecordingErr != nil {
		return m.stopRecordingErr
	}
	m.recordingsStop = append(m.recordingsStop, channelID)
	return nil
}
func (m *mockTransport) RequestVideoQuality(targetID uint16, quality string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.requestVideoQualErr != nil {
		return m.requestVideoQualErr
	}
	m.videoQualityReqs = append(m.videoQualityReqs, struct {
		targetID uint16
		quality  string
	}{targetID, quality})
	return nil
}
func (m *mockTransport) APIBaseURL() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.apiBaseURLVal
}
func (m *mockTransport) RequestChannels() error    { return nil }
func (m *mockTransport) RequestMessages(_ int64) error { return nil }
func (m *mockTransport) RequestServerInfo() error  { return nil }

// Verify interface compliance at compile time.
var _ Transporter = (*mockTransport)(nil)

// ---------------------------------------------------------------------------
// Helper: create an App with mock transport (audio stays real but we don't
// call Start/Stop so PortAudio is not needed).
// ---------------------------------------------------------------------------

func newTestApp() (*App, *mockTransport) {
	mt := newMockTransport()
	app := &App{
		audio:     NewAudioEngine(),
		transport: mt,
	}
	return app, mt
}

// ===========================================================================
// IsConnected
// ===========================================================================

func TestIsConnectedDefault(t *testing.T) {
	app, _ := newTestApp()
	if app.IsConnected() {
		t.Error("expected not connected by default")
	}
}

func TestIsConnectedAfterSet(t *testing.T) {
	app, _ := newTestApp()
	app.connected.Store(true)
	if !app.IsConnected() {
		t.Error("expected connected after setting true")
	}
}

// ===========================================================================
// Connect
// ===========================================================================

func TestConnectAlreadyConnected(t *testing.T) {
	app, mt := newTestApp()
	app.serverAddr = "localhost:8080"
	result := app.Connect("localhost:8080", "alice")
	if result != "" {
		t.Errorf("expected empty result when reusing session, got %q", result)
	}
	mt.mu.Lock()
	called := mt.connectCalled
	mt.mu.Unlock()
	if called {
		t.Error("expected Connect to reuse existing session without dialing again")
	}
}

func TestConnectTransportError(t *testing.T) {
	app, mt := newTestApp()
	mt.connectErr = errors.New("dial failed")
	result := app.Connect("localhost:8080", "alice")
	if result != "dial failed" {
		t.Errorf("expected 'dial failed', got %q", result)
	}
	if app.IsConnected() {
		t.Error("should not be connected after transport error")
	}
}

func TestConnectDoesNotDisconnectBeforeDial(t *testing.T) {
	app, mt := newTestApp()
	// Force a transport error and ensure Connect does not pre-emptively disconnect.
	mt.connectErr = errors.New("fail")
	app.Connect("localhost:8080", "bob")
	mt.mu.Lock()
	dc := mt.disconnected
	mt.mu.Unlock()
	if dc != 0 {
		t.Errorf("expected no Disconnect before failed Connect, got %d", dc)
	}
}

// ===========================================================================
// Disconnect
// ===========================================================================

func TestDisconnectClearsMetrics(t *testing.T) {
	app, _ := newTestApp()
	app.connected.Store(true)
	app.metricsMu.Lock()
	app.cachedMetrics = Metrics{RTTMs: 10.0, PacketLoss: 0.05}
	app.metricsMu.Unlock()

	app.Disconnect()

	if app.IsConnected() {
		t.Error("expected disconnected")
	}
	m := app.GetMetrics()
	if m.RTTMs != 0 || m.PacketLoss != 0 {
		t.Errorf("expected zeroed metrics, got rtt=%f loss=%f", m.RTTMs, m.PacketLoss)
	}
}

func TestDisconnectCallsTransportDisconnect(t *testing.T) {
	app, mt := newTestApp()
	app.connected.Store(true)
	app.Disconnect()
	mt.mu.Lock()
	dc := mt.disconnected
	mt.mu.Unlock()
	if dc < 1 {
		t.Error("expected transport.Disconnect to be called")
	}
}

func TestDisconnectIdempotent(t *testing.T) {
	app, mt := newTestApp()
	app.Disconnect()
	app.Disconnect()
	mt.mu.Lock()
	dc := mt.disconnected
	mt.mu.Unlock()
	// First call disconnects transport, second is a no-op (transport already nil).
	if dc < 1 {
		t.Errorf("expected at least 1 Disconnect call, got %d", dc)
	}
}

// ===========================================================================
// SendChat
// ===========================================================================

func TestSendChatSuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.SendChat("hello")
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.chatsSent) != 1 || mt.chatsSent[0] != "hello" {
		t.Errorf("expected 1 chat 'hello', got %v", mt.chatsSent)
	}
}

func TestSendChatError(t *testing.T) {
	app, mt := newTestApp()
	mt.sendChatErr = errors.New("send failed")
	result := app.SendChat("hello")
	if result != "send failed" {
		t.Errorf("expected 'send failed', got %q", result)
	}
}

// ===========================================================================
// SendChannelChat
// ===========================================================================

func TestSendChannelChatSuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.SendChannelChat(42, "hi channel")
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.channelChats) != 1 {
		t.Fatalf("expected 1 channel chat, got %d", len(mt.channelChats))
	}
	if mt.channelChats[0].channelID != 42 || mt.channelChats[0].msg != "hi channel" {
		t.Errorf("unexpected channel chat: %+v", mt.channelChats[0])
	}
}

func TestSendChannelChatError(t *testing.T) {
	app, mt := newTestApp()
	mt.sendChannelChatErr = errors.New("no channel")
	result := app.SendChannelChat(1, "msg")
	if result != "no channel" {
		t.Errorf("expected 'no channel', got %q", result)
	}
}

// ===========================================================================
// EditMessage
// ===========================================================================

func TestEditMessageSuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.EditMessage(100, "updated text")
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.editedMessages) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(mt.editedMessages))
	}
	if mt.editedMessages[0].msgID != 100 || mt.editedMessages[0].msg != "updated text" {
		t.Errorf("unexpected edit: %+v", mt.editedMessages[0])
	}
}

func TestEditMessageError(t *testing.T) {
	app, mt := newTestApp()
	mt.editMessageErr = errors.New("not allowed")
	result := app.EditMessage(1, "text")
	if result != "not allowed" {
		t.Errorf("expected 'not allowed', got %q", result)
	}
}

// ===========================================================================
// DeleteMessage
// ===========================================================================

func TestDeleteMessageSuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.DeleteMessage(200)
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.deletedMessages) != 1 || mt.deletedMessages[0] != 200 {
		t.Errorf("expected delete of msg 200, got %v", mt.deletedMessages)
	}
}

func TestDeleteMessageError(t *testing.T) {
	app, mt := newTestApp()
	mt.deleteMessageErr = errors.New("forbidden")
	result := app.DeleteMessage(1)
	if result != "forbidden" {
		t.Errorf("expected 'forbidden', got %q", result)
	}
}

// ===========================================================================
// AddReaction / RemoveReaction
// ===========================================================================

func TestAddReactionSuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.AddReaction(10, "thumbsup")
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.reactions) != 1 || mt.reactions[0].msgID != 10 || mt.reactions[0].emoji != "thumbsup" {
		t.Errorf("unexpected reaction: %v", mt.reactions)
	}
}

func TestAddReactionError(t *testing.T) {
	app, mt := newTestApp()
	mt.addReactionErr = errors.New("rate limited")
	result := app.AddReaction(10, "fire")
	if result != "rate limited" {
		t.Errorf("expected 'rate limited', got %q", result)
	}
}

func TestRemoveReactionSuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.RemoveReaction(10, "thumbsup")
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.removedReactions) != 1 || mt.removedReactions[0].emoji != "thumbsup" {
		t.Errorf("unexpected removed reaction: %v", mt.removedReactions)
	}
}

func TestRemoveReactionError(t *testing.T) {
	app, mt := newTestApp()
	mt.removeReactionErr = errors.New("not found")
	result := app.RemoveReaction(10, "fire")
	if result != "not found" {
		t.Errorf("expected 'not found', got %q", result)
	}
}

// ===========================================================================
// SendTyping
// ===========================================================================

func TestSendTypingSuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.SendTyping(5)
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.typingSent) != 1 || mt.typingSent[0] != 5 {
		t.Errorf("expected typing in channel 5, got %v", mt.typingSent)
	}
}

func TestSendTypingError(t *testing.T) {
	app, mt := newTestApp()
	mt.sendTypingErr = errors.New("typing error")
	result := app.SendTyping(1)
	if result != "typing error" {
		t.Errorf("expected 'typing error', got %q", result)
	}
}

// ===========================================================================
// KickUser
// ===========================================================================

func TestKickUserSuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.KickUser(7)
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.kickedUsers) != 1 || mt.kickedUsers[0] != 7 {
		t.Errorf("expected kick of user 7, got %v", mt.kickedUsers)
	}
}

func TestKickUserError(t *testing.T) {
	app, mt := newTestApp()
	mt.kickUserErr = errors.New("not owner")
	result := app.KickUser(7)
	if result != "not owner" {
		t.Errorf("expected 'not owner', got %q", result)
	}
}

// ===========================================================================
// RenameUser
// ===========================================================================

func TestRenameUserSuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.RenameUser("newname")
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.renamedUsers) != 1 || mt.renamedUsers[0] != "newname" {
		t.Errorf("expected rename to 'newname', got %v", mt.renamedUsers)
	}
}

func TestRenameUserError(t *testing.T) {
	app, mt := newTestApp()
	mt.renameUserErr = errors.New("name taken")
	result := app.RenameUser("taken")
	if result != "name taken" {
		t.Errorf("expected 'name taken', got %q", result)
	}
}

// ===========================================================================
// RenameServer
// ===========================================================================

func TestRenameServerSuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.RenameServer("My Server")
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.renamedServers) != 1 || mt.renamedServers[0] != "My Server" {
		t.Errorf("expected rename to 'My Server', got %v", mt.renamedServers)
	}
}

func TestRenameServerError(t *testing.T) {
	app, mt := newTestApp()
	mt.renameServerErr = errors.New("not owner")
	result := app.RenameServer("x")
	if result != "not owner" {
		t.Errorf("expected 'not owner', got %q", result)
	}
}

// ===========================================================================
// JoinChannel
// ===========================================================================

func TestJoinChannelSuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.JoinChannel(3)
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.channelsJoined) != 1 || mt.channelsJoined[0] != 3 {
		t.Errorf("expected join channel 3, got %v", mt.channelsJoined)
	}
}

func TestJoinChannelLobby(t *testing.T) {
	app, mt := newTestApp()
	result := app.JoinChannel(0)
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.channelsJoined) != 1 || mt.channelsJoined[0] != 0 {
		t.Errorf("expected join channel 0 (lobby), got %v", mt.channelsJoined)
	}
}

func TestJoinChannelError(t *testing.T) {
	app, mt := newTestApp()
	mt.joinChannelErr = errors.New("channel full")
	result := app.JoinChannel(1)
	if result != "channel full" {
		t.Errorf("expected 'channel full', got %q", result)
	}
}

// ===========================================================================
// CreateChannel
// ===========================================================================

func TestCreateChannelSuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.CreateChannel("General")
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.channelsCreated) != 1 || mt.channelsCreated[0] != "General" {
		t.Errorf("expected create 'General', got %v", mt.channelsCreated)
	}
}

func TestCreateChannelError(t *testing.T) {
	app, mt := newTestApp()
	mt.createChannelErr = errors.New("not owner")
	result := app.CreateChannel("x")
	if result != "not owner" {
		t.Errorf("expected 'not owner', got %q", result)
	}
}

// ===========================================================================
// RenameChannel
// ===========================================================================

func TestRenameChannelSuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.RenameChannel(5, "New Name")
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.channelsRenamed) != 1 || mt.channelsRenamed[0].id != 5 || mt.channelsRenamed[0].name != "New Name" {
		t.Errorf("unexpected rename: %v", mt.channelsRenamed)
	}
}

func TestRenameChannelError(t *testing.T) {
	app, mt := newTestApp()
	mt.renameChannelErr = errors.New("not allowed")
	result := app.RenameChannel(1, "x")
	if result != "not allowed" {
		t.Errorf("expected 'not allowed', got %q", result)
	}
}

// ===========================================================================
// DeleteChannel
// ===========================================================================

func TestDeleteChannelSuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.DeleteChannel(8)
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.channelsDeleted) != 1 || mt.channelsDeleted[0] != 8 {
		t.Errorf("expected delete channel 8, got %v", mt.channelsDeleted)
	}
}

func TestDeleteChannelError(t *testing.T) {
	app, mt := newTestApp()
	mt.deleteChannelErr = errors.New("not owner")
	result := app.DeleteChannel(1)
	if result != "not owner" {
		t.Errorf("expected 'not owner', got %q", result)
	}
}

// ===========================================================================
// MoveUserToChannel
// ===========================================================================

func TestMoveUserToChannelSuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.MoveUserToChannel(3, 10)
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.usersMovedTo) != 1 || mt.usersMovedTo[0].userID != 3 || mt.usersMovedTo[0].channelID != 10 {
		t.Errorf("unexpected move: %v", mt.usersMovedTo)
	}
}

func TestMoveUserToChannelError(t *testing.T) {
	app, mt := newTestApp()
	mt.moveUserErr = errors.New("not owner")
	result := app.MoveUserToChannel(1, 2)
	if result != "not owner" {
		t.Errorf("expected 'not owner', got %q", result)
	}
}

// ===========================================================================
// Video / Screen Share
// ===========================================================================

func TestStartVideo(t *testing.T) {
	app, mt := newTestApp()
	result := app.StartVideo()
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.videoStates) != 1 || !mt.videoStates[0].active || mt.videoStates[0].screenShare {
		t.Errorf("expected (true, false), got %+v", mt.videoStates)
	}
}

func TestStopVideo(t *testing.T) {
	app, mt := newTestApp()
	result := app.StopVideo()
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.videoStates) != 1 || mt.videoStates[0].active || mt.videoStates[0].screenShare {
		t.Errorf("expected (false, false), got %+v", mt.videoStates)
	}
}

func TestStartScreenShare(t *testing.T) {
	app, mt := newTestApp()
	result := app.StartScreenShare()
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.videoStates) != 1 || !mt.videoStates[0].active || !mt.videoStates[0].screenShare {
		t.Errorf("expected (true, true), got %+v", mt.videoStates)
	}
}

func TestStopScreenShare(t *testing.T) {
	app, mt := newTestApp()
	result := app.StopScreenShare()
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.videoStates) != 1 || mt.videoStates[0].active || mt.videoStates[0].screenShare {
		t.Errorf("expected (false, false), got %+v", mt.videoStates)
	}
}

func TestStartVideoError(t *testing.T) {
	app, mt := newTestApp()
	mt.sendVideoStateErr = errors.New("not connected")
	result := app.StartVideo()
	if result != "not connected" {
		t.Errorf("expected 'not connected', got %q", result)
	}
}

func TestStopVideoError(t *testing.T) {
	app, mt := newTestApp()
	mt.sendVideoStateErr = errors.New("not connected")
	result := app.StopVideo()
	if result != "not connected" {
		t.Errorf("expected 'not connected', got %q", result)
	}
}

func TestStartScreenShareError(t *testing.T) {
	app, mt := newTestApp()
	mt.sendVideoStateErr = errors.New("not connected")
	result := app.StartScreenShare()
	if result != "not connected" {
		t.Errorf("expected 'not connected', got %q", result)
	}
}

func TestStopScreenShareError(t *testing.T) {
	app, mt := newTestApp()
	mt.sendVideoStateErr = errors.New("not connected")
	result := app.StopScreenShare()
	if result != "not connected" {
		t.Errorf("expected 'not connected', got %q", result)
	}
}

// ===========================================================================
// Recording
// ===========================================================================

func TestStartRecordingSuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.StartRecording(3)
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.recordingsStart) != 1 || mt.recordingsStart[0] != 3 {
		t.Errorf("expected start recording in channel 3, got %v", mt.recordingsStart)
	}
}

func TestStartRecordingError(t *testing.T) {
	app, mt := newTestApp()
	mt.startRecordingErr = errors.New("not owner")
	result := app.StartRecording(1)
	if result != "not owner" {
		t.Errorf("expected 'not owner', got %q", result)
	}
}

func TestStopRecordingSuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.StopRecording(3)
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.recordingsStop) != 1 || mt.recordingsStop[0] != 3 {
		t.Errorf("expected stop recording in channel 3, got %v", mt.recordingsStop)
	}
}

func TestStopRecordingError(t *testing.T) {
	app, mt := newTestApp()
	mt.stopRecordingErr = errors.New("not recording")
	result := app.StopRecording(1)
	if result != "not recording" {
		t.Errorf("expected 'not recording', got %q", result)
	}
}

// ===========================================================================
// RequestVideoQuality
// ===========================================================================

func TestRequestVideoQualitySuccess(t *testing.T) {
	app, mt := newTestApp()
	result := app.RequestVideoQuality(5, "high")
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.videoQualityReqs) != 1 || mt.videoQualityReqs[0].targetID != 5 || mt.videoQualityReqs[0].quality != "high" {
		t.Errorf("unexpected video quality request: %v", mt.videoQualityReqs)
	}
}

func TestRequestVideoQualityError(t *testing.T) {
	app, mt := newTestApp()
	mt.requestVideoQualErr = errors.New("invalid quality")
	result := app.RequestVideoQuality(1, "ultra")
	if result != "invalid quality" {
		t.Errorf("expected 'invalid quality', got %q", result)
	}
}

// ===========================================================================
// Audio controls (delegating to AudioEngine)
// ===========================================================================

func TestSetVolume(t *testing.T) {
	app, _ := newTestApp()
	app.SetVolume(0.5)
	// Volume is stored inside AudioEngine; we verify it doesn't panic.
}

func TestSetMuted(t *testing.T) {
	app, _ := newTestApp()
	app.SetMuted(true)
	// Internally sets muted on the audio engine + plays notification.
}

func TestSetDeafened(t *testing.T) {
	app, _ := newTestApp()
	app.SetDeafened(true)
	// Internally sets deafened on the audio engine + plays notification.
}

func TestSetAEC(t *testing.T) {
	app, _ := newTestApp()
	app.SetAEC(true)
	app.SetAEC(false)
}

func TestSetAGC(t *testing.T) {
	app, _ := newTestApp()
	app.SetAGC(true)
	app.SetAGC(false)
}

func TestSetAGCLevel(t *testing.T) {
	app, _ := newTestApp()
	app.SetAGCLevel(75)
}

func TestSetPTTMode(t *testing.T) {
	app, _ := newTestApp()
	app.SetPTTMode(true)
}

func TestPTTKeyDownUp(t *testing.T) {
	app, _ := newTestApp()
	app.PTTKeyDown()
	app.PTTKeyUp()
}

func TestSetInputDevice(t *testing.T) {
	app, _ := newTestApp()
	app.SetInputDevice(2)
}

func TestSetOutputDevice(t *testing.T) {
	app, _ := newTestApp()
	app.SetOutputDevice(3)
}

func TestGetInputLevel(t *testing.T) {
	app, _ := newTestApp()
	lvl := app.GetInputLevel()
	if lvl < 0 || lvl > 1 {
		t.Errorf("expected level in [0,1], got %f", lvl)
	}
}

func TestSetNotificationVolume(t *testing.T) {
	app, _ := newTestApp()
	app.SetNotificationVolume(0.5)
}

func TestGetNotificationVolume(t *testing.T) {
	app, _ := newTestApp()
	app.SetNotificationVolume(0.75)
	vol := app.GetNotificationVolume()
	if vol < 0.74 || vol > 0.76 {
		t.Errorf("expected ~0.75, got %f", vol)
	}
}

// ===========================================================================
// Muting (delegating to transport)
// ===========================================================================

func TestMuteUser(t *testing.T) {
	app, mt := newTestApp()
	app.MuteUser(10)
	if !mt.IsUserMuted(10) {
		t.Error("expected user 10 to be muted")
	}
}

func TestUnmuteUser(t *testing.T) {
	app, mt := newTestApp()
	app.MuteUser(10)
	app.UnmuteUser(10)
	if mt.IsUserMuted(10) {
		t.Error("expected user 10 to be unmuted")
	}
}

func TestGetMutedUsers(t *testing.T) {
	app, _ := newTestApp()
	app.MuteUser(1)
	app.MuteUser(2)
	ids := app.GetMutedUsers()
	if len(ids) != 2 {
		t.Errorf("expected 2 muted users, got %d", len(ids))
	}
}

// ===========================================================================
// Per-user volume (delegating to transport)
// ===========================================================================

func TestSetUserVolume(t *testing.T) {
	app, mt := newTestApp()
	app.SetUserVolume(5, 1.5)
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if mt.userVolumes[5] != 1.5 {
		t.Errorf("expected volume 1.5, got %f", mt.userVolumes[5])
	}
}

func TestGetUserVolumeDefault(t *testing.T) {
	app, _ := newTestApp()
	vol := app.GetUserVolume(99)
	if vol != 1.0 {
		t.Errorf("expected default volume 1.0, got %f", vol)
	}
}

func TestGetUserVolumeAfterSet(t *testing.T) {
	app, _ := newTestApp()
	app.SetUserVolume(5, 0.3)
	vol := app.GetUserVolume(5)
	if vol != 0.3 {
		t.Errorf("expected volume 0.3, got %f", vol)
	}
}

// ===========================================================================
// GetStartupAddr / GetAutoLogin
// ===========================================================================

func TestGetStartupAddrDefault(t *testing.T) {
	app, _ := newTestApp()
	if addr := app.GetStartupAddr(); addr != "" {
		t.Errorf("expected empty startup addr, got %q", addr)
	}
}

func TestGetStartupAddrSet(t *testing.T) {
	app, _ := newTestApp()
	app.startupAddr = "192.168.1.5:8080"
	if addr := app.GetStartupAddr(); addr != "192.168.1.5:8080" {
		t.Errorf("expected '192.168.1.5:8080', got %q", addr)
	}
}

func TestGetAutoLoginDefaults(t *testing.T) {
	app, _ := newTestApp()
	// Clear env to test default behavior.
	t.Setenv("BKEN_USERNAME", "")
	t.Setenv("BKEN_ADDR", "")
	login := app.GetAutoLogin()
	if login.Username != "" {
		t.Errorf("expected empty username, got %q", login.Username)
	}
	if login.Addr != "localhost:8080" {
		t.Errorf("expected 'localhost:8080', got %q", login.Addr)
	}
}

func TestGetAutoLoginFromEnv(t *testing.T) {
	app, _ := newTestApp()
	t.Setenv("BKEN_USERNAME", "testuser")
	t.Setenv("BKEN_ADDR", "10.0.0.1:5000")
	login := app.GetAutoLogin()
	if login.Username != "testuser" {
		t.Errorf("expected 'testuser', got %q", login.Username)
	}
	if login.Addr != "10.0.0.1:5000" {
		t.Errorf("expected '10.0.0.1:5000', got %q", login.Addr)
	}
}

// ===========================================================================
// GetConfig / SaveConfig (thin wrappers)
// ===========================================================================

func TestGetConfigReturnsConfig(t *testing.T) {
	app, _ := newTestApp()
	// Use a temp dir so we get defaults.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := app.GetConfig()
	if cfg.Theme != "dark" {
		t.Errorf("expected default theme 'dark', got %q", cfg.Theme)
	}
}

func TestSaveConfigNoError(t *testing.T) {
	app, _ := newTestApp()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := LoadConfig()
	cfg.Username = "test-save"
	app.SaveConfig(cfg)
	// Verify round-trip.
	loaded := app.GetConfig()
	if loaded.Username != "test-save" {
		t.Errorf("expected 'test-save', got %q", loaded.Username)
	}
}

// ===========================================================================
// GetMetrics (cached)
// ===========================================================================

func TestGetMetricsReturnsCache(t *testing.T) {
	app, _ := newTestApp()
	app.metricsMu.Lock()
	app.cachedMetrics = Metrics{RTTMs: 42.5, PacketLoss: 0.01}
	app.metricsMu.Unlock()
	m := app.GetMetrics()
	if m.RTTMs != 42.5 {
		t.Errorf("expected RTT 42.5, got %f", m.RTTMs)
	}
	if m.PacketLoss != 0.01 {
		t.Errorf("expected loss 0.01, got %f", m.PacketLoss)
	}
}

// ===========================================================================
// fileURL
// ===========================================================================

func TestFileURLWithBase(t *testing.T) {
	app, mt := newTestApp()
	mt.apiBaseURLVal = "http://localhost:8080"
	url := app.fileURL(123)
	if url != "http://localhost:8080/api/files/123" {
		t.Errorf("expected 'http://localhost:8080/api/files/123', got %q", url)
	}
}

func TestFileURLNoBase(t *testing.T) {
	app, _ := newTestApp()
	url := app.fileURL(123)
	if url != "" {
		t.Errorf("expected empty URL, got %q", url)
	}
}

// ===========================================================================
// UploadFileFromPath validation
// ===========================================================================

func TestUploadFileFromPathEmpty(t *testing.T) {
	app, _ := newTestApp()
	result := app.UploadFileFromPath(1, "")
	if result != "no file path" {
		t.Errorf("expected 'no file path', got %q", result)
	}
}

func TestUploadFileFromPathNoAPIBase(t *testing.T) {
	app, _ := newTestApp()
	// apiBaseURL is empty, so uploadFilePath should return error.
	result := app.UploadFileFromPath(1, "/tmp/nonexistent.txt")
	if result != "server API not available" {
		t.Errorf("expected 'server API not available', got %q", result)
	}
}

// ===========================================================================
// DisconnectVoice / ConnectVoice (partial tests: no audio start)
// ===========================================================================

func TestDisconnectVoiceJoinsLobby(t *testing.T) {
	app, mt := newTestApp()
	app.serverAddr = "localhost:8080"
	app.connected.Store(true)
	// DisconnectVoice calls transport.JoinChannel(0).
	result := app.DisconnectVoice()
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	mt.mu.Lock()
	defer mt.mu.Unlock()
	if len(mt.channelsJoined) != 1 || mt.channelsJoined[0] != 0 {
		t.Errorf("expected join channel 0, got %v", mt.channelsJoined)
	}
}

func TestDisconnectVoiceError(t *testing.T) {
	app, mt := newTestApp()
	app.serverAddr = "localhost:8080"
	app.connected.Store(true)
	mt.joinChannelErr = errors.New("not connected")
	result := app.DisconnectVoice()
	if result != "not connected" {
		t.Errorf("expected 'not connected', got %q", result)
	}
	// connected must be cleared even on error — audio is already stopped
	if app.connected.Load() {
		t.Error("expected connected=false after DisconnectVoice error")
	}
}

// ===========================================================================
// wireCallbacks: verify all callbacks are set
// ===========================================================================

func TestWireCallbacksSetsAllCallbacks(t *testing.T) {
	app, mt := newTestApp()
	app.ctx = context.Background() // avoid nil panic in callback bodies
	app.wireCallbacks()

	// Verify every callback was set on the mock transport.
	if mt.onUserList == nil {
		t.Error("onUserList not set")
	}
	if mt.onUserJoined == nil {
		t.Error("onUserJoined not set")
	}
	if mt.onUserLeft == nil {
		t.Error("onUserLeft not set")
	}
	if mt.onAudioReceived == nil {
		t.Error("onAudioReceived not set")
	}
	if mt.onDisconnected == nil {
		t.Error("onDisconnected not set")
	}
	if mt.onChatMessage == nil {
		t.Error("onChatMessage not set")
	}
	if mt.onChannelChatMessage == nil {
		t.Error("onChannelChatMessage not set")
	}
	if mt.onLinkPreview == nil {
		t.Error("onLinkPreview not set")
	}
	if mt.onServerInfo == nil {
		t.Error("onServerInfo not set")
	}
	if mt.onKicked == nil {
		t.Error("onKicked not set")
	}
	if mt.onOwnerChanged == nil {
		t.Error("onOwnerChanged not set")
	}
	if mt.onChannelList == nil {
		t.Error("onChannelList not set")
	}
	if mt.onUserChannel == nil {
		t.Error("onUserChannel not set")
	}
	if mt.onUserRenamed == nil {
		t.Error("onUserRenamed not set")
	}
	if mt.onMessageEdited == nil {
		t.Error("onMessageEdited not set")
	}
	if mt.onMessageDeleted == nil {
		t.Error("onMessageDeleted not set")
	}
	if mt.onVideoState == nil {
		t.Error("onVideoState not set")
	}
	if mt.onReactionAdded == nil {
		t.Error("onReactionAdded not set")
	}
	if mt.onReactionRemoved == nil {
		t.Error("onReactionRemoved not set")
	}
	if mt.onUserTyping == nil {
		t.Error("onUserTyping not set")
	}
	if mt.onMessagePinned == nil {
		t.Error("onMessagePinned not set")
	}
	if mt.onMessageUnpinned == nil {
		t.Error("onMessageUnpinned not set")
	}
	if mt.onRecordingState == nil {
		t.Error("onRecordingState not set")
	}
	if mt.onVideoLayers == nil {
		t.Error("onVideoLayers not set")
	}
	if mt.onVideoQualityReq == nil {
		t.Error("onVideoQualityReq not set")
	}
}

// ===========================================================================
// sendLoop: consecutive error threshold triggers disconnect
// ===========================================================================

// mockFailTransport returns errors from SendAudio.
type mockFailTransport struct {
	mockTransport
	sendAudioErr error
	sendCount    int
	mu2          sync.Mutex
}

func (m *mockFailTransport) SendAudio(_ []byte) error {
	m.mu2.Lock()
	defer m.mu2.Unlock()
	m.sendCount++
	return m.sendAudioErr
}

func TestSendLoopDisconnectsAfterThreshold(t *testing.T) {
	mt := &mockFailTransport{
		mockTransport: *newMockTransport(),
		sendAudioErr:  errors.New("send failed"),
	}
	app := &App{
		audio:     NewAudioEngine(),
		transport: mt,
	}
	app.serverAddr = "localhost:8080"

	// Create channels that the send loop uses.
	app.audio.CaptureOut = make(chan []byte, sendFailureThreshold+10)
	app.audio.stopCh = make(chan struct{})

	// Fill capture channel with frames.
	for i := 0; i < sendFailureThreshold+5; i++ {
		app.audio.CaptureOut <- []byte{0x01, 0x02}
	}

	// Run sendLoop; it should exit after sendFailureThreshold consecutive errors.
	done := make(chan struct{})
	go func() {
		app.sendLoop()
		close(done)
	}()

	<-done
	mt.mu2.Lock()
	count := mt.sendCount
	mt.mu2.Unlock()

	if count < sendFailureThreshold {
		t.Errorf("expected at least %d send attempts, got %d", sendFailureThreshold, count)
	}
}

// ===========================================================================
// Interface compliance: App has all Wails-bound methods
// ===========================================================================

// This test doesn't verify interface compliance (App doesn't implement an interface),
// but it verifies the struct can be constructed and key methods exist.
func TestAppConstructionViaNewApp(t *testing.T) {
	app := NewApp()
	if app == nil {
		t.Fatal("NewApp returned nil")
	}
	if app.audio == nil {
		t.Error("audio engine is nil")
	}
	if app.transport == nil {
		t.Error("transport is nil")
	}
	if app.IsConnected() {
		t.Error("newly created app should not be connected")
	}
}
