package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// Server holds the signaling/control server and room state.
type Server struct {
	addr        string
	tlsConfig   *tls.Config
	room        *Room
	idleTimeout time.Duration
}

func NewServer(addr string, tlsConfig *tls.Config, room *Room, idleTimeout time.Duration) *Server {
	return &Server{
		addr:        addr,
		tlsConfig:   tlsConfig,
		room:        room,
		idleTimeout: idleTimeout,
	}
}

// Run starts the HTTPS + WebSocket server and blocks until the context is canceled.
func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[server] websocket upgrade failed: %v", err)
			return
		}
		go handleWebSocketClient(ctx, conn, s.room)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("bken signaling server"))
	})

	httpSrv := &http.Server{
		Addr:              s.addr,
		Handler:           mux,
		TLSConfig:         s.tlsConfig,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       s.idleTimeout,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			log.Printf("[server] shutdown: %v", err)
		}
	}()

	log.Printf("[server] listening on %s", s.addr)

	err := httpSrv.ListenAndServeTLS("", "")
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
