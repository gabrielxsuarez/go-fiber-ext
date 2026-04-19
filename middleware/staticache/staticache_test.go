package staticache

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gofiber/fiber/v3"
)

func TestNewFSRegistersIndexRoutes(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		"index.html":      {Data: []byte("<html><body>home</body></html>")},
		"docs/index.html": {Data: []byte("<html><body>docs</body></html>")},
	}, Config{IndexFile: "index.html"})

	if cache.files["/"] != cache.files["/index.html"] {
		t.Fatalf("expected root index route to be registered")
	}
	if cache.files["/docs/"] != cache.files["/docs/index.html"] {
		t.Fatalf("expected /docs/ index route to be registered")
	}
	if cache.files["/docs"] != cache.files["/docs/index.html"] {
		t.Fatalf("expected /docs index route without trailing slash to be registered")
	}
}

func TestOnlyManagedFilesAreCached(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		"app.css":                    {Data: []byte("body { color: red; }")},
		"logo.webp":                  {Data: []byte("not really a webp")},
		"file.bin":                   {Data: []byte("binary")},
		".well-known/traffic-advice": {Data: []byte(`{"ttl": 300}`)},
	}, Config{})

	if _, found := cache.files["/app.css"]; !found {
		t.Fatalf("expected managed css file to be cached")
	}
	if _, found := cache.files[builtinTrafficAdvice]; !found {
		t.Fatalf("expected traffic advice file to be cached")
	}
	if _, found := cache.files["/logo.webp"]; found {
		t.Fatalf("did not expect unsupported webp file to be cached")
	}
	if _, found := cache.files["/file.bin"]; found {
		t.Fatalf("did not expect unsupported binary file to be cached")
	}
}

func TestTrafficAdviceUsesBuiltinMimeTypeInProd(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		".well-known/traffic-advice": {Data: []byte(`{"ttl": 300}`)},
	}, Config{})
	app := newTestApp(cache)

	resp := performRequest(t, app, http.MethodGet, builtinTrafficAdvice, nil)
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != builtinTrafficAdviceMIME {
		t.Fatalf("expected content-type %q, got %q", builtinTrafficAdviceMIME, got)
	}
}

func TestCompressionStoredOnlyWhenSmaller(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		"tiny.css": {Data: []byte("a{}")},
		"big.css":  {Data: compressibleCSS()},
	})

	tiny := cache.files["/tiny.css"]
	if tiny.variants[encodingGzip] != nil || tiny.variants[encodingBrotli] != nil {
		t.Fatalf("expected tiny asset to keep only identity representation")
	}

	big := cache.files["/big.css"]
	if big.variants[encodingGzip] == nil && big.variants[encodingBrotli] == nil {
		t.Fatalf("expected compressible asset to keep at least one compressed representation")
	}
}

