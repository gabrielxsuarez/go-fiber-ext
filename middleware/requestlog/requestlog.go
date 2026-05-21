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
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
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
	"authorization", "auth", "pass", "password", "token", "key", "secret",
	"clave", "usuario_clave", "api_key", "apikey", "access_token",
	"refresh_token", "bearer",
}

// DefaultKnownUserAgents are substrings present in every mainstream browser's
// User-Agent string. Any real browser includes at least one of these.
var DefaultKnownUserAgents = []string{
	"Mozilla", "Chrome", "Safari", "Firefox", "Edge", "Opera",
}

// RequestIDLocalKey is the Fiber locals key used to expose the request ID.
const RequestIDLocalKey = "request_id"

const maxRequestIDLength = 128

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

	// RequestIDHeader is the header used to read/write request IDs.
	// Default: X-Request-ID.
	RequestIDHeader string
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

// RedactQueryString redacts configured query parameter values in a raw query
// string without the leading '?'.
func RedactQueryString(rawQuery string, params []string) string {
	if rawQuery == "" {
		return ""
	}
	redacted := RedactURL("/?"+rawQuery, params)
	if strings.HasPrefix(redacted, "/?") {
		return strings.TrimPrefix(redacted, "/?")
	}
	return rawQuery
}

// RequestID returns the request ID stored by this middleware in Fiber locals.
func RequestID(c fiber.Ctx) string {
	requestID, _ := c.Locals(RequestIDLocalKey).(string)
	return requestID
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
	requestIDHeader := cfg.RequestIDHeader
	if requestIDHeader == "" {
		requestIDHeader = fiber.HeaderXRequestID
	}

	return func(c fiber.Ctx) error {
		requestID := ensureRequestID(c, requestIDHeader)
		start := time.Now()
		err := c.Next()
		duration := time.Since(start)

		path := c.Path()
		status := statusCode(c.Response().StatusCode(), err)
		ext := strings.ToLower(filepath.Ext(path))
		skipPath := ShouldSkipPath(path, skipPaths)
		logURL := RedactURL(c.OriginalURL(), redactQueryParams)
		query := RedactQueryString(string(c.Request().URI().QueryString()), redactQueryParams)
		ua := c.Get("User-Agent")
		attrs := requestAttrs(c, requestID, logURL, query, status, duration, err)

		// Access log: skip static assets and configured noisy endpoints.
		if !skipPath && !ShouldSkipAccess(ext, skipExts) {
			fl.AccessAttrs("http_request", attrs...)
		}

		// Warning log: client errors, optionally unknown User-Agents.
		warn := status >= http.StatusBadRequest && status < http.StatusInternalServerError
		if cfg.WarnUnknownUserAgent && !IsKnownUserAgent(ua, knownUserAgents) {
			warn = true
		}
		if warn && !skipPath {
			fl.WarningAttrs("http_warning", attrs...)
		}

		// Error log: server errors
		if status >= http.StatusInternalServerError {
			fl.ErrorAttrs("http_error", attrs...)
		}

		if len(cfg.SuspiciousPaths) > 0 && ShouldSkipPath(path, cfg.SuspiciousPaths) {
			fl.LogAttrs(suspiciousLogName, "http_suspicious", attrs...)
		}

		return err
	}
}

func requestAttrs(c fiber.Ctx, requestID, logURL, query string, status int, duration time.Duration, err error) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("request_id", requestID),
		slog.String("client_ip", copyString(c.IP())),
		slog.String("xff", copyString(c.Get(fiber.HeaderXForwardedFor))),
		slog.String("host", copyString(c.Hostname())),
		slog.String("method", copyString(c.Method())),
		slog.String("path", copyString(c.Path())),
		slog.String("route", routePath(c)),
		slog.String("url", copyString(logURL)),
		slog.String("query", copyString(query)),
		slog.Int("status", status),
		slog.Int64("dur_ms", duration.Milliseconds()),
		slog.Int("req_bytes", len(c.Request().Body())),
		slog.Int("resp_bytes", nonNegative(c.Response().Header.ContentLength())),
		slog.String("ua", copyString(c.Get(fiber.HeaderUserAgent))),
		slog.String("referer", copyString(c.Get(fiber.HeaderReferer))),
		slog.String("content_type", copyString(c.Get(fiber.HeaderContentType))),
		slog.String("accept_encoding", copyString(c.Get(fiber.HeaderAcceptEncoding))),
		slog.String("content_encoding", copyString(c.GetRespHeader(fiber.HeaderContentEncoding))),
	}
	if err != nil {
		attrs = append(attrs,
			slog.String("error", copyString(err.Error())),
			slog.String("error_kind", errorKind(err)),
		)
	}
	return attrs
}

func statusCode(current int, err error) int {
	if err == nil {
		return current
	}
	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) && fiberErr.Code > 0 {
		return fiberErr.Code
	}
	if current >= http.StatusBadRequest {
		return current
	}
	return http.StatusInternalServerError
}

func errorKind(err error) string {
	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		return "fiber_" + strconv.Itoa(fiberErr.Code)
	}
	return "error"
}

func routePath(c fiber.Ctx) string {
	route := c.Route()
	if route == nil {
		return ""
	}
	return copyString(route.Path)
}

func nonNegative(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func ensureRequestID(c fiber.Ctx, header string) string {
	requestID := c.Get(header)
	if !validRequestID(requestID) {
		requestID = generateRequestID()
	}
	c.Set(header, requestID)
	c.Locals(RequestIDLocalKey, requestID)
	return copyString(requestID)
}

func validRequestID(value string) bool {
	if value == "" || len(value) > maxRequestIDLength {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

func generateRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func copyString(value string) string {
	if value == "" {
		return ""
	}
	return strings.Clone(value)
}
