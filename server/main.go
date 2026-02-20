package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

	"bken/server/store"
)

func main() {
	addr    := flag.String("addr",     ":4433",    "WebTransport listen address")
	apiAddr := flag.String("api-addr", ":8080",    "REST API listen address (empty to disable)")
	dbPath  := flag.String("db",       "bken.db",  "SQLite database path")
	flag.Parse()

	// Open persistent store; seed defaults on first run.
	st, err := store.New(*dbPath)
	if err != nil {
		log.Fatalf("[store] %v", err)
	}
	defer st.Close()
	seedDefaults(st)

	tlsConfig, fingerprint := generateTLSConfig()
	log.Printf("[server] TLS certificate fingerprint: %s", fingerprint)

	room := NewRoom()

	// Seed room with persisted server name so connecting clients see it immediately.
	if name, ok, err := st.GetSetting("server_name"); err == nil && ok {
		room.SetServerName(name)
	}

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

	// Start REST API server if an address is configured.
	if *apiAddr != "" {
		api := NewAPIServer(room, st)
		go api.Run(ctx, *apiAddr)
		log.Printf("[api] listening on %s", *apiAddr)
	}

	srv := NewServer(*addr, tlsConfig, room)
	if err := srv.Run(ctx); err != nil {
		log.Fatalf("[server] %v", err)
	}
}

// seedDefaults writes factory-default settings when they have not been set yet.
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
}