func TestHandlerNegotiatesEncodingWithQValues(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		"app.css": {Data: compressibleCSS()},
	}, Config{})
	app := newTestApp(cache)

	tests := []struct {
		name           string
		acceptEncoding string
		wantStatus     int
		wantEncoding   string
		wantVary       string
	}{
		{
			name:           "prefers higher q over server order with identity excluded",
			acceptEncoding: "br;q=0.4, gzip;q=0.9, identity;q=0",
			wantStatus:     fiber.StatusOK,
			wantEncoding:   encodingGzip,
			wantVary:       varyAcceptEncoding,
		},
		{
			name:           "prefers brotli on tie",
			acceptEncoding: "gzip;q=1, br;q=1",
			wantStatus:     fiber.StatusOK,
			wantEncoding:   encodingBrotli,
			wantVary:       varyAcceptEncoding,
		},
		{
			name:           "falls back to identity when compressed variants are rejected",
			acceptEncoding: "gzip;q=0, br;q=0",
			wantStatus:     fiber.StatusOK,
			wantEncoding:   "",
			wantVary:       varyAcceptEncoding,
		},
		{
			name:           "respects explicit identity preference",
			acceptEncoding: "identity;q=0.9, gzip;q=0.4, br;q=0.4",
			wantStatus:     fiber.StatusOK,
			wantEncoding:   "",
			wantVary:       varyAcceptEncoding,
		},
		{
			name:           "returns 406 when identity is also rejected",
			acceptEncoding: "gzip;q=0, br;q=0, identity;q=0",
			wantStatus:     fiber.StatusNotAcceptable,
			wantEncoding:   "",
			wantVary:       varyAcceptEncoding,
		},
		{
			name:           "implicit identity has default quality",
			acceptEncoding: "gzip;q=0.9",
			wantStatus:     fiber.StatusOK,
			wantEncoding:   "",
			wantVary:       varyAcceptEncoding,
		},
		{
			name:           "explicit identity quality overrides implicit default",
			acceptEncoding: "gzip;q=0.9, identity;q=0.5",
			wantStatus:     fiber.StatusOK,
			wantEncoding:   encodingGzip,
			wantVary:       varyAcceptEncoding,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := performRequest(t, app, http.MethodGet, "/app.css", map[string]string{
				"Accept-Encoding": tt.acceptEncoding,
			})
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}
			if got := resp.Header.Get("Content-Encoding"); got != tt.wantEncoding {
				t.Fatalf("expected content-encoding %q, got %q", tt.wantEncoding, got)
			}
			if got := resp.Header.Get("Vary"); got != tt.wantVary {
				t.Fatalf("expected vary %q, got %q", tt.wantVary, got)
			}
		})
	}
}

func TestHandlerValidatesIfNoneMatchAgainstSelectedRepresentation(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		"app.css": {Data: compressibleCSS()},
	}, Config{})
	app := newTestApp(cache)

	gzipResp := performRequest(t, app, http.MethodGet, "/app.css", map[string]string{
		"Accept-Encoding": "gzip",
	})
	defer gzipResp.Body.Close()
	brResp := performRequest(t, app, http.MethodGet, "/app.css", map[string]string{
		"Accept-Encoding": "br",
	})
	defer brResp.Body.Close()

	gzipTag := gzipResp.Header.Get("ETag")
	brTag := brResp.Header.Get("ETag")

	if gzipTag == "" || brTag == "" {
		t.Fatalf("expected etags for compressed representations")
	}
	if gzipTag == brTag {
		t.Fatalf("expected distinct etags per representation variant")
	}

	tests := []struct {
		name           string
		acceptEncoding string
		ifNoneMatch    string
		wantStatus     int
	}{
		{
			name:           "different variant tag does not produce 304",
			acceptEncoding: "br",
			ifNoneMatch:    gzipTag,
			wantStatus:     fiber.StatusOK,
		},
		{
			name:           "weak comparison matches selected representation",
			acceptEncoding: "br",
			ifNoneMatch:    "W/" + brTag,
			wantStatus:     fiber.StatusNotModified,
		},
		{
			name:           "list values are parsed correctly",
			acceptEncoding: "gzip",
			ifNoneMatch:    `"other", W/` + gzipTag,
			wantStatus:     fiber.StatusNotModified,
		},
		{
			name:           "star matches existing resource",
			acceptEncoding: "br",
			ifNoneMatch:    "*",
			wantStatus:     fiber.StatusNotModified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := performRequest(t, app, http.MethodGet, "/app.css", map[string]string{
				"Accept-Encoding": tt.acceptEncoding,
				"If-None-Match":   tt.ifNoneMatch,
			})
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}
		})
	}
}

