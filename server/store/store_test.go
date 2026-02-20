package store

import (
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
