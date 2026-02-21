package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"client/internal/config"
)

func TestDefault(t *testing.T) {
	cfg := config.Default()
	if cfg.Theme != "dark" {
		t.Errorf("expected theme 'dark', got %q", cfg.Theme)
	}
	if cfg.Volume != 1.0 {
		t.Errorf("expected volume 1.0, got %v", cfg.Volume)
	}
	if cfg.InputDeviceID != -1 || cfg.OutputDeviceID != -1 {
		t.Error("expected device IDs to default to -1")
	}
	if len(cfg.Servers) == 0 {
		t.Error("expected at least one default server")
	}
	if !cfg.NoiseEnabled {
		t.Error("expected noise suppression enabled by default")
	}
	if !cfg.AGCEnabled {
		t.Error("expected AGC enabled by default")
	}
	if !cfg.AECEnabled {
		t.Error("expected echo cancellation enabled by default")
	}
	if cfg.PTTEnabled {
		t.Error("expected PTT disabled by default")
	}
	if cfg.PTTKey != "Backquote" {
		t.Errorf("expected default PTT key 'Backquote', got %q", cfg.PTTKey)
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := config.Config{
		Theme:          "dracula",
		Username:       "alice",
		InputDeviceID:  2,
		OutputDeviceID: 3,
		Volume:         0.75,
		AECEnabled:     true,
		NoiseEnabled:   true,
		AGCEnabled:     true,
		PTTEnabled:     true,
		PTTKey:         "Space",
		Servers: []config.ServerEntry{
			{Name: "Home", Addr: "192.168.1.10:8443"},
		},
	}

	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded := config.Load()
	if loaded.Theme != cfg.Theme {
		t.Errorf("theme: want %q got %q", cfg.Theme, loaded.Theme)
	}
	if loaded.Username != cfg.Username {
		t.Errorf("username: want %q got %q", cfg.Username, loaded.Username)
	}
	if loaded.InputDeviceID != cfg.InputDeviceID {
		t.Errorf("input device: want %d got %d", cfg.InputDeviceID, loaded.InputDeviceID)
	}
	if loaded.Volume != cfg.Volume {
		t.Errorf("volume: want %v got %v", cfg.Volume, loaded.Volume)
	}
	if loaded.AECEnabled != cfg.AECEnabled {
		t.Errorf("aec enabled: want %v got %v", cfg.AECEnabled, loaded.AECEnabled)
	}
	if loaded.NoiseEnabled != cfg.NoiseEnabled {
		t.Errorf("noise enabled: want %v got %v", cfg.NoiseEnabled, loaded.NoiseEnabled)
	}
	if loaded.AGCEnabled != cfg.AGCEnabled {
		t.Errorf("agc enabled: want %v got %v", cfg.AGCEnabled, loaded.AGCEnabled)
	}
	if loaded.PTTEnabled != cfg.PTTEnabled {
		t.Errorf("ptt enabled: want %v got %v", cfg.PTTEnabled, loaded.PTTEnabled)
	}
	if loaded.PTTKey != cfg.PTTKey {
		t.Errorf("ptt key: want %q got %q", cfg.PTTKey, loaded.PTTKey)
	}
	if len(loaded.Servers) != 1 || loaded.Servers[0].Addr != "192.168.1.10:8443" {
		t.Errorf("servers: unexpected value %+v", loaded.Servers)
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := config.Load()
	if cfg.Theme == "" {
		t.Error("expected non-empty theme from defaults")
	}
}

func TestLoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path := filepath.Join(dir, "bken", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json {{{"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Load()
	if cfg.Theme != "dark" {
		t.Errorf("expected default theme on corrupt file, got %q", cfg.Theme)
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	if err := config.Save(config.Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(dir, "bken", "config.json")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config file not created: %v", err)
	}
}
