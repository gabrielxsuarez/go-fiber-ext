package requestlog

import (
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
	if !strings.Contains(access, "| 200") {
		t.Fatalf("access log does not include status: %q", access)
	}
	if !strings.Contains(access, "pass=REDACTED") || !strings.Contains(access, "token=REDACTED") {
		t.Fatalf("access log does not redact sensitive query params: %q", access)
	}
	if strings.Contains(access, "abc123") || strings.Contains(access, "tok456") {
		t.Fatalf("access log leaked sensitive query values: %q", access)
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
	if !strings.Contains(warning, "/not-found") || !strings.Contains(warning, "| 404") {
		t.Fatalf("warning log missing 4xx request: %q", warning)
	}

	errorLog := readLog(t, dir, "error.log")
	if !strings.Contains(errorLog, "/boom") || !strings.Contains(errorLog, "| 503") {
		t.Fatalf("error log missing 5xx request: %q", errorLog)
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
	if !strings.Contains(warning, "/ok") || !strings.Contains(warning, "CustomBot/1.0") {
		t.Fatalf("warning log missing unknown user-agent request: %q", warning)
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

func TestShouldSkipPathMatchesMountedSuffix(t *testing.T) {
	if !ShouldSkipPath("/api/health", []string{"/health"}) {
		t.Fatal("expected mounted /health path to be skipped")
	}
	if ShouldSkipPath("/api/healthz", []string{"/health"}) {
		t.Fatal("did not expect /healthz to be skipped")
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
