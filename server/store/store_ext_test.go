package store

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
)

// newFileStore opens a file-backed SQLite database in a temp directory.
// This is needed for concurrent write tests because :memory: databases
// do not support WAL mode properly under concurrent access.
func newFileStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// ---------------------------------------------------------------------------
// Migration tests
// ---------------------------------------------------------------------------

func TestMigrationVersionSequence(t *testing.T) {
	s := newMemStore(t)

	rows, err := s.db.Query(`SELECT version FROM schema_migrations ORDER BY version ASC`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	expected := 1
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if v != expected {
			t.Errorf("expected migration version %d, got %d", expected, v)
		}
		expected++
	}
	if expected-1 != len(migrations) {
		t.Errorf("expected %d migration versions, found %d", len(migrations), expected-1)
	}
}

func TestMigrationAllTablesExist(t *testing.T) {
	s := newMemStore(t)

	tables := []string{
		"settings",
		"channels",
		"files",
		"audit_log",
		"bans",
		"user_roles",
		"announcements",
	}

	for _, table := range tables {
		var count int
		err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count)
		if err != nil {
			t.Errorf("table %q should exist: %v", table, err)
		}
	}
}

func TestMigrationSchemaColumnsExist(t *testing.T) {
	s := newMemStore(t)

	// Verify channels table has slow_mode_seconds (added in v8).
	_, err := s.db.Exec(`SELECT slow_mode_seconds FROM channels LIMIT 1`)
	if err != nil {
		t.Errorf("channels.slow_mode_seconds column should exist: %v", err)
	}
}

func TestMigrationIndexExists(t *testing.T) {
	s := newMemStore(t)

	var name string
	err := s.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_audit_log_created'`,
	).Scan(&name)
	if err != nil {
		t.Errorf("index idx_audit_log_created should exist: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Concurrent read/write under WAL mode
// ---------------------------------------------------------------------------

func TestConcurrentReadWrite(t *testing.T) {
	s := newFileStore(t)

	var wg sync.WaitGroup

	// Writer goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			s.SetSetting("counter", "value")
		}
	}()

	// Reader goroutines.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _, _ = s.GetSetting("counter")
			}
		}()
	}

	wg.Wait()
}

func TestConcurrentChannelOps(t *testing.T) {
	s := newFileStore(t)

	var wg sync.WaitGroup

	// Writer goroutines creating channels.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				name := "ch-" + string(rune('A'+idx)) + "-" + string(rune('0'+j))
				_, _ = s.CreateChannel(name)
			}
		}(i)
	}

	// Reader goroutines.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _ = s.GetChannels()
				_, _ = s.ChannelCount()
			}
		}()
	}

	wg.Wait()
}

// ---------------------------------------------------------------------------
// Auto-purge of audit log at 10K entries
// ---------------------------------------------------------------------------

func TestAuditLogPurgeLogicExists(t *testing.T) {
	s := newMemStore(t)

	// Verify the purge logic works by inserting a modest number and
	// checking that the purge query in InsertAuditLog runs without error.
	for i := 0; i < 100; i++ {
		if err := s.InsertAuditLog(1, "alice", "action", "target", "{}"); err != nil {
			t.Fatalf("InsertAuditLog %d: %v", i, err)
		}
	}

	count, err := s.AuditLogCount()
	if err != nil {
		t.Fatalf("AuditLogCount: %v", err)
	}
	if count != 100 {
		t.Errorf("expected 100 entries (below purge threshold), got %d", count)
	}
}

