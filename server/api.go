package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"bken/server/store"
)

// APIServer provides HTTP REST endpoints for health checking and room state.
// It runs on a separate TCP port from the websocket signaling server.
type APIServer struct {
	room       *Room
	store      *store.Store
	echo       *echo.Echo
	uploadsDir string // directory where uploaded files are stored
}

// NewAPIServer constructs an APIServer and registers all routes.
// uploadsDir is the directory where uploaded files are stored on disk.
func NewAPIServer(room *Room, st *store.Store, uploadsDir string) *APIServer {
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
	e.HTTPErrorHandler = jsonErrorHandler

	s := &APIServer{room: room, store: st, echo: e, uploadsDir: uploadsDir}
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
	s.echo.GET("/invite", s.handleInvite)
	s.echo.POST("/api/upload", s.handleUpload)
	s.echo.GET("/api/files/:id", s.handleGetFile)
	// Phase 8: Server Administration
	s.echo.GET("/api/audit", s.handleGetAuditLog)
	s.echo.GET("/api/bans", s.handleGetBans)
	s.echo.DELETE("/api/bans/:id", s.handleDeleteBan)
	// Phase 7: Recordings
	s.echo.GET("/api/recordings", s.handleListRecordings)
	s.echo.GET("/api/recordings/:filename", s.handleDownloadRecording)
	// Phase 10: Performance metrics
	s.echo.GET("/api/metrics", s.handleMetrics)
	// Phase 11: Version endpoint
	s.echo.GET("/api/version", s.handleVersion)
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
	name, err := validateName(req.ServerName, MaxNameLength)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
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

// Version is the current server version. Set at build time via -ldflags.
var Version = "0.1.0-dev"

// VersionResponse is the payload for GET /api/version.
type VersionResponse struct {
	Version string `json:"version"`
}

func (s *APIServer) handleVersion(c echo.Context) error {
	return c.JSON(http.StatusOK, VersionResponse{Version: Version})
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
	name, err := validateName(req.Name, MaxNameLength)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	id, err := s.store.CreateChannel(name)
	if err != nil {
		return echo.NewHTTPError(http.StatusConflict, "channel name already exists")
	}
	s.refreshChannels()
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
	name, err := validateName(req.Name, MaxNameLength)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := s.store.RenameChannel(id, name); err != nil {
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusNotFound, "channel not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	s.refreshChannels()
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
	s.refreshChannels()
	return c.NoContent(http.StatusNoContent)
}

// handleInvite serves a browser-friendly invite page for the server.
// The optional ?addr=host:port query parameter is the signaling server address;
// when provided the page includes a clickable bken:// deep-link.
func (s *APIServer) handleInvite(c echo.Context) error {
	name := s.room.ServerName()
	if name == "" {
		name = "bken server"
	}
	addr := c.QueryParam("addr")

	var linkHTML string
	if addr != "" {
		bkenURL := "bken://" + addr
		linkHTML = fmt.Sprintf(
			`<a href="%s" class="btn">Open in bken</a><div class="addr">%s</div>`,
			bkenURL, addr,
		)
	} else {
		linkHTML = `<p class="hint">Ask the server owner for the invite link.</p>`
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Join %s – bken</title>
  <style>
    *{box-sizing:border-box}
    body{font-family:system-ui,sans-serif;background:#1a1a2e;color:#e2e8f0;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}
    .card{background:#16213e;border-radius:12px;padding:2rem;max-width:400px;width:90%%;box-shadow:0 8px 32px rgba(0,0,0,.5)}
    h1{margin:0 0 .2rem;font-size:1rem;opacity:.45;letter-spacing:.18em;font-weight:700;text-transform:uppercase}
    h2{margin:.2rem 0 .75rem;font-size:1.6rem;font-weight:700}
    p{margin:0 0 1rem;opacity:.65;font-size:.9rem;line-height:1.5}
    .btn{display:inline-block;padding:.7rem 1.4rem;background:#7c3aed;color:#fff;border-radius:8px;text-decoration:none;font-weight:600;font-size:.95rem}
    .btn:hover{background:#6d28d9}
    .addr{display:inline-block;font-family:monospace;background:#0f3460;padding:.35rem .7rem;border-radius:6px;font-size:.85rem;margin-top:.75rem;color:#93c5fd}
    .hint{font-size:.85rem;opacity:.55}
    .note{margin-top:1.5rem;font-size:.75rem;opacity:.4}
    .note a{color:#a78bfa}
  </style>
</head>
<body>
  <div class="card">
    <h1>bken</h1>
    <h2>%s</h2>
    <p>You have been invited to join this voice server.</p>
    %s
    <div class="note">Don't have bken? Get it at <a href="https://github.com/rustyguts/bken">github.com/rustyguts/bken</a>.</div>
  </div>
</body>
</html>`, name, name, linkHTML)

	return c.HTML(http.StatusOK, html)
}

// UploadResponse is the JSON payload returned by POST /api/upload.
type UploadResponse struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

func (s *APIServer) handleUpload(c echo.Context) error {
	// Limit request body to MaxFileSize + 1 KB (for form overhead).
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, MaxFileSize+1024)

	file, header, err := c.Request().FormFile("file")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "missing or invalid file field")
	}
	defer file.Close()

	if header.Size > MaxFileSize {
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge,
			fmt.Sprintf("file exceeds %d MB limit", MaxFileSize/(1024*1024)))
	}

	// Determine content type from the file header.
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Generate a unique filename to avoid collisions.
	ext := filepath.Ext(header.Filename)
	diskName := uuid.New().String() + ext
	diskPath := filepath.Join(s.uploadsDir, diskName)

	dst, err := os.Create(diskPath)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create file")
	}
	defer dst.Close()

	written, err := io.Copy(dst, file)
	if err != nil {
		os.Remove(diskPath)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to write file")
	}

	id, err := s.store.CreateFile(header.Filename, contentType, diskPath, written)
	if err != nil {
		os.Remove(diskPath)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to record file")
	}

	return c.JSON(http.StatusCreated, UploadResponse{
		ID:          id,
		Name:        header.Filename,
		Size:        written,
		ContentType: contentType,
	})
}

func (s *APIServer) handleGetFile(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid file id")
	}

	f, err := s.store.GetFile(id)
	if err != nil {
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusNotFound, "file not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	c.Response().Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s"`, f.Name))
	return c.File(f.DiskPath)
}

// convertChannels maps store channel records to the wire-protocol ChannelInfo slice.
func convertChannels(chs []store.Channel) []ChannelInfo {
	infos := make([]ChannelInfo, 0, len(chs))
	for _, ch := range chs {
		infos = append(infos, ChannelInfo{ID: ch.ID, Name: ch.Name})
	}
	return infos
}

// refreshChannels reloads the channel list from the store, updates the room
// cache, and broadcasts a channel_list message to all connected clients.
func (s *APIServer) refreshChannels() {
	chs, err := s.store.GetChannels()
	if err != nil {
		log.Printf("[api] reload channels: %v", err)
		return
	}
	s.room.SetChannels(convertChannels(chs))
}

// --- Phase 8: Audit Log API ---

func (s *APIServer) handleGetAuditLog(c echo.Context) error {
	action := c.QueryParam("action")
	limit := 100
	if l := c.QueryParam("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	entries, err := s.store.GetAuditLog(action, limit)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	if entries == nil {
		entries = []store.AuditEntry{}
	}
	return c.JSON(http.StatusOK, entries)
}

// --- Phase 8: Ban Management API ---

func (s *APIServer) handleGetBans(c echo.Context) error {
	bans, err := s.store.GetBans()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	if bans == nil {
		bans = []store.Ban{}
	}
	return c.JSON(http.StatusOK, bans)
}

func (s *APIServer) handleDeleteBan(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid ban id")
	}
	if err := s.store.DeleteBan(id); err != nil {
		if err == sql.ErrNoRows {
			return echo.NewHTTPError(http.StatusNotFound, "ban not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}

// --- Phase 7: Recordings API ---

func (s *APIServer) handleListRecordings(c echo.Context) error {
	recordings := s.room.ListRecordings()
	if recordings == nil {
		recordings = []RecordingInfo{}
	}
	return c.JSON(http.StatusOK, recordings)
}

func (s *APIServer) handleDownloadRecording(c echo.Context) error {
	filename := c.Param("filename")
	if filename == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "missing filename")
	}
	// Sanitize filename to prevent path traversal.
	filename = filepath.Base(filename)
	path := s.room.GetRecordingFilePath(filename)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return echo.NewHTTPError(http.StatusNotFound, "recording not found")
	}
	c.Response().Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s"`, filename))
	return c.File(path)
}

// --- Phase 10: Metrics endpoint ---

// MetricsResponse includes runtime metrics for health monitoring.
type MetricsResponse struct {
	Status     string `json:"status"`
	Clients    int    `json:"clients"`
	Channels   int    `json:"channels"`
	Goroutines int    `json:"goroutines"`
}

func (s *APIServer) handleMetrics(c echo.Context) error {
	return c.JSON(http.StatusOK, MetricsResponse{
		Status:   "ok",
		Clients:  s.room.ClientCount(),
		Channels: s.room.ChannelCount(),
	})
}


// jsonErrorHandler ensures all error responses have a consistent JSON body:
//
//	{"error": "message"}
//
// This replaces Echo's default handler which varies between text and JSON.
func jsonErrorHandler(err error, c echo.Context) {
	code := http.StatusInternalServerError
	msg := err.Error()
	if he, ok := err.(*echo.HTTPError); ok {
		code = he.Code
		if m, ok := he.Message.(string); ok {
			msg = m
		}
	}
	if !c.Response().Committed {
		if c.Request().Method == http.MethodHead {
			c.NoContent(code) //nolint:errcheck
		} else {
			c.JSON(code, map[string]string{"error": msg}) //nolint:errcheck
		}
	}
}
