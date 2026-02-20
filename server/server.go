package main

import (
	"context"
	"crypto/tls"
	"log"
	"net/http"

	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
)

// Server holds the WebTransport server and room state.
type Server struct {
	addr      string
	tlsConfig *tls.Config
	room      *Room
	wt        *webtransport.Server
}

func NewServer(addr string, tlsConfig *tls.Config, room *Room) *Server {
	return &Server{
		addr:      addr,
		tlsConfig: tlsConfig,
		room:      room,
	}
}

// Run starts the WebTransport server and blocks until the context is canceled.
func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()

	s.wt = &webtransport.Server{
		H3: &http3.Server{
			Addr:      s.addr,
			TLSConfig: s.tlsConfig,
			Handler:   mux,
		},
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	webtransport.ConfigureHTTP3Server(s.wt.H3)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		sess, err := s.wt.Upgrade(w, r)
		if err != nil {
			log.Printf("[server] upgrade failed: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		handleClient(ctx, sess, s.room)
	})

	log.Printf("[server] listening on %s", s.addr)

	go func() {
		<-ctx.Done()
		s.wt.Close()
	}()

	return s.wt.ListenAndServe()
}
