package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// shareTemplate is a line-for-line port of app/server/routes/share/[id].get.ts.
// Using html/template handles the required escaping so the inline data can't
// break out of attributes or script contexts.
var shareTemplate = template.Must(template.New("share").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.Title}} — bken</title>
  <meta name="description" content="{{.DescriptionShort}}">

  <meta property="og:type" content="video.other">
  <meta property="og:url" content="{{.ShareURL}}">
  <meta property="og:title" content="{{.Title}}">
  <meta property="og:description" content="{{.DescriptionLong}}">
  <meta property="og:site_name" content="bken">
  <meta property="og:image" content="{{.ThumbURL}}">
  <meta property="og:image:width" content="{{.Width}}">
  <meta property="og:image:height" content="{{.Height}}">
  <meta property="og:video" content="{{.VideoURL}}">
  <meta property="og:video:url" content="{{.VideoURL}}">
  <meta property="og:video:secure_url" content="{{.VideoURL}}">
  <meta property="og:video:type" content="video/mp4">
  <meta property="og:video:width" content="{{.Width}}">
  <meta property="og:video:height" content="{{.Height}}">

  <meta name="twitter:card" content="player">
  <meta name="twitter:title" content="{{.Title}}">
  <meta name="twitter:description" content="{{.DescriptionShort}}">
  <meta name="twitter:image" content="{{.ThumbURL}}">
  <meta name="twitter:player" content="{{.ShareURL}}">
  <meta name="twitter:player:width" content="{{.Width}}">
  <meta name="twitter:player:height" content="{{.Height}}">
  <meta name="twitter:player:stream" content="{{.VideoURL}}">
  <meta name="twitter:player:stream:content_type" content="video/mp4">

  <style>
    :root { color-scheme: dark; }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      background: #0b0c10; color: #c5c6c7;
      font-family: system-ui, -apple-system, Segoe UI, Roboto, Ubuntu, Cantarell, Noto Sans, Helvetica, Arial;
      display: flex; flex-direction: column; align-items: center;
      min-height: 100vh; padding: 24px 16px;
    }
    .brand { font-weight: 700; letter-spacing: .08em; text-transform: uppercase;
      font-size: 12px; color: #66fcf1; margin-bottom: 18px; text-decoration: none; }
    .card { width: 100%; max-width: 960px; background: #1f2833;
      border-radius: 14px; overflow: hidden; box-shadow: 0 20px 60px rgba(0,0,0,.45); }
    video { width: 100%; height: auto; display: block; background: #000; outline: none; }
    .info { padding: 18px 20px 20px; }
    h1 { font-size: 20px; line-height: 1.25; color: #fff; margin-bottom: 8px; }
    .meta { font-size: 13px; color: #8892b0; margin-bottom: 12px; }
    .meta span + span::before { content: "·"; margin: 0 8px; opacity: .6; }
    .desc { font-size: 14px; line-height: 1.55; color: #c5c6c7; margin-bottom: 14px; }
    .actions { display: flex; gap: 10px; flex-wrap: wrap; }
    .btn { display: inline-flex; align-items: center; gap: 6px;
      padding: 8px 14px; border-radius: 8px; font-size: 13px; font-weight: 600;
      text-decoration: none; border: 1px solid transparent; cursor: pointer;
      transition: transform .05s ease, opacity .15s ease; }
    .btn:active { transform: scale(.98); }
    .btn-primary { background: #66fcf1; color: #0b0c10; border-color: #66fcf1; }
    .btn-secondary { background: transparent; color: #66fcf1; border-color: #45a29e; }
    .btn-secondary:hover { background: rgba(102,252,241,.08); }
    .tagbar { margin-top: 14px; display: flex; flex-wrap: wrap; gap: 6px; }
    .tag { font-size: 11px; font-weight: 600; color: #45a29e;
      background: rgba(69,162,158,.12); padding: 4px 8px; border-radius: 999px; }
    footer { margin-top: 24px; font-size: 12px; color: #8892b0; text-align: center; }
    footer a { color: #66fcf1; text-decoration: none; }
  </style>
</head>
<body>
  <a class="brand" href="/">bken</a>
  <div class="card">
    <video controls playsinline preload="metadata" poster="{{.ThumbURL}}"
           width="{{.Width}}" height="{{.Height}}">
      <source src="{{.VideoURL}}" type="video/mp4">
      Your browser does not support the video tag.
    </video>
    <div class="info">
      <h1>{{.Title}}</h1>
      <div class="meta">
        <span>{{.Game}}</span>
        <span>{{.DateShort}}</span>
        <span>{{.DurationLabel}}</span>
      </div>
      {{if .LLMDesc}}<p class="desc">{{.LLMDesc}}</p>{{end}}
      {{if .LLMContents}}<p class="desc">{{.LLMContents}}</p>{{end}}
      <div class="actions">
        <a class="btn btn-primary" href="{{.DownloadURL}}" download>Download</a>
        <button class="btn btn-secondary" onclick="copyLink()">Copy link</button>
      </div>
      {{if .Tags}}
      <div class="tagbar">
        {{range .Tags}}<span class="tag">#{{.}}</span>{{end}}
      </div>
      {{end}}
    </div>
  </div>
  <footer>Shared with <a href="/">bken</a> — clip archive</footer>
  <script>
    function copyLink() {
      navigator.clipboard.writeText(location.href).then(() => {
        const btn = document.querySelector('button');
        const old = btn.textContent;
        btn.textContent = 'Copied!';
        setTimeout(() => btn.textContent = old, 1200);
      });
    }
  </script>
</body>
</html>`))

type shareData struct {
	Title            string
	DescriptionShort string
	DescriptionLong  string
	ShareURL         string
	VideoURL         string
	ThumbURL         string
	DownloadURL      string
	Width            int64
	Height           int64
	Game             string
	DateShort        string
	DurationLabel    string
	LLMDesc          string
	LLMContents      string
	Tags             []string
}

func (d *Deps) sharePage(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parsePositive(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid clip id")
	}
	var (
		startS, endS            float64
		clipPath                sql.NullString
		llmTitle, llmDesc       sql.NullString
		llmContents, llmTags    sql.NullString
		game, recorded          sql.NullString
		width, height           sql.NullInt64
	)
	err = d.DB.QueryRowContext(ctx, `SELECT c.start_s, c.end_s, c.clip_path,
		c.llm_title, c.llm_desc, c.llm_contents, c.llm_tags,
		v.game, v.recorded_at, v.width, v.height
		FROM candidate_clip c JOIN video v ON v.id = c.video_id
		WHERE c.id = ?`, id).Scan(&startS, &endS, &clipPath,
		&llmTitle, &llmDesc, &llmContents, &llmTags,
		&game, &recorded, &width, &height)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "clip not found")
		}
		return err
	}
	if !clipPath.Valid || clipPath.String == "" {
		return echo.NewHTTPError(http.StatusNotFound, "clip not found")
	}

	origin := d.Cfg.BaseURL
	if origin == "" {
		req := c.Request()
		scheme := c.Scheme()
		if fp := req.Header.Get("X-Forwarded-Proto"); fp != "" {
			scheme = fp
		}
		host := req.Host
		if fh := req.Header.Get("X-Forwarded-Host"); fh != "" {
			host = fh
		}
		origin = scheme + "://" + host
	}
	origin = strings.TrimRight(origin, "/")

	gameS := game.String
	if gameS == "" {
		gameS = "Unknown"
	}
	title := llmTitle.String
	if title == "" {
		title = "Clip from " + gameS
	}
	desc := llmDesc.String
	if desc == "" {
		desc = llmContents.String
	}
	if desc == "" {
		dateFallback := "undated"
		if recorded.Valid && len(recorded.String) >= 10 {
			dateFallback = recorded.String[:10]
		}
		desc = gameS + " — " + dateFallback
	}

	date := "undated"
	if recorded.Valid && len(recorded.String) >= 10 {
		date = recorded.String[:10]
	}
	dur := endS - startS
	if dur < 0 {
		dur = 0
	}
	durM := int(dur) / 60
	durS := int(dur) % 60

	var tags []string
	if llmTags.Valid && llmTags.String != "" {
		_ = json.Unmarshal([]byte(llmTags.String), &tags)
	}

	w := int64(1920)
	h := int64(1080)
	if width.Valid && width.Int64 > 0 {
		w = width.Int64
	}
	if height.Valid && height.Int64 > 0 {
		h = height.Int64
	}

	data := shareData{
		Title:            title,
		DescriptionShort: truncateRunes(desc, 200),
		DescriptionLong:  truncateRunes(desc, 300),
		ShareURL:         origin + "/share/" + itoa(id),
		VideoURL:         origin + "/clip/" + itoa(id) + ".mp4",
		ThumbURL:         origin + "/thumb/" + itoa(id) + ".jpg",
		DownloadURL:      origin + "/clip/" + itoa(id) + ".mp4?download=1",
		Width:            w, Height: h,
		Game:          gameS,
		DateShort:     date,
		DurationLabel: formatDuration(durM, durS),
		LLMDesc:       llmDesc.String,
		LLMContents:   llmContents.String,
		Tags:          tags,
	}

	c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Response().WriteHeader(http.StatusOK)
	return shareTemplate.Execute(c.Response(), data)
}

func truncateRunes(s string, max int) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func formatDuration(mins, secs int) string {
	// `M:SS` to match Nuxt's `${min}:${String(sec).padStart(2,'0')}`.
	b := strings.Builder{}
	b.WriteString(itoa(int64(mins)))
	b.WriteString(":")
	if secs < 10 {
		b.WriteString("0")
	}
	b.WriteString(itoa(int64(secs)))
	return b.String()
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
