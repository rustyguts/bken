// Package ui hosts the Datastar-driven HTML UI (templates + static assets)
// that replaces the Nuxt frontend. Templates are parsed once at startup;
// static assets (built Tailwind CSS, fonts) are embedded so the binary is
// self-contained.
package ui

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"strings"
	"sync"
)

//go:embed templates
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// TemplateFS exposes the embedded templates/ tree (useful for tooling).
var TemplateFS = templateFS

// StaticFS exposes the embedded static/ tree so the echo handler can
// serve it via http.FS. The `static/` prefix is stripped so URLs like
// /static/dist/app.css resolve to static/dist/app.css inside the FS.
var StaticFS fs.FS

var (
	tmplOnce sync.Once
	tmpls    map[string]*template.Template
	tmplErr  error
)

func init() {
	// sub-FS so callers can serve the files at `/static/*` without
	// repeating the folder name.
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	StaticFS = sub
}

// funcMap exposes a handful of helpers templates need. Kept small on
// purpose: anything more complex belongs in the handler.
var funcMap = template.FuncMap{
	"fmtDuration": formatDurationSeconds,
	"fmtTime":     formatTimestamp,
	"fmtTimeLong": formatTimestampLong,
	"pct":         percent,
	"add":         func(a, b int) int { return a + b },
	"sub":         func(a, b int) int { return a - b },
	"default":     defaultValue,
	"truncate":    truncateString,
	"jsonTags":    jsonTags,
	"seq":         seq,
}

// seq returns [from..to] inclusive. Used by rating-star loops.
func seq(from, to int) []int {
	if to < from {
		return nil
	}
	out := make([]int, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

func formatDurationSeconds(sec float64) string {
	if sec <= 0 {
		return "–"
	}
	s := int(sec)
	h := s / 3600
	m := (s % 3600) / 60
	ss := s % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, ss)
	}
	return fmt.Sprintf("%d:%02d", m, ss)
}

func formatTimestamp(iso string) string {
	if len(iso) < 16 {
		return iso
	}
	return strings.Replace(iso[:16], "T", " ", 1)
}

func formatTimestampLong(iso string) string {
	if len(iso) < 19 {
		return iso
	}
	return strings.Replace(iso[:19], "T", " ", 1)
}

func percent(num, denom float64) float64 {
	if denom <= 0 {
		return 0
	}
	p := (num / denom) * 100
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

func defaultValue(def, v any) any {
	if v == nil {
		return def
	}
	if s, ok := v.(string); ok && s == "" {
		return def
	}
	return v
}

func truncateString(max int, s string) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// jsonTags parses a JSON array string into a slice of strings. Returns
// empty if parsing fails or input is blank — templates can range freely.
func jsonTags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// light-weight parse: strip brackets, split on comma, strip quotes.
	// Kept here (rather than encoding/json) so templates don't pay the
	// reflection cost on every render for a trivial structure.
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"`)
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parse builds one template tree per page. Each tree contains base.html +
// partials + exactly one page template, so redefinitions of blocks like
// `page` and `header_actions` across pages don't collide.
func parse() (map[string]*template.Template, error) {
	tmplOnce.Do(func() {
		partials, err := fs.Glob(templateFS, "templates/partials/*.html")
		if err != nil {
			tmplErr = err
			return
		}
		pages, err := fs.Glob(templateFS, "templates/*.html")
		if err != nil {
			tmplErr = err
			return
		}
		out := make(map[string]*template.Template, len(pages))
		for _, page := range pages {
			name := strings.TrimSuffix(strings.TrimPrefix(page, "templates/"), ".html")
			files := append([]string{"templates/base.html", page}, partials...)
			t, err := template.New(name).Funcs(funcMap).ParseFS(templateFS, files...)
			if err != nil {
				tmplErr = fmt.Errorf("parse %s: %w", page, err)
				return
			}
			out[name] = t
		}
		tmpls = out
	})
	return tmpls, tmplErr
}

// Render executes the page template `name` (e.g. "index", "video") with data.
// Each page tree includes base.html; the page-specific template defines
// `page` and `header_actions` blocks that base calls.
func Render(w io.Writer, name string, data any) error {
	trees, err := parse()
	if err != nil {
		return err
	}
	t, ok := trees[name]
	if !ok {
		return fmt.Errorf("unknown template %q", name)
	}
	return t.ExecuteTemplate(w, "base", data)
}

// PartialTemplate returns a single page tree so callers can execute a
// partial (e.g. "job_row") from a streaming handler.
func PartialTemplate(name string) (*template.Template, error) {
	trees, err := parse()
	if err != nil {
		return nil, err
	}
	for _, t := range trees {
		if p := t.Lookup(name); p != nil {
			return t, nil
		}
	}
	return nil, fmt.Errorf("partial %q not found", name)
}
