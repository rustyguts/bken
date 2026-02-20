package main

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// linkPreviewTimeout is the maximum time the server will spend fetching a URL
// for link preview metadata. Kept short so chat messages are never delayed.
const linkPreviewTimeout = 4 * time.Second

// linkPreviewMaxBody is the maximum number of bytes read from a page when
// extracting OpenGraph metadata. We only need the <head> section.
const linkPreviewMaxBody = 256 * 1024 // 256 KB

// urlPattern matches http:// and https:// URLs in message text.
var urlPattern = regexp.MustCompile(`https?://[^\s<>"]+`)

// extractFirstURL returns the first http(s) URL found in text, or "".
func extractFirstURL(text string) string {
	return urlPattern.FindString(text)
}

// LinkPreview holds OpenGraph metadata extracted from a web page.
type LinkPreview struct {
	URL      string
	Title    string
	Desc     string
	Image    string
	SiteName string
}

// fetchLinkPreview fetches the given URL and extracts OpenGraph metadata.
// Returns a zero LinkPreview and an error if the fetch or parse fails.
// The caller should run this in a goroutine to avoid blocking chat delivery.
func fetchLinkPreview(rawURL string) (LinkPreview, error) {
	client := &http.Client{
		Timeout: linkPreviewTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return LinkPreview{}, err
	}
	req.Header.Set("User-Agent", "bken-linkpreview/1.0")
	req.Header.Set("Accept", "text/html")

	resp, err := client.Do(req)
	if err != nil {
		return LinkPreview{}, err
	}
	defer resp.Body.Close()

	// Only parse HTML responses.
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") && !strings.Contains(ct, "application/xhtml") {
		return LinkPreview{URL: rawURL}, nil
	}

	body := io.LimitReader(resp.Body, linkPreviewMaxBody)
	return parseOGTags(rawURL, body)
}

// parseOGTags reads HTML from r and extracts OpenGraph meta tags and the <title>.
func parseOGTags(rawURL string, r io.Reader) (LinkPreview, error) {
	lp := LinkPreview{URL: rawURL}
	tokenizer := html.NewTokenizer(r)
	var inTitle bool
	var titleText string

	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			// EOF or error — done parsing.
			if lp.Title == "" && titleText != "" {
				lp.Title = titleText
			}
			return lp, nil

		case html.StartTagToken, html.SelfClosingTagToken:
			tn, hasAttr := tokenizer.TagName()
			tag := string(tn)

			if tag == "title" {
				inTitle = true
				continue
			}

			// Stop at <body> — no need to parse further.
			if tag == "body" {
				if lp.Title == "" && titleText != "" {
					lp.Title = titleText
				}
				return lp, nil
			}

			if tag == "meta" && hasAttr {
				parseMeta(tokenizer, &lp)
			}

		case html.TextToken:
			if inTitle {
				titleText += string(tokenizer.Text())
			}

		case html.EndTagToken:
			tn, _ := tokenizer.TagName()
			if string(tn) == "title" {
				inTitle = false
			}
		}
	}
}

// parseMeta extracts OpenGraph and standard meta properties from a <meta> tag.
func parseMeta(tokenizer *html.Tokenizer, lp *LinkPreview) {
	var property, name, content string
	for {
		key, val, more := tokenizer.TagAttr()
		k := string(key)
		v := string(val)
		switch k {
		case "property":
			property = v
		case "name":
			name = v
		case "content":
			content = v
		}
		if !more {
			break
		}
	}

	if content == "" {
		return
	}

	switch property {
	case "og:title":
		lp.Title = content
	case "og:description":
		lp.Desc = content
	case "og:image":
		lp.Image = content
	case "og:site_name":
		lp.SiteName = content
	}

	// Fallback to standard meta tags if OG is not set.
	if name == "description" && lp.Desc == "" {
		lp.Desc = content
	}
}
