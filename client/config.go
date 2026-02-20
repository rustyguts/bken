package main

import "client/internal/config"

// Re-export types and functions from the config sub-package so they are
// available as Wails-bound method return/parameter types in the main package.

// Config holds all persistent user preferences.
type Config = config.Config

// ServerEntry is a saved server shown in the server browser.
type ServerEntry = config.ServerEntry

// LoadConfig loads the config from disk, returning defaults on any error.
func LoadConfig() Config { return config.Load() }

// SaveConfig persists cfg to disk.
func SaveConfig(cfg Config) error { return config.Save(cfg) }
