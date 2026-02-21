package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bken/server/internal/blob"
	"bken/server/internal/core"
	"bken/server/internal/protocol"
	"bken/server/internal/store"
	"bken/server/internal/ws"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Server is the Echo application.
type Server struct {
	echo         *echo.Echo
	channelState *core.ChannelState
	blobs        *blob.Store
}

// New constructs an Echo app with websocket + REST routes.
func New(channelState *core.ChannelState, blobs ...*blob.Store) *Server {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.Recover())

	var blobStore *blob.Store
	if len(blobs) > 0 {
		blobStore = blobs[0]
	}

	s := &Server{echo: e, channelState: channelState, blobs: blobStore}
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
	if s.blobs != nil {
		s.echo.POST("/api/blobs", s.handleBlobUpload)
		s.echo.POST("/api/upload", s.handleBlobUpload) // Backward-compatible alias.
		s.echo.GET("/api/blobs/:id", s.handleBlobDownload)
		s.echo.GET("/api/files/:id", s.handleBlobDownload) // Backward-compatible alias.
	}
	ws.NewHandler(s.channelState).Register(s.echo)
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
		Clients: s.channelState.ClientCount(),
	})
}

type stateResponse struct {
	Clients int             `json:"clients"`
	Users   []protocol.User `json:"users"`
}

func (s *Server) handleState(c echo.Context) error {
	users := s.channelState.Users()
	if users == nil {
		users = []protocol.User{}
	}
	return c.JSON(http.StatusOK, stateResponse{
		Clients: len(users),
		Users:   users,
	})
}

type blobUploadResponse struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	OriginalName string `json:"original_name"`
	ContentType  string `json:"content_type"`
	SizeBytes    int64  `json:"size_bytes"`
	CreatedAt    string `json:"created_at"`
}

func (s *Server) handleBlobUpload(c echo.Context) error {
	if s.blobs == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "blob storage is not configured")
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "multipart file field \"file\" is required")
	}

	src, err := fileHeader.Open()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("open uploaded file: %v", err))
	}
	defer src.Close()

	contentType := strings.TrimSpace(fileHeader.Header.Get(echo.HeaderContentType))
	meta, err := s.blobs.Put(c.Request().Context(), blob.PutInput{
		Kind:         c.FormValue("kind"),
		OriginalName: fileHeader.Filename,
		ContentType:  contentType,
		Reader:       src,
	})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("persist blob: %v", err))
	}

	return c.JSON(http.StatusCreated, blobUploadResponse{
		ID:           meta.ID,
		Kind:         meta.Kind,
		OriginalName: meta.OriginalName,
		ContentType:  meta.ContentType,
		SizeBytes:    meta.SizeBytes,
		CreatedAt:    meta.CreatedAt.Format(time.RFC3339Nano),
	})
}

func (s *Server) handleBlobDownload(c echo.Context) error {
	if s.blobs == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "blob storage is not configured")
	}

	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "blob id is required")
	}

	result, err := s.blobs.Open(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrBlobNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "blob not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("open blob: %v", err))
	}
	defer result.File.Close()

	c.Response().Header().Set(echo.HeaderContentType, result.Metadata.ContentType)
	c.Response().Header().Set(echo.HeaderContentLength, strconv.FormatInt(result.Metadata.SizeBytes, 10))
	c.Response().Header().Set(
		echo.HeaderContentDisposition,
		fmt.Sprintf(`attachment; filename="%s"`, safeFilename(result.Metadata.OriginalName)),
	)
	c.Response().WriteHeader(http.StatusOK)
	_, copyErr := io.Copy(c.Response().Writer, result.File)
	return copyErr
}

func safeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "blob"
	}
	name = strings.ReplaceAll(name, `"`, "_")
	name = strings.ReplaceAll(name, "\\", "_")
	return name
}
