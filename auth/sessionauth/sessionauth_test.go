package sessionauth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gabrielxsuarez/go-fiber-ext/auth"
	"github.com/gabrielxsuarez/go-fiber-ext/sessionext"
	"github.com/gofiber/fiber/v3"
)

func TestLoginAndRequireRole(t *testing.T) {
	app, manager := newTestApp()
	app.Post("/login", func(c fiber.Ctx) error {
		return manager.Login(c, auth.Principal{
			Subject: "42",
			Name:    "Ada",
			Roles:   []string{"admin"},
			Data:    map[string]string{"tenant": "main"},
		})
	})
	app.Get("/admin", auth.RequireRole(manager, "admin"), func(c fiber.Ctx) error {
		principal := auth.MustCurrent(c)
		return c.SendString(principal.Data["tenant"])
	})

	loginResp := performRequest(t, app, http.MethodPost, "/login", nil)
	loginResp.Body.Close()

	adminResp := performRequest(t, app, http.MethodGet, "/admin", loginResp.Cookies())
	defer adminResp.Body.Close()

	body := readBody(t, adminResp)
	if adminResp.StatusCode != fiber.StatusOK || body != "main" {
		t.Fatalf("expected authenticated admin request, got status=%d body=%q", adminResp.StatusCode, body)
	}
}

func TestLogoutClearsPrincipal(t *testing.T) {
	app, manager := newTestApp()
	app.Post("/login", func(c fiber.Ctx) error {
		return manager.Login(c, auth.Principal{Subject: "42"})
	})
	app.Post("/logout", func(c fiber.Ctx) error {
		if err := manager.Logout(c); err != nil {
			return err
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/private", auth.Require(manager), func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	loginResp := performRequest(t, app, http.MethodPost, "/login", nil)
	loginResp.Body.Close()

	logoutResp := performRequest(t, app, http.MethodPost, "/logout", loginResp.Cookies())
	logoutResp.Body.Close()

	privateResp := performRequest(t, app, http.MethodGet, "/private", logoutResp.Cookies())
	defer privateResp.Body.Close()

	if privateResp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d", privateResp.StatusCode)
	}
}

func TestLoginRegeneratesSessionID(t *testing.T) {
	app, manager := newTestApp()
	app.Get("/touch", func(c fiber.Ctx) error {
		if err := sessionext.Set(c, "cart", "preserved"); err != nil {
			return err
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Post("/login", func(c fiber.Ctx) error {
		return manager.Login(c, auth.Principal{Subject: "42"})
	})

	touchResp := performRequest(t, app, http.MethodGet, "/touch", nil)
	touchResp.Body.Close()
	touchCookie := findCookie(touchResp.Cookies(), "test_session")
	if touchCookie == nil {
		t.Fatalf("expected anonymous session cookie")
	}

	loginResp := performRequest(t, app, http.MethodPost, "/login", []*http.Cookie{touchCookie})
	loginResp.Body.Close()
	loginCookie := findCookie(loginResp.Cookies(), "test_session")
	if loginCookie == nil {
		t.Fatalf("expected login session cookie")
	}
	if loginCookie.Value == touchCookie.Value {
		t.Fatalf("expected login to regenerate session id")
	}
}

func newTestApp() (*fiber.App, *Manager) {
	app := fiber.New()
	app.Use(sessionext.New(sessionext.Config{
		AppName:     "test",
		Development: true,
	}))
	return app, New()
}

func performRequest(t *testing.T, app *fiber.App, method, target string, cookies []*http.Cookie) *http.Response {
	t.Helper()

	req := httptest.NewRequest(method, target, nil)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

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

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}
