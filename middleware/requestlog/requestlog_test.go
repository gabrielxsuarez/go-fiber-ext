package requestlog

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabrielxsuarez/go-fiber-ext/filelog"
	"github.com/gofiber/fiber/v3"
)

func TestDefaultLogsStatusRedactsQueryAndSkipsHealth(t *testing.T) {
	dir := t.TempDir()
	fl := filelog.New(dir)
	t.Cleanup(func() {
		if err := fl.Close(); err != nil {
			t.Fatalf("close logs: %v", err)
		}
	})

	app := fiber.New()
	app.Use(New(fl))
	app.Get("/secret", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})
	app.Get("/health", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})
	app.Get("/api/health", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	doRequest(t, app, "/secret?name=bob&pass=abc123&token=tok456", "Wget")
	doRequest(t, app, "/health", "Wget")
	doRequest(t, app, "/api/health", "Wget")

	access := readLog(t, dir, "access.log")
	entry := decodeLogLine(t, access)
	if entry["msg"] != "http_request" {
		t.Fatalf("access log msg = %v, want http_request", entry["msg"])
	}
	if number(entry["status"]) != 200 {
		t.Fatalf("access log status = %v, want 200", entry["status"])
	}
	if !strings.Contains(access, "pass=REDACTED") || !strings.Contains(access, "token=REDACTED") {
		t.Fatalf("access log does not redact sensitive query params: %q", access)
	}
	if strings.Contains(access, "abc123") || strings.Contains(access, "tok456") {
		t.Fatalf("access log leaked sensitive query values: %q", access)
	}
	if entry["request_id"] == "" {
		t.Fatalf("access log does not include request_id: %q", access)
	}
	if entry["method"] != "GET" || entry["path"] != "/secret" {
		t.Fatalf("access log method/path unexpected: %#v", entry)
	}
	if strings.Contains(access, "/health") {
		t.Fatalf("access log contains skipped health path: %q", access)
	}
	assertNoLog(t, dir, "warning.log")
}

func TestWarningAndErrorLogsUseStatusClasses(t *testing.T) {
	dir := t.TempDir()
	fl := filelog.New(dir)
	t.Cleanup(func() {
		if err := fl.Close(); err != nil {
			t.Fatalf("close logs: %v", err)
		}
	})

	app := fiber.New()
	app.Use(New(fl))
	app.Get("/not-found", func(c fiber.Ctx) error {
		return c.Status(fiber.StatusNotFound).SendString("missing")
	})
	app.Get("/boom", func(c fiber.Ctx) error {
		return c.Status(fiber.StatusServiceUnavailable).SendString("boom")
	})

	doRequest(t, app, "/not-found", "Mozilla/5.0")
	doRequest(t, app, "/boom", "Mozilla/5.0")

	warning := readLog(t, dir, "warning.log")
	warningEntry := decodeLogLine(t, warning)
	if warningEntry["path"] != "/not-found" || number(warningEntry["status"]) != 404 {
		t.Fatalf("warning log missing 4xx request: %#v", warningEntry)
	}

	errorLog := readLog(t, dir, "error.log")
	errorEntry := decodeLogLine(t, errorLog)
	if errorEntry["path"] != "/boom" || number(errorEntry["status"]) != 503 {
		t.Fatalf("error log missing 5xx request: %#v", errorEntry)
	}
}

