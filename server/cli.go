package main

import (
	"encoding/json"
	"fmt"
	"os"

	"bken/server/store"
)

// RunCLI handles subcommand execution. Returns true if a subcommand was handled.
func RunCLI(args []string, dbPath string) bool {
	if len(args) == 0 {
		return false
	}

	subcmd := args[0]
	switch subcmd {
	case "version":
		fmt.Printf("bken server %s\n", Version)
		return true
	case "status":
		return cliStatus(dbPath)
	case "channels":
		return cliChannels(args[1:], dbPath)
	case "settings":
		return cliSettings(args[1:], dbPath)
	case "backup":
		return cliBackup(args[1:], dbPath)
	default:
		return false
	}
}

func cliStatus(dbPath string) bool {
	st, err := store.New(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	name, _, _ := st.GetSetting("server_name")
	n, _ := st.ChannelCount()
	fmt.Printf("Server: %s\n", name)
	fmt.Printf("Database: %s\n", dbPath)
	fmt.Printf("Channels: %d\n", n)
	fmt.Printf("Version: %s\n", Version)
	return true
}

func cliChannels(args []string, dbPath string) bool {
	st, err := store.New(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	if len(args) == 0 || args[0] == "list" {
		chs, err := st.GetChannels()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if len(chs) == 0 {
			fmt.Println("No channels found.")
			return true
		}
		for _, ch := range chs {
			fmt.Printf("  [%d] %s\n", ch.ID, ch.Name)
		}
		return true
	}

	if args[0] == "create" && len(args) > 1 {
		name := args[1]
		id, err := st.CreateChannel(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating channel: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Created channel %q (id=%d)\n", name, id)
		return true
	}

	fmt.Fprintf(os.Stderr, "Usage: server channels [list|create <name>]\n")
	os.Exit(1)
	return true
}

func cliSettings(args []string, dbPath string) bool {
	st, err := store.New(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	if len(args) == 0 || args[0] == "list" {
		settings, err := st.GetAllSettings()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		out, _ := json.MarshalIndent(settings, "", "  ")
		fmt.Println(string(out))
		return true
	}

	if args[0] == "set" && len(args) > 2 {
		key, value := args[1], args[2]
		if err := st.SetSetting(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Set %s = %s\n", key, value)
		return true
	}

	fmt.Fprintf(os.Stderr, "Usage: server settings [list|set <key> <value>]\n")
	os.Exit(1)
	return true
}

func cliBackup(args []string, dbPath string) bool {
	st, err := store.New(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	outPath := "bken-backup.db"
	if len(args) > 0 {
		outPath = args[0]
	}

	if err := st.Backup(outPath); err != nil {
		fmt.Fprintf(os.Stderr, "backup failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Database backed up to %s\n", outPath)
	return true
}
