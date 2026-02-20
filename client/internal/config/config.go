// Package config manages persistent user preferences for the bken client.
// Settings are stored as JSON at os.UserConfigDir()/bken/config.json.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds all persistent user preferences.
type Config struct {
	Theme          string        `json:"theme"`
	Username       string        `json:"username"`
	InputDeviceID  int           `json:"input_device_id"`
	OutputDeviceID int           `json:"output_device_id"`
	Volume         float64       `json:"volume"`
	NoiseEnabled   bool          `json:"noise_enabled"`
	NoiseLevel     int           `json:"noise_level"`
	Servers        []ServerEntry `json:"servers"`
}

// ServerEntry is a saved server shown in the server browser.
type ServerEntry struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

// Default returns a Config populated with sensible defaults.
func Default() Config {
	return Config{
		Theme:          "dark",
		Volume:         1.0,
		NoiseLevel:     80,
		InputDeviceID:  -1,
		OutputDeviceID: -1,
		Servers: []ServerEntry{
			{Name: "Local Dev", Addr: "localhost:4433"},
		},
	}
}

// Path returns the absolute path to the config file.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bken", "config.json"), nil
}

// Load reads the config file and returns it. If the file is missing or
// unreadable, the default config is returned — never an error.
func Load() Config {
	path, err := Path()
	if err != nil {
		return Default()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Default()
	}
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default()
	}
	return cfg
}

// Save writes cfg to disk, creating the directory if needed.
func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