func TestHeadReturnsRepresentationHeadersWithoutBody(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		"app.css": {Data: compressibleCSS()},
	}, Config{})
	app := newTestApp(cache)

	resp := performRequest(t, app, http.MethodHead, "/app.css", map[string]string{
		"Accept-Encoding": "gzip",
	})
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	gzipVariant := cache.files["/app.css"].variants[encodingGzip]
	if gzipVariant == nil {
		t.Fatalf("expected gzip representation to exist")
	}

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Encoding"); got != encodingGzip {
		t.Fatalf("expected content-encoding %q, got %q", encodingGzip, got)
	}
	if len(body) != 0 {
		t.Fatalf("expected empty body for HEAD, got %d bytes", len(body))
	}
	if got := resp.Header.Get("Content-Length"); got != strconv.Itoa(len(gzipVariant.body)) {
		t.Fatalf("expected content-length %d, got %q", len(gzipVariant.body), got)
	}
}

func TestAcceptEncodingAbsentReturnsIdentity(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		"app.css": {Data: compressibleCSS()},
	}, Config{})
	app := newTestApp(cache)

	resp := performRequest(t, app, http.MethodGet, "/app.css", nil)
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("expected no Content-Encoding, got %q", got)
	}
	if got := resp.Header.Get("Vary"); got != varyAcceptEncoding {
		t.Fatalf("expected Vary %q, got %q", varyAcceptEncoding, got)
	}
}

func TestAcceptEncodingWhitespaceOnlyReturnsIdentity(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		"app.css": {Data: compressibleCSS()},
	}, Config{})
	app := newTestApp(cache)

	resp := performRequest(t, app, http.MethodGet, "/app.css", map[string]string{
		"Accept-Encoding": "   ",
	})
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("expected no Content-Encoding, got %q", got)
	}
}

func TestWildcardAcceptEncoding(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		"app.css": {Data: compressibleCSS()},
	}, Config{})
	app := newTestApp(cache)

	resp := performRequest(t, app, http.MethodGet, "/app.css", map[string]string{
		"Accept-Encoding": "*;q=0.5",
	})
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Encoding"); got != encodingBrotli {
		t.Fatalf("expected Content-Encoding %q, got %q", encodingBrotli, got)
	}
}

func TestVaryMergesWithExistingHeader(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		"app.css": {Data: compressibleCSS()},
	}, Config{})
	app := fiber.New()
	app.Use(func(ctx fiber.Ctx) error {
		ctx.Set("Vary", "Origin")
		return ctx.Next()
	})
	app.Use(cache.Handler())

	resp := performRequest(t, app, http.MethodGet, "/app.css", map[string]string{
		"Accept-Encoding": "gzip",
	})
	defer resp.Body.Close()

	if got := resp.Header.Get("Vary"); got != "Origin, Accept-Encoding" {
		t.Fatalf("expected Vary %q, got %q", "Origin, Accept-Encoding", got)
	}
}

func TestVaryNotDuplicatedCaseInsensitive(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		"app.css": {Data: compressibleCSS()},
	}, Config{})
	app := fiber.New()
	app.Use(func(ctx fiber.Ctx) error {
		ctx.Set("Vary", "Origin, accept-encoding")
		return ctx.Next()
	})
	app.Use(cache.Handler())

	resp := performRequest(t, app, http.MethodGet, "/app.css", map[string]string{
		"Accept-Encoding": "gzip",
	})
	defer resp.Body.Close()

	if got := resp.Header.Get("Vary"); got != "Origin, accept-encoding" {
		t.Fatalf("expected Vary to remain unchanged, got %q", got)
	}
}

func TestDefaultCachePolicyRevalidates(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		"app.css": {Data: compressibleCSS()},
	}, Config{})
	app := newTestApp(cache)

	resp := performRequest(t, app, http.MethodGet, "/app.css", map[string]string{
		"Accept-Encoding": "gzip",
	})
	defer resp.Body.Close()

	if got := resp.Header.Get("Cache-Control"); got != defaultCacheControl {
		t.Fatalf("expected Cache-Control %q, got %q", defaultCacheControl, got)
	}
	if got := resp.Header.Get("Pragma"); got != defaultPragma {
		t.Fatalf("expected Pragma %q, got %q", defaultPragma, got)
	}
}

