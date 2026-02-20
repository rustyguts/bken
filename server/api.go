package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"bken/server/store"
)

// APIServer provides HTTP REST endpoints for health checking and room state.
// It runs on a separate TCP port from the WebTransport/QUIC server.
type APIServer struct {
	room  *Room
	store *store.Store
	echo  *echo.Echo
}

// NewAPIServer constructs an APIServer and registers all routes.
func NewAPIServer(room *Room, st *store.Store) *APIServer {
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

	s := &APIServer{room: room, store: st, echo: e}
	s.registerRoutes()
	return s
}

func (s *APIServer) registerRoutes() {
	s.echo.GET("/health", s.handleHealth)
	s.echo.GET("/api/room", s.handleRoom)
	s.echo.GET("/api/settings", s.handleGetSettings)
	s.echo.PUT("/api/settings", s.handlePutSettings)
	s.echo.GET("/api/channels", s.handleGetChannels)
	s.echo.POST("/api/channels", s.handleCreateChannel)
	s.echo.PUT("/api/channels/:id", s.handleRenameChannel)
	s.echo.DELETE("/api/channels/:id", s.handleDeleteChannel)
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

// SettingsResponse is the payload for GET /api/settings.
type SettingsResponse struct {
	ServerName string `json:"server_name"`
}

// SettingsRequest is the body for PUT /api/settings.
type SettingsRequest struct {
	ServerName string `json:"server_name"`
}

func (s *APIServer) handleGetSettings(c echo.Context) error {
	name, _, err := s.store.GetSetting("server_name")
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, SettingsResponse{ServerName: name})
}

func (s *APIServer) handlePutSettings(c echo.Context) error {
	var req SettingsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	name := strings.TrimSpace(req.ServerName)
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "server_name must not be empty")
	}
	if len(name) > 50 {
		return echo.NewHTTPError(http.StatusBadRequest, "server_name must not exceed 50 characters")
	}
	if err := s.store.SetSetting("server_name", name); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	// Update live room state so new connections see the new name without a restart,
	// and push a server_info message to all currently-connected clients.
	s.room.SetServerName(name)
	s.room.BroadcastControl(ControlMsg{Type: "server_info", ServerName: name}, 0)
	return c.NoContent(http.StatusNoContent)
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
	OwnerID uint16     `json:"owner_id"` // 0 when the room is empty
}

func (s *APIServer) handleRoom(c echo.Context) error {
	users := s.room.Clients()
	if users == nil {
		users = []UserInfo{}
	}
	return c.JSON(http.StatusOK, RoomResponse{
		Clients: len(users),
		Users:   users,
		OwnerID: s.room.OwnerID(),
	})
}

// ChannelResponse is an element in the GET /api/channels array.
type ChannelResponse struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

// ChannelRequest is the body for POST and PUT /api/channels.
type ChannelRequest struct {
	Name string `json:"name"`
}

func (s *APIServer) handleGetChannels(c echo.Context) error {
	channels, err := s.store.GetChannels()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	resp := make([]ChannelResponse, 0, len(channels))
	for _, ch := range channels {
		resp = append(resp, ChannelResponse{ID: ch.ID, Name: ch.Name, Position: ch.Position})
	}
	return c.JSON(http.StatusOK, resp)
}

func (s *APIServer) handleCreateChannel(c echo.Context) error {
	var req ChannelRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name must not be empty")
	}
	if len(name) > 50 {
		return echo.NewHTTPError(http.StatusBadRequest, "name must not exceed 50 characters")
	}
	id, err := s.store.CreateChannel(name)
	if err != nil {
		return echo.NewHTTPError(http.StatusConflict, "channel name already exists")
	}
	return c.JSON(http.StatusCreated, ChannelResponse{ID: id, Name: name})
}

func (s *APIServer) handleRenameChannel(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid channel id")
	}
	var req ChannelRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name must not be empty")
	}
	if len(name) > 50 {
		return echo.NewHTTPError(http.StatusBadRequest, "name must not exceed 50 characters")
	}
	if err := s.store.RenameChannel(id, name); err != nil {
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusNotFound, "channel not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}

func (s *APIServer) handleDeleteChannel(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid channel id")
	}
	if err := s.store.DeleteChannel(id); err != nil {
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusNotFound, "channel not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}
