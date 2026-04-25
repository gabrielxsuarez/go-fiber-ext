package sessionext

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
)

func TestDefaultCookieName(t *testing.T) {
	tests := []struct {
		appName string
		want    string
	}{
		{appName: "appadmin", want: "appadmin_session"},
		{appName: "My App", want: "my_app_session"},
		{appName: "app.admin", want: "app_admin_session"},
		{appName: "", want: "session"},
	}

	for _, tt := range tests {
		t.Run(tt.appName, func(t *testing.T) {
			if got := DefaultCookieName(tt.appName); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestFiberConfigDefaults(t *testing.T) {
	cfg := FiberConfig(Config{
		AppName:     "My App",
		Development: true,
	})

	if cfg.CookiePath != DefaultCookiePath {
		t.Fatalf("expected cookie path %q, got %q", DefaultCookiePath, cfg.CookiePath)
	}
	if cfg.CookieSameSite != fiber.CookieSameSiteLaxMode {
		t.Fatalf("expected SameSite=Lax, got %q", cfg.CookieSameSite)
	}
	if cfg.CookieSecure {
		t.Fatalf("expected insecure cookie in development")
	}
	if !cfg.CookieHTTPOnly {
		t.Fatalf("expected HttpOnly cookie")
	}
	if cfg.Extractor.Source != extractors.SourceCookie || cfg.Extractor.Key != "my_app_session" {
		t.Fatalf("expected cookie extractor my_app_session, got source=%d key=%q", cfg.Extractor.Source, cfg.Extractor.Key)
	}
}

func TestFiberConfigUsesSecureCookiesOutsideDevelopment(t *testing.T) {
	cfg := FiberConfig(Config{AppName: "appadmin"})
	if !cfg.CookieSecure {
		t.Fatalf("expected secure cookie outside development")
	}
}

func TestFiberConfigAllowsCookieSecureOverride(t *testing.T) {
	cfg := FiberConfig(Config{
		AppName:      "appadmin",
		CookieSecure: Bool(false),
	})
	if cfg.CookieSecure {
		t.Fatalf("expected cookie secure override to be respected")
	}
}

func TestMiddlewareSetsNamedCookie(t *testing.T) {
	app := fiber.New()
	app.Use(New(Config{
		AppName:     "My App",
		Development: true,
	}))
	app.Get("/", func(c fiber.Ctx) error {
		if err := Set(c, "value", "ok"); err != nil {
			return err
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	resp := performRequest(t, app, http.MethodGet, "/")
	defer resp.Body.Close()

	cookie := findCookie(resp.Cookies(), "my_app_session")
	if cookie == nil {
		t.Fatalf("expected my_app_session cookie")
	}
	if !cookie.HttpOnly {
		t.Fatalf("expected HttpOnly cookie")
	}
	if cookie.Secure {
		t.Fatalf("expected non-secure cookie in development")
	}

	setCookie := resp.Header.Get("Set-Cookie")
	if !strings.Contains(setCookie, "SameSite=Lax") {
		t.Fatalf("expected SameSite=Lax in Set-Cookie, got %q", setCookie)
	}
}

func performRequest(t *testing.T, app *fiber.App, method, target string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(method, target, nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app test: %v", err)
	}
	return resp
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}
