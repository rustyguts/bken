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
	addr          := flag.String("addr",          ":4433",   "WebTransport listen address")
	apiAddr       := flag.String("api-addr",      ":8080",   "REST API listen address (empty to disable)")
	dbPath        := flag.String("db",            "bken.db", "SQLite database path")
	idleTimeout   := flag.Duration("idle-timeout", 30*time.Second, "QUIC connection idle timeout")
	certValidity  := flag.Duration("cert-validity", 24*time.Hour,  "self-signed TLS certificate validity")
	testUser      := flag.String("test-user",     "",        "name for a virtual test bot that emits a 440 Hz tone (empty to disable)")
	flag.Parse()

	// Open persistent store; seed defaults on first run.
	st, err := store.New(*dbPath)
	if err != nil {
		log.Fatalf("[store] %v", err)
	}
	defer st.Close()
	seedDefaults(st)

	tlsConfig, fingerprint := generateTLSConfig(*certValidity)
	log.Printf("[server] TLS certificate fingerprint: %s", fingerprint)

	room := NewRoom()

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

	// Start virtual test bot if configured.
	if *testUser != "" {
		go RunTestBot(ctx, room, *testUser)
	}

	// Start REST API server if an address is configured.
	if *apiAddr != "" {
		api := NewAPIServer(room, st)
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
