package web

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/rustyguts/bken/internal/thumbs"
)

// Minimal grey JPEG — just enough header bytes so <img> doesn't render a
// broken icon when the real clip isn't available yet. Matches the Nuxt
// placeholder byte-for-byte.
var placeholderJPEG = func() []byte {
	b := []byte{0xff, 0xd8, 0xff, 0xdb, 0x00, 0x43, 0x00, 0x08}
	b = append(b, make([]byte, 60)...)
	return b
}()

// filenameSanitiser mirrors the [^A-Za-z0-9._-]+ regex in the Nuxt route.
var filenameSanitiser = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (d *Deps) serveClip(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parsePositive(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid clip id")
	}
	var (
		startS                   sql.NullFloat64
		clipPath, game, recorded sql.NullString
	)
	err = d.DB.QueryRowContext(ctx, `SELECT c.start_s, c.clip_path, v.game, v.recorded_at
		FROM candidate_clip c JOIN video v ON v.id = c.video_id
		WHERE c.id=?`, id).Scan(&startS, &clipPath, &game, &recorded)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "clip not found")
		}
		return err
	}
	if !clipPath.Valid || clipPath.String == "" {
		return echo.NewHTTPError(http.StatusNotFound, "clip not found")
	}
	path := d.Cfg.ResolveData(clipPath.String)

	f, err := os.Open(path)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "clip file missing")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}

	resp := c.Response()
	resp.Header().Set("Content-Type", "video/mp4")
	resp.Header().Set("Accept-Ranges", "bytes")
	if c.QueryParam("download") != "" {
		resp.Header().Set("Content-Disposition",
			fmt.Sprintf(`attachment; filename="%s"`, downloadFilename(id, startS, game, recorded)))
	}
	http.ServeContent(resp, c.Request(), st.Name(), st.ModTime(), f)
	return nil
}

func downloadFilename(id int64, startS sql.NullFloat64, game, recorded sql.NullString) string {
	g := "clip"
	if game.Valid {
		trimmed := strings.TrimSpace(game.String)
		if trimmed != "" {
			g = trimmed
		}
	}
	date := "undated"
	if recorded.Valid && len(recorded.String) >= 10 {
		date = recorded.String[:10]
	}
	start := 0
	if startS.Valid {
		start = int(startS.Float64)
	}
	mm := start / 60
	ss := start % 60
	name := fmt.Sprintf("%s_%s_%02d%02d_clip%d.mp4", g, date, mm, ss, id)
	return filenameSanitiser.ReplaceAllString(name, "_")
}

func (d *Deps) serveClipThumb(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parsePositive(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid clip id")
	}
	path, err := thumbs.ForClip(ctx, d.Cfg, d.DB, id)
	if err != nil || path == "" {
		c.Logger().Warnf("thumb: no file for clip_id=%d: %v", id, err)
		return servePlaceholderJPEG(c)
	}
	c.Response().Header().Set("Cache-Control", "public, max-age=3600")
	return serveFileAsContent(c, path, "image/jpeg")
}

func (d *Deps) serveVideoThumb(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parsePositive(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid video id")
	}
	path, err := thumbs.ForVideo(ctx, d.Cfg, d.DB, id)
	if err != nil || path == "" {
		return servePlaceholderJPEG(c)
	}
	c.Response().Header().Set("Cache-Control", "public, max-age=3600")
	return serveFileAsContent(c, path, "image/jpeg")
}

func (d *Deps) streamVideo(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parsePositive(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid video id")
	}
	var path sql.NullString
	if err := d.DB.QueryRowContext(ctx, "SELECT path FROM video WHERE id=?", id).Scan(&path); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "video not found")
		}
		return err
	}
	if !path.Valid || path.String == "" {
		return echo.NewHTTPError(http.StatusNotFound, "video not found")
	}
	c.Response().Header().Set("Accept-Ranges", "bytes")
	return serveFileAsContent(c, path.String, "video/mp4")
}

// serveFileAsContent opens `path` and hands it to http.ServeContent so
// Range + conditional headers are handled centrally. 404s if the file
// can't be opened.
func serveFileAsContent(c echo.Context, path, contentType string) error {
	f, err := os.Open(path)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "file missing")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	c.Response().Header().Set("Content-Type", contentType)
	http.ServeContent(c.Response(), c.Request(), st.Name(), st.ModTime(), f)
	return nil
}

func servePlaceholderJPEG(c echo.Context) error {
	c.Response().Header().Set("Content-Type", "image/jpeg")
	http.ServeContent(c.Response(), c.Request(), "placeholder.jpg", time.Time{}, bytes.NewReader(placeholderJPEG))
	return nil
}