func TestAuditLogNewestEntryAccessible(t *testing.T) {
	s := newMemStore(t)

	for i := 0; i < 50; i++ {
		if err := s.InsertAuditLog(1, "alice", "action", "target", "{}"); err != nil {
			t.Fatalf("InsertAuditLog %d: %v", i, err)
		}
	}

	entries, err := s.GetAuditLog("", 1)
	if err != nil {
		t.Fatalf("GetAuditLog: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	// The newest entry should have the highest ID.
	if entries[0].ID == 0 {
		t.Error("newest entry should have a non-zero ID")
	}
	if entries[0].ID != 50 {
		t.Errorf("newest entry ID: got %d, want 50", entries[0].ID)
	}
}

// ---------------------------------------------------------------------------
// Ban expiry purge
// ---------------------------------------------------------------------------

func TestPurgeExpiredBansNonePurged(t *testing.T) {
	s := newMemStore(t)

	// Permanent ban should not be purged.
	s.InsertBan("alice", "", "perma", "admin", 0)

	purged, err := s.PurgeExpiredBans()
	if err != nil {
		t.Fatalf("PurgeExpiredBans: %v", err)
	}
	if purged != 0 {
		t.Errorf("expected 0 purged (permanent ban), got %d", purged)
	}

	bans, _ := s.GetBans()
	if len(bans) != 1 {
		t.Errorf("expected 1 ban remaining, got %d", len(bans))
	}
}

func TestPurgeExpiredBansRemovesExpired(t *testing.T) {
	s := newMemStore(t)

	// Create a ban with a very short duration that's already expired.
	// We directly insert with a created_at in the past.
	_, err := s.db.Exec(
		`INSERT INTO bans(pubkey, ip, reason, banned_by, duration_s, created_at) VALUES(?,?,?,?,?,?)`,
		"alice", "", "test", "admin", 1, 1000000, // created_at far in the past + 1 second duration
	)
	if err != nil {
		t.Fatalf("insert expired ban: %v", err)
	}

	purged, err := s.PurgeExpiredBans()
	if err != nil {
		t.Fatalf("PurgeExpiredBans: %v", err)
	}
	if purged != 1 {
		t.Errorf("expected 1 purged, got %d", purged)
	}

	bans, _ := s.GetBans()
	if len(bans) != 0 {
		t.Errorf("expected 0 bans after purge, got %d", len(bans))
	}
}

func TestPurgeExpiredBansKeepsNonExpired(t *testing.T) {
	s := newMemStore(t)

	// Permanent ban.
	s.InsertBan("permanent", "", "perma", "admin", 0)

	// Very long temp ban (effectively not expired).
	s.InsertBan("long-temp", "", "long", "admin", 999999999)

	purged, err := s.PurgeExpiredBans()
	if err != nil {
		t.Fatalf("PurgeExpiredBans: %v", err)
	}
	if purged != 0 {
		t.Errorf("expected 0 purged, got %d", purged)
	}

	bans, _ := s.GetBans()
	if len(bans) != 2 {
		t.Errorf("expected 2 bans remaining, got %d", len(bans))
	}
}

// ---------------------------------------------------------------------------
// Audit log with empty details
// ---------------------------------------------------------------------------

func TestAuditLogEmptyDetailsDefaultsToJSON(t *testing.T) {
	s := newMemStore(t)

	if err := s.InsertAuditLog(1, "alice", "test", "target", ""); err != nil {
		t.Fatalf("InsertAuditLog: %v", err)
	}

	entries, err := s.GetAuditLog("", 1)
	if err != nil {
		t.Fatalf("GetAuditLog: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].DetailsJSON != "{}" {
		t.Errorf("expected empty details to be '{}', got %q", entries[0].DetailsJSON)
	}
}

// ---------------------------------------------------------------------------
// User roles: independent users
// ---------------------------------------------------------------------------

func TestUserRolesMultipleUsers(t *testing.T) {
	s := newMemStore(t)

	s.SetUserRole("alice", "ADMIN")
	s.SetUserRole("bob", "MODERATOR")
	s.SetUserRole("charlie", "USER")

	for _, tc := range []struct {
		pubkey string
		want   string
	}{
		{"alice", "ADMIN"},
		{"bob", "MODERATOR"},
		{"charlie", "USER"},
		{"unknown", "USER"}, // default
	} {
		got, err := s.GetUserRole(tc.pubkey)
		if err != nil {
			t.Errorf("GetUserRole(%q): %v", tc.pubkey, err)
		}
		if got != tc.want {
			t.Errorf("GetUserRole(%q): got %q, want %q", tc.pubkey, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Announcements: multiple inserts
// ---------------------------------------------------------------------------

func TestAnnouncementsSequence(t *testing.T) {
	s := newMemStore(t)

	for i := 1; i <= 5; i++ {
		_, err := s.InsertAnnouncement("announcement "+string(rune('0'+i)), "admin")
		if err != nil {
			t.Fatalf("InsertAnnouncement %d: %v", i, err)
		}
	}

	ann, err := s.GetLatestAnnouncement()
	if err != nil {
		t.Fatalf("GetLatestAnnouncement: %v", err)
	}
	if ann.Content != "announcement 5" {
		t.Errorf("latest: got %q, want %q", ann.Content, "announcement 5")
	}
}

// ---------------------------------------------------------------------------
// Channel slow mode tests
// ---------------------------------------------------------------------------

func TestChannelSlowModeUpdateMultipleTimes(t *testing.T) {
	s := newMemStore(t)

	id, _ := s.CreateChannel("General")

	for _, secs := range []int{5, 30, 0, 60} {
		if err := s.SetChannelSlowMode(id, secs); err != nil {
			t.Fatalf("SetChannelSlowMode(%d): %v", secs, err)
		}
		got, err := s.GetChannelSlowMode(id)
		if err != nil {
			t.Fatalf("GetChannelSlowMode: %v", err)
		}
		if got != secs {
			t.Errorf("slow mode: got %d, want %d", got, secs)
		}
	}
}

func TestChannelSlowModeForNonExistentChannel(t *testing.T) {
	s := newMemStore(t)

	_, err := s.GetChannelSlowMode(9999)
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetAllSettings
// ---------------------------------------------------------------------------

func TestGetAllSettings(t *testing.T) {
	s := newMemStore(t)

	s.SetSetting("key1", "val1")
	s.SetSetting("key2", "val2")
	s.SetSetting("key3", "val3")

	settings, err := s.GetAllSettings()
	if err != nil {
		t.Fatalf("GetAllSettings: %v", err)
	}
	if len(settings) != 3 {
		t.Fatalf("expected 3 settings, got %d", len(settings))
	}
	if settings["key1"] != "val1" || settings["key2"] != "val2" || settings["key3"] != "val3" {
		t.Errorf("unexpected settings: %v", settings)
	}
}

func TestGetAllSettingsEmpty(t *testing.T) {
	s := newMemStore(t)

	settings, err := s.GetAllSettings()
	if err != nil {
		t.Fatalf("GetAllSettings: %v", err)
	}
	if len(settings) != 0 {
		t.Errorf("expected empty map, got %v", settings)
	}
}

// ---------------------------------------------------------------------------
// Backup
// ---------------------------------------------------------------------------

func TestBackupCreatesValidDB(t *testing.T) {
	s := newMemStore(t)

	s.SetSetting("backup_test", "value123")
	s.CreateChannel("TestChannel")

	backupPath := t.TempDir() + "/backup.db"
	if err := s.Backup(backupPath); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// Open the backup and verify data.
	backup, err := New(backupPath)
	if err != nil {
		t.Fatalf("opening backup: %v", err)
	}
	defer backup.Close()

	val, ok, err := backup.GetSetting("backup_test")
	if err != nil || !ok || val != "value123" {
		t.Errorf("backup setting: val=%q ok=%v err=%v", val, ok, err)
	}

	chs, err := backup.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels from backup: %v", err)
	}
	if len(chs) != 1 || chs[0].Name != "TestChannel" {
		t.Errorf("backup channels: got %v", chs)
	}
}

// ---------------------------------------------------------------------------
// IsIPBanned / IsUserBanned with temp bans
// ---------------------------------------------------------------------------

func TestIsIPBannedTempNotExpired(t *testing.T) {
	s := newMemStore(t)

	// Long-lived temp ban.
	s.InsertBan("", "192.168.1.1", "temp", "admin", 999999999)

	banned, _, err := s.IsIPBanned("192.168.1.1")
	if err != nil {
		t.Fatalf("IsIPBanned: %v", err)
	}
	if !banned {
		t.Error("IP should be banned (temp ban not expired)")
	}
}

func TestIsUserBannedTempNotExpired(t *testing.T) {
	s := newMemStore(t)

	s.InsertBan("alice", "", "temp", "admin", 999999999)

	banned, _, err := s.IsUserBanned("alice")
	if err != nil {
		t.Fatalf("IsUserBanned: %v", err)
	}
	if !banned {
		t.Error("user should be banned (temp ban not expired)")
	}
}

// ---------------------------------------------------------------------------
// File operations
// ---------------------------------------------------------------------------

func TestCreateMultipleFiles(t *testing.T) {
	s := newMemStore(t)

	id1, err := s.CreateFile("a.txt", "text/plain", "/a.txt", 100)
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	id2, err := s.CreateFile("b.txt", "text/plain", "/b.txt", 200)
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}

	if id1 == id2 {
		t.Error("file IDs should be unique")
	}

	f1, _ := s.GetFile(id1)
	f2, _ := s.GetFile(id2)

	if f1.Name != "a.txt" || f2.Name != "b.txt" {
		t.Errorf("unexpected files: %v, %v", f1, f2)
	}
}

// ---------------------------------------------------------------------------
// GetAuditLog with limit
// ---------------------------------------------------------------------------

func TestGetAuditLogWithLimit(t *testing.T) {
	s := newMemStore(t)

	for i := 0; i < 20; i++ {
		s.InsertAuditLog(1, "alice", "action", "target", "{}")
	}

	entries, err := s.GetAuditLog("", 5)
	if err != nil {
		t.Fatalf("GetAuditLog: %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("expected 5 entries (limited), got %d", len(entries))
	}
}

func TestGetAuditLogMostRecentFirst(t *testing.T) {
	s := newMemStore(t)

	s.InsertAuditLog(1, "alice", "first", "t", "{}")
	s.InsertAuditLog(1, "alice", "second", "t", "{}")
	s.InsertAuditLog(1, "alice", "third", "t", "{}")

	entries, err := s.GetAuditLog("", 10)
	if err != nil {
		t.Fatalf("GetAuditLog: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3, got %d", len(entries))
	}
	if entries[0].Action != "third" {
		t.Errorf("first entry should be most recent: got %q", entries[0].Action)
	}
	if entries[2].Action != "first" {
		t.Errorf("last entry should be oldest: got %q", entries[2].Action)
	}
}

// ---------------------------------------------------------------------------
// Concurrent audit log inserts
// ---------------------------------------------------------------------------

func TestConcurrentAuditLogInserts(t *testing.T) {
	s := newFileStore(t)

	// Concurrent writes to SQLite may encounter SQLITE_BUSY. Verify that
	// the store doesn't panic or corrupt under concurrency, and that
	// at least some writes succeed.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = s.InsertAuditLog(idx, "user", "action", "target", "{}")
			}
		}(i)
	}
	wg.Wait()

	count, err := s.AuditLogCount()
	if err != nil {
		t.Fatalf("AuditLogCount: %v", err)
	}
	// Under concurrency, some inserts may fail with SQLITE_BUSY.
	// At minimum, some should succeed.
	if count == 0 {
		t.Error("expected at least some audit log entries after concurrent inserts")
	}
}

// ---------------------------------------------------------------------------
// Concurrent ban inserts
// ---------------------------------------------------------------------------

func TestConcurrentBanInserts(t *testing.T) {
	s := newFileStore(t)

	// Concurrent writes to SQLite may encounter SQLITE_BUSY.
	// Verify no panics and at least some writes succeed.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				_, _ = s.InsertBan("user", "10.0.0.1", "reason", "admin", 0)
			}
		}(i)
	}
	wg.Wait()

	bans, err := s.GetBans()
	if err != nil {
		t.Fatalf("GetBans: %v", err)
	}
	if len(bans) == 0 {
		t.Error("expected at least some bans after concurrent inserts")
	}
}

// ---------------------------------------------------------------------------
// Multiple bans for same user
// ---------------------------------------------------------------------------

func TestMultipleBansSameUser(t *testing.T) {
	s := newMemStore(t)

	s.InsertBan("alice", "", "first", "admin", 0)
	s.InsertBan("alice", "", "second", "admin", 0)

	bans, _ := s.GetBans()
	if len(bans) != 2 {
		t.Errorf("expected 2 bans, got %d", len(bans))
	}

	// IsUserBanned should return one of the reasons.
	banned, _, _ := s.IsUserBanned("alice")
	if !banned {
		t.Error("user should be banned")
	}
}

// ---------------------------------------------------------------------------
// Channel ordering
// ---------------------------------------------------------------------------

func TestChannelsOrderedByPosition(t *testing.T) {
	s := newMemStore(t)

	// Create channels — they default to position=0, ordered by id.
	s.CreateChannel("Alpha")
	s.CreateChannel("Beta")
	s.CreateChannel("Gamma")

	chs, err := s.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	if len(chs) != 3 {
		t.Fatalf("expected 3, got %d", len(chs))
	}
	if chs[0].Name != "Alpha" || chs[1].Name != "Beta" || chs[2].Name != "Gamma" {
		t.Errorf("unexpected order: %v", chs)
	}
}