func TestDevModeServesFreshContentWithoutRestart(t *testing.T) {
	root := t.TempDir()
	writeTempFile(t, root, "app.css", "body { color: red; }\n")

	cache, err := New(root, Config{DevMode: true})
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	app := newTestApp(cache)

	first := performRequest(t, app, http.MethodGet, "/app.css", nil)
	body, err := io.ReadAll(first.Body)
	first.Body.Close()
	if err != nil {
		t.Fatalf("read first body: %v", err)
	}
	firstTag := first.Header.Get("ETag")

	if string(body) != "body { color: red; }\n" {
		t.Fatalf("expected dev mode to serve source file without minify")
	}
	if got := first.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("expected no Content-Encoding in dev mode, got %q", got)
	}
	if got := first.Header.Get("Cache-Control"); got != defaultCacheControl {
		t.Fatalf("expected Cache-Control %q, got %q", defaultCacheControl, got)
	}
	if got := first.Header.Get("Pragma"); got != defaultPragma {
		t.Fatalf("expected Pragma %q, got %q", defaultPragma, got)
	}
	if got := first.Header.Get("Vary"); got != "" {
		t.Fatalf("expected no Vary header in dev mode, got %q", got)
	}

	writeTempFile(t, root, "app.css", "body { color: blue; }\n")

	second := performRequest(t, app, http.MethodGet, "/app.css", nil)
	body, err = io.ReadAll(second.Body)
	second.Body.Close()
	if err != nil {
		t.Fatalf("read second body: %v", err)
	}
	secondTag := second.Header.Get("ETag")

	if string(body) != "body { color: blue; }\n" {
		t.Fatalf("expected updated file contents without restarting the server")
	}
	if firstTag == "" || secondTag == "" || firstTag == secondTag {
		t.Fatalf("expected dev mode etag to change after modifying the file")
	}
}

func TestDevModeReturns304ForCurrentFile(t *testing.T) {
	root := t.TempDir()
	writeTempFile(t, root, "app.css", "body { color: red; }\n")

	cache, err := New(root, Config{DevMode: true})
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	app := newTestApp(cache)

	first := performRequest(t, app, http.MethodGet, "/app.css", nil)
	firstTag := first.Header.Get("ETag")
	first.Body.Close()

	notModified := performRequest(t, app, http.MethodGet, "/app.css", map[string]string{
		"If-None-Match": firstTag,
	})
	defer notModified.Body.Close()

	if notModified.StatusCode != fiber.StatusNotModified {
		t.Fatalf("expected status 304, got %d", notModified.StatusCode)
	}

	writeTempFile(t, root, "app.css", "body { color: blue; }\n")

	modified := performRequest(t, app, http.MethodGet, "/app.css", map[string]string{
		"If-None-Match": firstTag,
	})
	defer modified.Body.Close()

	if modified.StatusCode != fiber.StatusOK {
		t.Fatalf("expected status 200 after file changes, got %d", modified.StatusCode)
	}
}

func TestDevModeTrafficAdviceUsesBuiltinMimeType(t *testing.T) {
	root := t.TempDir()
	writeTempFile(t, root, filepath.Join(".well-known", "traffic-advice"), `{"ttl",300}`)

	cache, err := New(root, Config{DevMode: true})
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	app := newTestApp(cache)

	resp := performRequest(t, app, http.MethodGet, builtinTrafficAdvice, nil)
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != builtinTrafficAdviceMIME {
		t.Fatalf("expected content-type %q, got %q", builtinTrafficAdviceMIME, got)
	}
}

func TestDevModeCanServeNewManagedFilesAddedAfterStartup(t *testing.T) {
	root := t.TempDir()

	cache, err := New(root, Config{DevMode: true})
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	app := newTestApp(cache)

	writeTempFile(t, root, "new.js", "console.log('new file');\n")

	resp := performRequest(t, app, http.MethodGet, "/new.js", nil)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if string(body) != "console.log('new file');\n" {
		t.Fatalf("expected newly added file to be served in dev mode")
	}
}

