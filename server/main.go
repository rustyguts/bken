package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"bken/server/store"
)

func main() {
	// Check for CLI subcommands before parsing flags.
	if len(os.Args) > 1 {
		// Default DB path for CLI commands (overridable by the -db flag in serve mode).
		cliDB := "bken.db"
		if RunCLI(os.Args[1:], cliDB) {
			return
		}
	}

	addr := flag.String("addr", ":8443", "HTTPS/WebSocket listen address")
	apiAddr := flag.String("api-addr", ":8080", "REST API listen address (empty to disable)")
	dbPath := flag.String("db", "bken.db", "SQLite database path")
	idleTimeout := flag.Duration("idle-timeout", 30*time.Second, "HTTP idle timeout")
	certValidity := flag.Duration("cert-validity", 24*time.Hour, "self-signed TLS certificate validity")
	testUser := flag.String("test-user", "", "name for a virtual test bot that emits a 440 Hz tone (empty to disable)")
	maxConnections := flag.Int("max-connections", 500, "maximum total WebSocket connections")
	perIPLimit := flag.Int("per-ip-limit", 10, "maximum connections per IP address")
	rateLimit := flag.Int("rate-limit", 50, "maximum control messages per second per client")
	recDir := flag.String("recordings-dir", "recordings", "subdirectory name for voice recordings (relative to -db directory)")
	turnURL := flag.String("turn-url", "", "TURN server URL (e.g. turn:turn.example.com:3478)")
	turnUsername := flag.String("turn-username", "", "TURN server username")
	turnCredential := flag.String("turn-credential", "", "TURN server credential")
	flag.Parse()

	// Open persistent store; seed defaults on first run.
	st, err := store.New(*dbPath)
	if err != nil {
		log.Fatalf("[store] %v", err)
	}
	defer st.Close()
	seedDefaults(st)

	// Extract the hostname from the listen address for the TLS certificate.
	tlsHostname := ""
	if host, _, err := net.SplitHostPort(*addr); err == nil && host != "" {
		tlsHostname = host
	}

	tlsConfig, fingerprint, err := generateTLSConfig(*certValidity, tlsHostname)
	if err != nil {
		log.Fatalf("[server] %v", err)
	}
	log.Printf("[server] TLS certificate fingerprint: %s", fingerprint)

	room := NewRoom()
	room.SetDataDir(filepath.Dir(*dbPath))
	room.SetRecordingsDir(*recDir)

	// Configure ICE servers (STUN + optional TURN) for WebRTC peer connections.
	iceServers := []ICEServerInfo{
		{URLs: []string{"stun:stun.l.google.com:19302"}},
	}
	if *turnURL != "" {
		turnServer := ICEServerInfo{URLs: []string{*turnURL}}
		if *turnUsername != "" {
			turnServer.Username = *turnUsername
		}
		if *turnCredential != "" {
			turnServer.Credential = *turnCredential
		}
		iceServers = append(iceServers, turnServer)
		log.Printf("[server] TURN server configured: %s", *turnURL)
	}
	room.SetICEServers(iceServers)

	// Seed room with persisted server name so connecting clients see it immediately.
	if name, ok, err := st.GetSetting("server_name"); err == nil && ok {
		room.SetServerName(name)
	}

	// Persist server name to SQLite whenever a connected owner renames the room.
	room.SetOnRename(func(name string) error {
		return st.SetSetting("server_name", name)
	})

	// Wire channel CRUD callbacks to the store.
	room.SetOnCreateChannel(func(name string) (int64, error) {
		return st.CreateChannel(name)
	})
	room.SetOnRenameChannel(func(id int64, name string) error {
		return st.RenameChannel(id, name)
	})
	room.SetOnDeleteChannel(func(id int64) error {
		return st.DeleteChannel(id)
	})
	room.SetOnRefreshChannels(func() ([]ChannelInfo, error) {
		chs, err := st.GetChannels()
		if err != nil {
			return nil, err
		}
		return convertChannels(chs), nil
	})

	// Seed room's channel cache so newly-connecting clients receive the list.
	if chs, err := st.GetChannels(); err == nil {
		room.SetChannels(convertChannels(chs))
	}

	// Wire audit log and ban callbacks to the store.
	room.SetOnAuditLog(func(actorID int, actorName, action, target, details string) {
		if err := st.InsertAuditLog(actorID, actorName, action, target, details); err != nil {
			log.Printf("[audit] insert: %v", err)
		}
	})
	room.SetOnBan(func(pubkey, ip, reason, bannedBy string, durationS int) {
		if _, err := st.InsertBan(pubkey, ip, reason, bannedBy, durationS); err != nil {
			log.Printf("[ban] insert: %v", err)
		}
	})
	room.SetOnUnban(func(banID int64) {
		if err := st.DeleteBan(banID); err != nil {
			log.Printf("[ban] delete %d: %v", banID, err)
		}
	})

	// Connection limits.
	room.SetMaxConnections(*maxConnections)
	room.SetPerIPLimit(*perIPLimit)
	room.SetControlRateLimit(*rateLimit)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Graceful shutdown on interrupt.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		log.Println("[server] shutting down...")
		cancel()
	}()

	// Start metrics logging.
	go RunMetrics(ctx, room, 5*time.Second)

	// Periodically check mute expiry and purge expired bans.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				room.CheckMuteExpiry()
				if _, err := st.PurgeExpiredBans(); err != nil {
					log.Printf("[ban] purge expired: %v", err)
				}
			}
		}
	}()

	// Periodically optimize SQLite query planner.
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := st.Optimize(); err != nil {
					log.Printf("[store] optimize: %v", err)
				}
			}
		}
	}()

	// Start virtual test bot if configured.
	if *testUser != "" {
		go RunTestBot(ctx, room, *testUser)
	}

	// Start REST API server if an address is configured.
	if *apiAddr != "" {
		// Create uploads directory next to the database file.
		uploadsDir := filepath.Join(filepath.Dir(*dbPath), "uploads")
		if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
			log.Fatalf("[api] create uploads dir: %v", err)
		}

		// Tell the room which port the API lives on so it's included in the
		// user_list welcome message. Clients use this to construct file URLs.
		if _, port, err := net.SplitHostPort(*apiAddr); err == nil {
			if p, err := net.LookupPort("tcp", port); err == nil {
				room.SetAPIPort(p)
			}
		}

		api := NewAPIServer(room, st, uploadsDir)
		go api.Run(ctx, *apiAddr)
		log.Printf("[api] listening on %s", *apiAddr)
	}

	srv := NewServer(*addr, tlsConfig, room, *idleTimeout)
	if err := srv.Run(ctx); err != nil {
		log.Fatalf("[server] %v", err)
	}
}

// seedDefaults writes factory-default settings and channels when they have not
// been created yet (first-run initialisation).
func seedDefaults(st *store.Store) {
	defaults := [][2]string{
		{"server_name", "bken server"},
	}
	for _, kv := range defaults {
		if _, ok, err := st.GetSetting(kv[0]); err == nil && !ok {
			if err := st.SetSetting(kv[0], kv[1]); err != nil {
				log.Printf("[store] seed %q: %v", kv[0], err)
			}
		}
	}

	// Seed a default "General" channel if no channels exist yet.
	n, err := st.ChannelCount()
	if err != nil {
		log.Printf("[store] channel count: %v", err)
		return
	}
	if n == 0 {
		if _, err := st.CreateChannel("General"); err != nil {
			log.Printf("[store] seed General channel: %v", err)
		}
	}
}
