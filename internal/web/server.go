package web

import (
	"database/sql"

	"github.com/hibiken/asynq"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/rustyguts/bken/internal/config"
	"github.com/rustyguts/bken/internal/web/ui"
)

// Deps bundles the shared state every handler needs. Passed to New and
// stored on the server-level context so handlers can reach it cheaply.
type Deps struct {
	Cfg       *config.Config
	DB        *sql.DB
	Client    *asynq.Client
	Inspector *asynq.Inspector
}

// New builds the configured Echo server. Middleware is permissive so the
// dev Nuxt + Go instances can run on different ports without CORS pain.
func New(cfg *config.Config, conn *sql.DB, client *asynq.Client, inspector *asynq.Inspector) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.Gzip())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{
			echo.GET, echo.POST, echo.PATCH, echo.DELETE, echo.OPTIONS,
		},
	}))

	deps := &Deps{Cfg: cfg, DB: conn, Client: client, Inspector: inspector}

	api := e.Group("/api")
	api.GET("/clips", deps.listClips)
	api.GET("/games", deps.listGames)
	api.GET("/jobs", deps.listJobs)
	api.GET("/videos", deps.listVideos)
	api.GET("/videos/:id", deps.getVideo)
	api.GET("/videos/:id/moments", deps.listVideoMoments)
	api.POST("/videos/:id/reprocess", deps.reprocessVideo)
	api.POST("/moments", deps.createMoment)
	api.PATCH("/moments/:id", deps.patchMoment)
	api.DELETE("/moments/:id", deps.deleteMoment)
	api.POST("/moments/:id/clip", deps.createMomentClip)
	api.POST("/rate/:id", deps.rateClip)
	api.GET("/clip/:id/vision", deps.clipVision)
	api.GET("/search/object", deps.searchObject)
	api.GET("/search/ocr", deps.searchOCR)
	api.GET("/libraries", deps.listLibraries)
	api.POST("/libraries", deps.createLibrary)
	api.DELETE("/libraries/:id", deps.deleteLibrary)
	api.POST("/libraries/:id/sync", deps.syncLibrary)

	e.GET("/clip/:id", deps.serveClip)
	e.GET("/thumb/:id", deps.serveClipThumb)
	e.GET("/video_thumb/:id", deps.serveVideoThumb)
	e.GET("/stream/video/:id", deps.streamVideo)
	e.GET("/share/:id", deps.sharePage)

	(&ui.Deps{Cfg: cfg, DB: conn}).Register(e)

	return e
}
