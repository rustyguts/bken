package main

import (
	"os"
	"path/filepath"
	"testing"

	"bken/server/store"
)

// cliDBSetup creates a temp directory with an initialized store and returns
// the database path. The directory is cleaned up when the test finishes.
func cliDBSetup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "bken.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	st.Close()
	return dbPath
}

// cliDBWithChannels creates a database pre-seeded with the given channels.
func cliDBWithChannels(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "bken.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	for _, name := range names {
		if _, err := st.CreateChannel(name); err != nil {
			t.Fatalf("CreateChannel(%q): %v", name, err)
		}
	}
	st.Close()
	return dbPath
}

// cliDBWithSettings creates a database pre-seeded with the given settings.
func cliDBWithSettings(t *testing.T, kv map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "bken.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	for k, v := range kv {
		if err := st.SetSetting(k, v); err != nil {
			t.Fatalf("SetSetting(%q, %q): %v", k, v, err)
		}
	}
	st.Close()
	return dbPath
}

// ---------------------------------------------------------------------------
// RunCLI: subcommand dispatch
// ---------------------------------------------------------------------------

func TestRunCLIVersionReturnsTrue(t *testing.T) {
	if !RunCLI([]string{"version"}, "not-used.db") {
		t.Error("RunCLI(version) should return true")
	}
}

func TestRunCLIUnknownSubcommandReturnsFalse(t *testing.T) {
	if RunCLI([]string{"nonexistent-cmd"}, "not-used.db") {
		t.Error("RunCLI(unknown) should return false")
	}
}

func TestRunCLIEmptyArgsReturnsFalse(t *testing.T) {
	if RunCLI([]string{}, "not-used.db") {
		t.Error("RunCLI([]) should return false")
	}
}

func TestRunCLINilArgsReturnsFalse(t *testing.T) {
	if RunCLI(nil, "not-used.db") {
		t.Error("RunCLI(nil) should return false")
	}
}

// ---------------------------------------------------------------------------
// "status" subcommand
// ---------------------------------------------------------------------------

func TestCLIStatusReturnsTrue(t *testing.T) {
	dbPath := cliDBSetup(t)
	if !RunCLI([]string{"status"}, dbPath) {
		t.Error("RunCLI(status) should return true")
	}
}

// ---------------------------------------------------------------------------
// "channels" subcommand
// ---------------------------------------------------------------------------

func TestCLIChannelsListReturnsTrue(t *testing.T) {
	dbPath := cliDBWithChannels(t, "General", "Gaming")
	if !RunCLI([]string{"channels"}, dbPath) {
		t.Error("RunCLI(channels) should return true")
	}
}

func TestCLIChannelsListExplicitReturnsTrue(t *testing.T) {
	dbPath := cliDBWithChannels(t, "General")
	if !RunCLI([]string{"channels", "list"}, dbPath) {
		t.Error("RunCLI(channels list) should return true")
	}
}

func TestCLIChannelsEmptyDBReturnsTrue(t *testing.T) {
	dbPath := cliDBSetup(t)
	if !RunCLI([]string{"channels"}, dbPath) {
		t.Error("RunCLI(channels) with empty db should return true")
	}
}

func TestCLIChannelsCreateReturnsTrue(t *testing.T) {
	dbPath := cliDBSetup(t)
	if !RunCLI([]string{"channels", "create", "TestChan"}, dbPath) {
		t.Error("RunCLI(channels create) should return true")
	}

	// Verify the channel was actually created.
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer st.Close()

	chs, err := st.GetChannels()
	if err != nil {
		t.Fatalf("GetChannels: %v", err)
	}
	found := false
	for _, ch := range chs {
		if ch.Name == "TestChan" {
			found = true
			break
		}
	}
	if !found {
		t.Error("channel 'TestChan' should exist after CLI create")
	}
}

// ---------------------------------------------------------------------------
// "settings" subcommand
// ---------------------------------------------------------------------------

func TestCLISettingsListReturnsTrue(t *testing.T) {
	dbPath := cliDBWithSettings(t, map[string]string{"server_name": "test"})
	if !RunCLI([]string{"settings"}, dbPath) {
		t.Error("RunCLI(settings) should return true")
	}
}

func TestCLISettingsListExplicitReturnsTrue(t *testing.T) {
	dbPath := cliDBSetup(t)
	if !RunCLI([]string{"settings", "list"}, dbPath) {
		t.Error("RunCLI(settings list) should return true")
	}
}

func TestCLISettingsSetReturnsTrue(t *testing.T) {
	dbPath := cliDBSetup(t)
	if !RunCLI([]string{"settings", "set", "mykey", "myvalue"}, dbPath) {
		t.Error("RunCLI(settings set) should return true")
	}

	// Verify the setting was persisted.
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer st.Close()

	val, ok, err := st.GetSetting("mykey")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if !ok {
		t.Fatal("expected setting to exist")
	}
	if val != "myvalue" {
		t.Errorf("setting value: got %q, want %q", val, "myvalue")
	}
}

// ---------------------------------------------------------------------------
// "backup" subcommand
// ---------------------------------------------------------------------------

func TestCLIBackupDefaultPath(t *testing.T) {
	dbPath := cliDBSetup(t)

	// We need to be in a temp dir so the default "bken-backup.db" doesn't
	// pollute the project directory.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer os.Chdir(origDir)

	if !RunCLI([]string{"backup"}, dbPath) {
		t.Error("RunCLI(backup) should return true")
	}

	// Default backup path is "bken-backup.db".
	backupPath := filepath.Join(tmpDir, "bken-backup.db")
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Error("backup file should exist at default path")
	}

	// Verify the backup is a valid SQLite database.
	backupStore, err := store.New(backupPath)
	if err != nil {
		t.Fatalf("opening backup: %v", err)
	}
	backupStore.Close()
}

func TestCLIBackupCustomPath(t *testing.T) {
	dbPath := cliDBWithSettings(t, map[string]string{"server_name": "backup-test"})
	outPath := filepath.Join(t.TempDir(), "custom-backup.db")

	if !RunCLI([]string{"backup", outPath}, dbPath) {
		t.Error("RunCLI(backup <path>) should return true")
	}

	if _, err := os.Stat(outPath); os.IsNotExist(err) {
		t.Error("backup file should exist at custom path")
	}

	// Verify data was preserved.
	backupStore, err := store.New(outPath)
	if err != nil {
		t.Fatalf("opening backup: %v", err)
	}
	defer backupStore.Close()

	val, ok, err := backupStore.GetSetting("server_name")
	if err != nil || !ok || val != "backup-test" {
		t.Errorf("backup should contain server_name=backup-test, got %q ok=%v err=%v", val, ok, err)
	}
}
