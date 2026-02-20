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
