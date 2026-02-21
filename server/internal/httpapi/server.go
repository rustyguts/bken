package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"bken/server/internal/core"
	"bken/server/internal/protocol"
	"bken/server/internal/ws"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Server is the Echo application.
type Server struct {
	echo *echo.Echo
	room *core.Room
}

// New constructs an Echo app with websocket + REST routes.
func New(room *core.Room) *Server {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.Recover())

	s := &Server{echo: e, room: room}
	s.registerRoutes()
	return s
}

// Echo exposes the underlying Echo instance for tests.
func (s *Server) Echo() *echo.Echo {
	return s.echo
}

func (s *Server) registerRoutes() {
	s.echo.GET("/health", s.handleHealth)
	s.echo.GET("/api/state", s.handleState)
	ws.NewHandler(s.room).Register(s.echo)
}

// Run starts Echo and blocks until ctx cancellation or startup failure.
func (s *Server) Run(ctx context.Context, addr string) error {
	errCh := make(chan error, 1)
	go func() {
		err := s.echo.Start(addr)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.echo.Shutdown(shutCtx)
		return nil
	}
}

type healthResponse struct {
	Status  string `json:"status"`
	Clients int    `json:"clients"`
}

func (s *Server) handleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, healthResponse{
		Status:  "ok",
		Clients: s.room.ClientCount(),
	})
}

type stateResponse struct {
	Clients int             `json:"clients"`
	Users   []protocol.User `json:"users"`
}

func (s *Server) handleState(c echo.Context) error {
	users := s.room.Users()
	if users == nil {
		users = []protocol.User{}
	}
	return c.JSON(http.StatusOK, stateResponse{
		Clients: len(users),
		Users:   users,
	})
}
