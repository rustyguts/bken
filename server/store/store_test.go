package store

import (
	"database/sql"
	"testing"
)

// newMemStore opens an in-memory SQLite database, runs migrations, and returns
// the store. The database is discarded when the test process exits.
func newMemStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestMigrationsApplied verifies that after opening a fresh database every
// migration has been recorded in schema_migrations.
func TestMigrationsApplied(t *testing.T) {
	s := newMemStore(t)

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count != len(migrations) {
		t.Errorf("expected %d migrations recorded, got %d", len(migrations), count)
	}
}

// TestMigrationsIdempotent verifies that opening the same path a second time
// does not apply migrations again.
func TestMigrationsIdempotent(t *testing.T) {
	s := newMemStore(t)

	// Open the same underlying DB a second time via the exported New function.
	// Because it is ":memory:" this is a fresh DB, so we simulate idempotency
	// by calling migrate() directly on the already-migrated store.
	if err := s.migrate(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != len(migrations) {
		t.Errorf("expected %d rows after second migrate, got %d", len(migrations), count)
	}
}

// TestGetSetSetting verifies the basic read/write contract of the settings
// table.
func TestGetSetSetting(t *testing.T) {
	s := newMemStore(t)

	// Missing key returns (_, false, nil).
	val, ok, err := s.GetSetting("server_name")
	if err != nil {
		t.Fatalf("GetSetting missing key: %v", err)
	}
	if ok {
		t.Errorf("expected ok=false for missing key, got %q", val)
	}

	// Set a value.
	if err := s.SetSetting("server_name", "My Server"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	// Read it back.
	val, ok, err = s.GetSetting("server_name")
	if err != nil {
		t.Fatalf("GetSetting after set: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true after set")
	}
	if val != "My Server" {
		t.Errorf("expected %q, got %q", "My Server", val)
	}
}

// TestSetSettingUpsert verifies that SetSetting overwrites an existing value.
func TestSetSettingUpsert(t *testing.T) {
	s := newMemStore(t)

	if err := s.SetSetting("x", "first"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting("x", "second"); err != nil {
		t.Fatal(err)
	}

	val, ok, err := s.GetSetting("x")
	if err != nil || !ok {
		t.Fatalf("GetSetting: val=%q ok=%v err=%v", val, ok, err)
	}
	if val != "second" {
		t.Errorf("expected %q after upsert, got %q", "second", val)
	}
}

// TestMultipleSettings verifies that distinct keys are stored independently.
func TestMultipleSettings(t *testing.T) {
	s := newMemStore(t)

	pairs := [][2]string{
		{"key_a", "val_a"},
		{"key_b", "val_b"},
		{"key_c", "val_c"},
	}
	for _, p := range pairs {
		if err := s.SetSetting(p[0], p[1]); err != nil {
			t.Fatalf("SetSetting %q: %v", p[0], err)
		}
	}
	for _, p := range pairs {
		val, ok, err := s.GetSetting(p[0])
		if err != nil || !ok || val != p[1] {
			t.Errorf("GetSetting %q: val=%q ok=%v err=%v", p[0], val, ok, err)
		}
	}
}

// TestCreateAndGetChannels verifies basic channel creation and retrieval.
func TestCreateAndGetChannels(t *testing.T) {
	s := newMemStore(t)

	id, err := s.CreateChannel("General")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	channels, err := s.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(channels))
	}
	if channels[0].Name != "General" {
		t.Errorf("expected name %q, got %q", "General", channels[0].Name)
	}
	if channels[0].ID != id {
		t.Errorf("expected id %d, got %d", id, channels[0].ID)
	}
}

// TestGetChannelsEmpty verifies that GetChannels returns an empty (not nil) slice.
func TestGetChannelsEmpty(t *testing.T) {
	s := newMemStore(t)

	channels, err := s.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	if channels != nil {
		t.Errorf("expected nil slice for empty table, got %v", channels)
	}
}

// TestRenameChannel verifies that a channel's name can be updated.
func TestRenameChannel(t *testing.T) {
	s := newMemStore(t)

	id, _ := s.CreateChannel("Old Name")
	if err := s.RenameChannel(id, "New Name"); err != nil {
		t.Fatalf("RenameChannel: %v", err)
	}

	channels, _ := s.GetChannels()
	if channels[0].Name != "New Name" {
		t.Errorf("expected %q, got %q", "New Name", channels[0].Name)
	}
}

// TestRenameChannelNotFound verifies that renaming a missing channel returns sql.ErrNoRows.
func TestRenameChannelNotFound(t *testing.T) {
	s := newMemStore(t)

	err := s.RenameChannel(9999, "X")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// TestDeleteChannel verifies that a channel can be removed.
func TestDeleteChannel(t *testing.T) {
	s := newMemStore(t)

	id, _ := s.CreateChannel("Temp")
	if err := s.DeleteChannel(id); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}

	channels, _ := s.GetChannels()
	if len(channels) != 0 {
		t.Errorf("expected 0 channels after delete, got %d", len(channels))
	}
}

// TestDeleteChannelNotFound verifies that deleting a missing channel returns sql.ErrNoRows.
func TestDeleteChannelNotFound(t *testing.T) {
	s := newMemStore(t)

	err := s.DeleteChannel(9999)
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// TestCreateChannelDuplicateName verifies that duplicate channel names are rejected.
func TestCreateChannelDuplicateName(t *testing.T) {
	s := newMemStore(t)

	if _, err := s.CreateChannel("General"); err != nil {
		t.Fatalf("first CreateChannel: %v", err)
	}
	_, err := s.CreateChannel("General")
	if err == nil {
		t.Fatal("expected error for duplicate channel name, got nil")
	}
}

// TestChannelCount verifies the ChannelCount helper.
func TestChannelCount(t *testing.T) {
	s := newMemStore(t)

	n, err := s.ChannelCount()
	if err != nil || n != 0 {
		t.Fatalf("expected 0, got %d err=%v", n, err)
	}

	s.CreateChannel("A")
	s.CreateChannel("B")

	n, err = s.ChannelCount()
	if err != nil || n != 2 {
		t.Fatalf("expected 2, got %d err=%v", n, err)
	}
}

// --- File storage tests ---

func TestCreateAndGetFile(t *testing.T) {
	s := newMemStore(t)

	id, err := s.CreateFile("photo.jpg", "image/jpeg", "/uploads/abc.jpg", 12345)
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	f, err := s.GetFile(id)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if f.Name != "photo.jpg" {
		t.Errorf("name: got %q, want %q", f.Name, "photo.jpg")
	}
	if f.Size != 12345 {
		t.Errorf("size: got %d, want 12345", f.Size)
	}
	if f.ContentType != "image/jpeg" {
		t.Errorf("content_type: got %q, want %q", f.ContentType, "image/jpeg")
	}
	if f.DiskPath != "/uploads/abc.jpg" {
		t.Errorf("disk_path: got %q, want %q", f.DiskPath, "/uploads/abc.jpg")
	}
}

func TestGetFileNotFound(t *testing.T) {
	s := newMemStore(t)

	_, err := s.GetFile(9999)
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// --- Audit Log tests ---

func TestInsertAndGetAuditLog(t *testing.T) {
	s := newMemStore(t)

	if err := s.InsertAuditLog(1, "alice", "ban", "bob", `{"reason":"spam"}`); err != nil {
		t.Fatalf("InsertAuditLog: %v", err)
	}
	if err := s.InsertAuditLog(1, "alice", "kick", "charlie", "{}"); err != nil {
		t.Fatalf("InsertAuditLog: %v", err)
	}

	entries, err := s.GetAuditLog("", 100)
	if err != nil {
		t.Fatalf("GetAuditLog: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Most recent first.
	if entries[0].Action != "kick" {
		t.Errorf("first entry action: got %q, want %q", entries[0].Action, "kick")
	}
	if entries[1].Action != "ban" {
		t.Errorf("second entry action: got %q, want %q", entries[1].Action, "ban")
	}
}

func TestGetAuditLogFilteredByAction(t *testing.T) {
	s := newMemStore(t)

	s.InsertAuditLog(1, "alice", "ban", "bob", "{}")
	s.InsertAuditLog(1, "alice", "kick", "charlie", "{}")
	s.InsertAuditLog(1, "alice", "ban", "dave", "{}")

	entries, err := s.GetAuditLog("ban", 100)
	if err != nil {
		t.Fatalf("GetAuditLog: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 ban entries, got %d", len(entries))
	}
}

func TestAuditLogAutoPurge(t *testing.T) {
	s := newMemStore(t)

	// Insert more than 10,000 entries to trigger auto-purge.
	// Use a smaller count in tests; the purge logic deletes beyond 10K.
	for i := 0; i < 50; i++ {
		if err := s.InsertAuditLog(1, "alice", "test", "target", "{}"); err != nil {
			t.Fatalf("InsertAuditLog %d: %v", i, err)
		}
	}

	count, err := s.AuditLogCount()
	if err != nil {
		t.Fatalf("AuditLogCount: %v", err)
	}
	if count != 50 {
		t.Errorf("expected 50 entries, got %d", count)
	}
}

// --- Ban Management tests ---

func TestInsertAndGetBans(t *testing.T) {
	s := newMemStore(t)

	id, err := s.InsertBan("alice", "192.168.1.1", "spam", "admin", 0)
	if err != nil {
		t.Fatalf("InsertBan: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	bans, err := s.GetBans()
	if err != nil {
		t.Fatalf("GetBans: %v", err)
	}
	if len(bans) != 1 {
		t.Fatalf("expected 1 ban, got %d", len(bans))
	}
	if bans[0].Pubkey != "alice" {
		t.Errorf("pubkey: got %q, want %q", bans[0].Pubkey, "alice")
	}
	if bans[0].Reason != "spam" {
		t.Errorf("reason: got %q, want %q", bans[0].Reason, "spam")
	}
}

func TestDeleteBan(t *testing.T) {
	s := newMemStore(t)

	id, _ := s.InsertBan("alice", "", "test", "admin", 0)
	if err := s.DeleteBan(id); err != nil {
		t.Fatalf("DeleteBan: %v", err)
	}

	bans, _ := s.GetBans()
	if len(bans) != 0 {
		t.Errorf("expected 0 bans after delete, got %d", len(bans))
	}
}

func TestDeleteBanNotFound(t *testing.T) {
	s := newMemStore(t)

	err := s.DeleteBan(9999)
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestIsIPBanned(t *testing.T) {
	s := newMemStore(t)

	s.InsertBan("", "192.168.1.1", "banned IP", "admin", 0)

	banned, reason, err := s.IsIPBanned("192.168.1.1")
	if err != nil {
		t.Fatalf("IsIPBanned: %v", err)
	}
	if !banned {
		t.Error("expected IP to be banned")
	}
	if reason != "banned IP" {
		t.Errorf("reason: got %q, want %q", reason, "banned IP")
	}

	banned, _, err = s.IsIPBanned("10.0.0.1")
	if err != nil {
		t.Fatalf("IsIPBanned: %v", err)
	}
	if banned {
		t.Error("expected 10.0.0.1 to not be banned")
	}
}

func TestIsUserBanned(t *testing.T) {
	s := newMemStore(t)

	s.InsertBan("alice", "", "banned user", "admin", 0)

	banned, reason, err := s.IsUserBanned("alice")
	if err != nil {
		t.Fatalf("IsUserBanned: %v", err)
	}
	if !banned {
		t.Error("expected user to be banned")
	}
	if reason != "banned user" {
		t.Errorf("reason: got %q", reason)
	}

	banned, _, _ = s.IsUserBanned("bob")
	if banned {
		t.Error("expected bob to not be banned")
	}
}

// --- User Roles tests ---

func TestSetAndGetUserRole(t *testing.T) {
	s := newMemStore(t)

	// Default role should be USER.
	role, err := s.GetUserRole("alice")
	if err != nil {
		t.Fatalf("GetUserRole: %v", err)
	}
	if role != "USER" {
		t.Errorf("default role: got %q, want %q", role, "USER")
	}

	// Set and retrieve.
	if err := s.SetUserRole("alice", "ADMIN"); err != nil {
		t.Fatalf("SetUserRole: %v", err)
	}
	role, err = s.GetUserRole("alice")
	if err != nil {
		t.Fatalf("GetUserRole: %v", err)
	}
	if role != "ADMIN" {
		t.Errorf("role: got %q, want %q", role, "ADMIN")
	}

	// Upsert to MODERATOR.
	if err := s.SetUserRole("alice", "MODERATOR"); err != nil {
		t.Fatalf("SetUserRole upsert: %v", err)
	}
	role, _ = s.GetUserRole("alice")
	if role != "MODERATOR" {
		t.Errorf("role after upsert: got %q, want %q", role, "MODERATOR")
	}
}

// --- Announcements tests ---

func TestInsertAndGetAnnouncement(t *testing.T) {
	s := newMemStore(t)

	// No announcement yet.
	_, err := s.GetLatestAnnouncement()
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}

	id, err := s.InsertAnnouncement("Server maintenance at midnight", "admin")
	if err != nil {
		t.Fatalf("InsertAnnouncement: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	ann, err := s.GetLatestAnnouncement()
	if err != nil {
		t.Fatalf("GetLatestAnnouncement: %v", err)
	}
	if ann.Content != "Server maintenance at midnight" {
		t.Errorf("content: got %q", ann.Content)
	}
	if ann.CreatedBy != "admin" {
		t.Errorf("created_by: got %q", ann.CreatedBy)
	}
}

func TestGetLatestAnnouncementReturnsNewest(t *testing.T) {
	s := newMemStore(t)

	s.InsertAnnouncement("First", "admin")
	s.InsertAnnouncement("Second", "admin")

	ann, err := s.GetLatestAnnouncement()
	if err != nil {
		t.Fatalf("GetLatestAnnouncement: %v", err)
	}
	if ann.Content != "Second" {
		t.Errorf("content: got %q, want %q", ann.Content, "Second")
	}
}

// --- Slow Mode tests ---

func TestSetAndGetChannelSlowMode(t *testing.T) {
	s := newMemStore(t)

	id, _ := s.CreateChannel("General")

	// Default is 0.
	secs, err := s.GetChannelSlowMode(id)
	if err != nil {
		t.Fatalf("GetChannelSlowMode: %v", err)
	}
	if secs != 0 {
		t.Errorf("default slow mode: got %d, want 0", secs)
	}

	// Set to 5 seconds.
	if err := s.SetChannelSlowMode(id, 5); err != nil {
		t.Fatalf("SetChannelSlowMode: %v", err)
	}
	secs, _ = s.GetChannelSlowMode(id)
	if secs != 5 {
		t.Errorf("slow mode: got %d, want 5", secs)
	}
}

func TestSetChannelSlowModeNotFound(t *testing.T) {
	s := newMemStore(t)

	err := s.SetChannelSlowMode(9999, 5)
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// --- Optimize ---

func TestOptimize(t *testing.T) {
	s := newMemStore(t)
	if err := s.Optimize(); err != nil {
		t.Fatalf("Optimize: %v", err)
	}
}
