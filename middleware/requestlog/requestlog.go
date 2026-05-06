// Package requestlog provides a Fiber middleware that logs HTTP requests
// to a [filelog.FileLog] instance.
//
// Access log: every request whose URL extension is NOT in the skip set.
// Warning log: client errors (4xx), with optional unknown User-Agent warnings.
// Error log: server errors (5xx).
//
// The middleware is opinionated by default but exports its helper functions
// so they can be reused in custom middleware.
package requestlog

import (
	"net/http"
	"net/url"
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

// DefaultSkipPaths is the list of request paths excluded from access and
// warning logs. Matching accepts the exact path and mounted suffixes, so
// "/health" also matches "/api/health".
var DefaultSkipPaths = []string{
	"/health",
}

// DefaultRedactQueryParams is the list of query parameter names whose values
// are redacted in request logs.
var DefaultRedactQueryParams = []string{
	"pass", "password", "token", "key", "secret",
}

// DefaultKnownUserAgents are substrings present in every mainstream browser's
// User-Agent string. Any real browser includes at least one of these.
var DefaultKnownUserAgents = []string{
	"Mozilla", "Chrome", "Safari", "Firefox", "Edge", "Opera",
}

// Config controls the behaviour of the request logger middleware.
// All fields are optional; zero values use sensible defaults.
type Config struct {
	// SkipExtensions overrides the list of file extensions that are excluded
	// from the access log. If nil, DefaultSkipExtensions is used.
	SkipExtensions []string

	// SkipPaths overrides the list of paths that are excluded from access and
	// warning logs. If nil, DefaultSkipPaths is used. Use an empty slice to log
	// every path.
	SkipPaths []string

	// RedactQueryParams overrides the list of query parameter names whose
	// values are redacted in logged URLs. If nil, DefaultRedactQueryParams is
	// used. Use an empty slice to disable redaction.
	RedactQueryParams []string

	// WarnUnknownUserAgent writes successful requests with an unrecognised
	// User-Agent to warning.log. It is disabled by default to avoid noisy API
	// logs for legitimate non-browser clients.
	WarnUnknownUserAgent bool

	// KnownUserAgents overrides the User-Agent substrings used when
	// WarnUnknownUserAgent is true. If nil, DefaultKnownUserAgents is used.
	KnownUserAgents []string

	// SuspiciousPaths enables a separate suspicious log when the request path
	// matches one of these values. It is empty by default because edge scanners
	// are usually better handled at the reverse proxy.
	SuspiciousPaths []string

	// SuspiciousLogName is the filelog name used for SuspiciousPaths. It
	// defaults to "suspicious", creating suspicious.log on first use.
	SuspiciousLogName string
}

// ShouldSkipAccess reports whether ext (e.g. ".css") is a static asset
// extension that should be excluded from the access log.
func ShouldSkipAccess(ext string, skip map[string]struct{}) bool {
	_, ok := skip[ext]
	return ok
}

// ShouldSkipPath reports whether path is in the skip set. It accepts both
// exact matches and mounted suffixes, so "/health" also matches "/api/health".
func ShouldSkipPath(path string, skip []string) bool {
	for _, skipped := range skip {
		if skipped == "" {
			continue
		}
		if path == skipped || strings.HasSuffix(path, skipped) {
			return true
		}
	}
	return false
}

// IsKnownBrowser reports whether ua contains at least one token that
// identifies a mainstream web browser.
func IsKnownBrowser(ua string) bool {
	return IsKnownUserAgent(ua, DefaultKnownUserAgents)
}

// IsKnownUserAgent reports whether ua contains at least one of the supplied
// tokens.
func IsKnownUserAgent(ua string, tokens []string) bool {
	for _, token := range tokens {
		if strings.Contains(ua, token) {
			return true
		}
	}
	return false
}

// RedactURL redacts configured query parameter values in rawURL.
func RedactURL(rawURL string, params []string) string {
	if len(params) == 0 || rawURL == "" {
		return rawURL
	}

	redacted := make(map[string]struct{}, len(params))
	for _, param := range params {
		if param == "" {
			continue
		}
		redacted[strings.ToLower(param)] = struct{}{}
	}
	if len(redacted) == 0 {
		return rawURL
	}

	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return rawURL
	}

	query := parsed.Query()
	changed := false
	for name, values := range query {
		if _, ok := redacted[strings.ToLower(name)]; !ok {
			continue
		}
		for i := range values {
			values[i] = "REDACTED"
		}
		query[name] = values
		changed = true
	}
	if !changed {
		return rawURL
	}

	parsed.RawQuery = query.Encode()
	return parsed.RequestURI()
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
	skipExts := make(map[string]struct{}, len(exts))
	for _, e := range exts {
		skipExts[strings.ToLower(e)] = struct{}{}
	}

	skipPaths := cfg.SkipPaths
	if skipPaths == nil {
		skipPaths = DefaultSkipPaths
	}

	redactQueryParams := cfg.RedactQueryParams
	if redactQueryParams == nil {
		redactQueryParams = DefaultRedactQueryParams
	}

	knownUserAgents := cfg.KnownUserAgents
	if knownUserAgents == nil {
		knownUserAgents = DefaultKnownUserAgents
	}

	suspiciousLogName := cfg.SuspiciousLogName
	if suspiciousLogName == "" {
		suspiciousLogName = "suspicious"
	}

	return func(c fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		duration := time.Since(start).Round(time.Millisecond)

		path := c.Path()
		status := c.Response().StatusCode()
		ext := strings.ToLower(filepath.Ext(path))
		skipPath := ShouldSkipPath(path, skipPaths)
		logURL := RedactURL(c.OriginalURL(), redactQueryParams)
		ua := c.Get("User-Agent")

		// Access log: skip static assets and configured noisy endpoints.
		if !skipPath && !ShouldSkipAccess(ext, skipExts) {
			fl.Access("| %s | %s %s (%s) | %d", c.IP(), c.Method(), logURL, duration, status)
		}

		// Warning log: client errors, optionally unknown User-Agents.
		warn := status >= http.StatusBadRequest && status < http.StatusInternalServerError
		if cfg.WarnUnknownUserAgent && !IsKnownUserAgent(ua, knownUserAgents) {
			warn = true
		}
		if warn && !skipPath {
			fl.Warning("| %s | %s %s (%s) | %d | %q", c.IP(), c.Method(), logURL, duration, status, ua)
		}

		// Error log: server errors
		if status >= http.StatusInternalServerError {
			fl.Error("| %s | %s %s (%s) | %d | %q", c.IP(), c.Method(), logURL, duration, status, ua)
		}

		if len(cfg.SuspiciousPaths) > 0 && ShouldSkipPath(path, cfg.SuspiciousPaths) {
			fl.Log(suspiciousLogName, "| %s | %s %s (%s) | %d | %q", c.IP(), c.Method(), logURL, duration, status, ua)
		}

		return err
	}
}