func TestUnknownUserAgentWarningIsConfigurable(t *testing.T) {
	dir := t.TempDir()
	fl := filelog.New(dir)
	t.Cleanup(func() {
		if err := fl.Close(); err != nil {
			t.Fatalf("close logs: %v", err)
		}
	})

	app := fiber.New()
	app.Use(New(fl, Config{
		WarnUnknownUserAgent: true,
	}))
	app.Get("/ok", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	doRequest(t, app, "/ok", "CustomBot/1.0")

	warning := readLog(t, dir, "warning.log")
	entry := decodeLogLine(t, warning)
	if entry["path"] != "/ok" || entry["ua"] != "CustomBot/1.0" {
		t.Fatalf("warning log missing unknown user-agent request: %#v", entry)
	}
}

func TestRequestIDHeaderIsPreservedAndExposed(t *testing.T) {
	dir := t.TempDir()
	fl := filelog.New(dir)
	t.Cleanup(func() {
		if err := fl.Close(); err != nil {
			t.Fatalf("close logs: %v", err)
		}
	})

	var seenRequestID string
	app := fiber.New()
	app.Use(New(fl))
	app.Get("/ok", func(c fiber.Ctx) error {
		seenRequestID = RequestID(c)
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/ok", nil)
	req.Header.Set(fiber.HeaderXRequestID, "req-test-123")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request /ok: %v", err)
	}
	if resp.Body != nil {
		resp.Body.Close()
	}

	if got := resp.Header.Get(fiber.HeaderXRequestID); got != "req-test-123" {
		t.Fatalf("response request id header = %q, want req-test-123", got)
	}
	if seenRequestID != "req-test-123" {
		t.Fatalf("RequestID(c) = %q, want req-test-123", seenRequestID)
	}

	entry := decodeLogLine(t, readLog(t, dir, "access.log"))
	if entry["request_id"] != "req-test-123" {
		t.Fatalf("logged request_id = %v, want req-test-123", entry["request_id"])
	}
}

func TestKnownUserAgentsAreConfigurable(t *testing.T) {
	dir := t.TempDir()
	fl := filelog.New(dir)
	t.Cleanup(func() {
		if err := fl.Close(); err != nil {
			t.Fatalf("close logs: %v", err)
		}
	})

	app := fiber.New()
	app.Use(New(fl, Config{
		WarnUnknownUserAgent: true,
		KnownUserAgents:      []string{"RESTClient"},
	}))
	app.Get("/ok", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	doRequest(t, app, "/ok", "Embarcadero RESTClient/1.0")

	assertNoLog(t, dir, "warning.log")
}

func TestRedactURL(t *testing.T) {
	got := RedactURL("/api?Pass=abc&x=1&token=tok", []string{"pass", "token"})
	if !strings.Contains(got, "Pass=REDACTED") || !strings.Contains(got, "token=REDACTED") {
		t.Fatalf("RedactURL did not redact configured params: %q", got)
	}
	if strings.Contains(got, "abc") || strings.Contains(got, "token=tok") {
		t.Fatalf("RedactURL leaked sensitive values: %q", got)
	}
}

func TestRedactQueryString(t *testing.T) {
	got := RedactQueryString("clave=abc&usuario=123&authorization=bearer", DefaultRedactQueryParams)
	if !strings.Contains(got, "clave=REDACTED") || !strings.Contains(got, "authorization=REDACTED") {
		t.Fatalf("RedactQueryString did not redact configured params: %q", got)
	}
	if strings.Contains(got, "abc") || strings.Contains(got, "bearer") {
		t.Fatalf("RedactQueryString leaked sensitive values: %q", got)
	}
}

func TestShouldSkipPathMatchesMountedSuffix(t *testing.T) {
	if !ShouldSkipPath("/api/health", []string{"/health"}) {
		t.Fatal("expected mounted /health path to be skipped")
	}
	if ShouldSkipPath("/api/healthz", []string{"/health"}) {
		t.Fatal("did not expect /healthz to be skipped")
	}
}

func decodeLogLine(t *testing.T, line string) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(line), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatalf("empty log")
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("decode JSON log %q: %v", lines[0], err)
	}
	return entry
}

func number(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

func doRequest(t *testing.T, app *fiber.App, target string, ua string) {
	t.Helper()
	req := httptest.NewRequest("GET", target, nil)
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s: %v", target, err)
	}
	if resp.Body != nil {
		resp.Body.Close()
	}
}

func readLog(t *testing.T, dir string, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(content)
}

func assertNoLog(t *testing.T, dir string, name string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
		t.Fatalf("expected %s to not exist, stat err=%v", name, err)
	}
}
