// Package requestlog provides a Fiber middleware that logs HTTP requests
// to a [filelog.FileLog] instance.
//
// Access log: every request whose URL extension is NOT in the skip set.
// Warning log: requests that result in status >= 400 or come from an
// unrecognised User-Agent (empty or not matching any known browser token).
//
// The middleware is opinionated by default but exports its helper functions
// so they can be reused in custom middleware.
package requestlog

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gabrielxsuarez/go-fiber-ext/filelog"
	"github.com/gofiber/fiber/v3"
)

// DefaultSkipExtensions is the list of file extensions whose requests are
// not written to the access log. These are typically static assets served
// by a CDN or cache layer.
var DefaultSkipExtensions = []string{
	".css", ".js", ".png", ".jpg", ".jpeg",
	".gif", ".svg", ".ico", ".woff", ".woff2",
	".ttf", ".eot", ".webp", ".avif", ".map",
}

// defaultBrowserTokens are substrings present in every mainstream browser's
// User-Agent string. Any real browser includes at least one of these.
var defaultBrowserTokens = []string{
	"Mozilla", "Chrome", "Safari", "Firefox", "Edge", "Opera",
}

// Config controls the behaviour of the request logger middleware.
// All fields are optional; zero values use sensible defaults.
type Config struct {
	// SkipExtensions overrides the list of file extensions that are excluded
	// from the access log. If nil, DefaultSkipExtensions is used.
	SkipExtensions []string
}

// ShouldSkipAccess reports whether ext (e.g. ".css") is a static asset
// extension that should be excluded from the access log.
func ShouldSkipAccess(ext string, skip map[string]struct{}) bool {
	_, ok := skip[ext]
	return ok
}

// IsKnownBrowser reports whether ua contains at least one token that
// identifies a mainstream web browser.
func IsKnownBrowser(ua string) bool {
	for _, token := range defaultBrowserTokens {
		if strings.Contains(ua, token) {
			return true
		}
	}
	return false
}

// New creates a Fiber middleware that logs requests to fl.
func New(fl *filelog.FileLog, cfgs ...Config) fiber.Handler {
	var cfg Config
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	exts := cfg.SkipExtensions
	if exts == nil {
		exts = DefaultSkipExtensions
	}
	skip := make(map[string]struct{}, len(exts))
	for _, e := range exts {
		skip[e] = struct{}{}
	}

	return func(c fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		duration := time.Since(start).Round(time.Millisecond)

		path := c.Path()
		status := c.Response().StatusCode()
		ext := strings.ToLower(filepath.Ext(path))

		// Access log: skip static asset extensions
		if !ShouldSkipAccess(ext, skip) {
			fl.Access("| %s | %s %s (%s)", c.IP(), c.Method(), c.OriginalURL(), duration)
		}

		// Warning log: error status or unrecognised User-Agent
		ua := c.Get("User-Agent")
		if status >= http.StatusBadRequest || !IsKnownBrowser(ua) {
			fl.Warning("| %s | %s %s (%s) | %d | %q", c.IP(), c.Method(), c.OriginalURL(), duration, status, ua)
		}

		return err
	}
}