func TestNonManagedFilesPassThrough(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		"logo.webp": {Data: []byte("fake image")},
	}, Config{})
	app := newTestApp(cache)

	// First request: staticache injects ETag but the file is served by the next handler.
	// Since there is no fallback handler in the test app, we get 404 but the ETag header
	// should still be present.
	resp := performRequest(t, app, http.MethodGet, "/logo.webp", nil)
	defer resp.Body.Close()

	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatalf("expected ETag header for passthrough file, got none")
	}

	// Second request with If-None-Match: should get 304.
	resp2 := performRequest(t, app, http.MethodGet, "/logo.webp", map[string]string{
		"If-None-Match": etag,
	})
	defer resp2.Body.Close()

	if resp2.StatusCode != fiber.StatusNotModified {
		t.Fatalf("expected 304 for passthrough file with matching ETag, got %d", resp2.StatusCode)
	}
}

func TestNonGetHeadMethodsPassThrough(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		"app.css": {Data: compressibleCSS()},
	}, Config{})
	app := newTestApp(cache)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		resp := performRequest(t, app, method, "/app.css", nil)
		resp.Body.Close()

		if resp.Header.Get("ETag") != "" {
			t.Fatalf("expected no ETag header for %s request", method)
		}
	}
}

func TestNextFunctionSkipsHandler(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		"app.css": {Data: compressibleCSS()},
	}, Config{
		Next: func(ctx fiber.Ctx) bool {
			return true
		},
	})
	app := newTestApp(cache)

	resp := performRequest(t, app, http.MethodGet, "/app.css", nil)
	defer resp.Body.Close()

	if resp.Header.Get("ETag") != "" {
		t.Fatalf("expected no ETag when Next skips")
	}
}

func TestEmptyFileIsCached(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		"empty.css": {Data: []byte("")},
	}, Config{})

	cf, found := cache.files["/empty.css"]
	if !found {
		t.Fatalf("expected empty.css to be cached")
	}
	identity := cf.variants[encodingIdentity]
	if identity == nil {
		t.Fatalf("expected identity variant for empty file")
	}
	if len(identity.body) != 0 {
		t.Fatalf("expected empty body, got %d bytes", len(identity.body))
	}
}

func TestETagIsTruncated(t *testing.T) {
	cache := newTestCache(t, fstest.MapFS{
		"app.css": {Data: compressibleCSS()},
	}, Config{})
	app := newTestApp(cache)

	resp := performRequest(t, app, http.MethodGet, "/app.css", map[string]string{
		"Accept-Encoding": "identity",
	})
	defer resp.Body.Close()

	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatalf("expected ETag header")
	}
	if inner := strings.Trim(etag, `"`); len(inner) != 16 {
		t.Fatalf("expected ETag to be 16 hex chars, got %d chars: %s", len(inner), inner)
	}
}

func newTestCache(t *testing.T, files fstest.MapFS, configs ...Config) *Cache {
	t.Helper()

	cache, err := NewFS(files, configs...)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}

	return cache
}

func newTestApp(cache *Cache) *fiber.App {
	app := fiber.New()
	app.Use(cache.Handler())
	return app
}

func performRequest(t *testing.T, app *fiber.App, method, target string, headers map[string]string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(method, target, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app test: %v", err)
	}

	return resp
}

func writeTempFile(t *testing.T, root, relativePath, contents string) {
	t.Helper()

	fullPath := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", fullPath, err)
	}
	if err := os.WriteFile(fullPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", fullPath, err)
	}
}

func compressibleCSS() []byte {
	return []byte(strings.Repeat(".banner{color:red}.hero{margin:0;padding:0}", 512))
}
