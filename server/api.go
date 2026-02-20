package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// APIServer provides HTTP REST endpoints for health checking and room state.
// It runs on a separate TCP port from the WebTransport/QUIC server.
type APIServer struct {
	room *Room
	echo *echo.Echo
}

// NewAPIServer constructs an APIServer and registers all routes.
func NewAPIServer(room *Room) *APIServer {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogMethod: true,
		LogURI:    true,
		LogStatus: true,
		LogValuesFunc: func(_ echo.Context, v middleware.RequestLoggerValues) error {
			log.Printf("[api] %s %s %d", v.Method, v.URI, v.Status)
			return nil
		},
	}))
	e.Use(middleware.Recover())

	s := &APIServer{room: room, echo: e}
	s.registerRoutes()
	return s
}

func (s *APIServer) registerRoutes() {
	s.echo.GET("/health", s.handleHealth)
	s.echo.GET("/api/room", s.handleRoom)
}

// Run starts the Echo HTTP server on addr and blocks until ctx is cancelled.
func (s *APIServer) Run(ctx context.Context, addr string) {
	go func() {
		if err := s.echo.Start(addr); err != nil && err != http.ErrServerClosed {
			log.Printf("[api] server error: %v", err)
		}
	}()
	<-ctx.Done()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.echo.Shutdown(shutCtx); err != nil {
		log.Printf("[api] shutdown: %v", err)
	}
}

// HealthResponse is the payload for GET /health.
type HealthResponse struct {
	Status  string `json:"status"`
	Clients int    `json:"clients"`
}

func (s *APIServer) handleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, HealthResponse{
		Status:  "ok",
		Clients: s.room.ClientCount(),
	})
}

// RoomResponse is the payload for GET /api/room.
type RoomResponse struct {
	Clients int        `json:"clients"`
	Users   []UserInfo `json:"users"`
}

func (s *APIServer) handleRoom(c echo.Context) error {
	users := s.room.Clients()
	if users == nil {
		users = []UserInfo{}
	}
	return c.JSON(http.StatusOK, RoomResponse{
		Clients: len(users),
		Users:   users,
	})
}
