package auth

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

type sourceFunc func(fiber.Ctx) (Principal, bool, error)

func (fn sourceFunc) Current(c fiber.Ctx) (Principal, bool, error) {
	return fn(c)
}

func TestRequireStoresCurrentPrincipal(t *testing.T) {
	app := fiber.New()
	app.Get("/private", Require(sourceFunc(func(fiber.Ctx) (Principal, bool, error) {
		return Principal{Subject: "42", Name: "Ada"}, true, nil
	})), func(c fiber.Ctx) error {
		principal, ok := Current(c)
		if !ok {
			t.Fatalf("expected current principal")
		}
		return c.SendString(principal.Subject)
	})

	resp := performRequest(t, app, http.MethodGet, "/private")
	defer resp.Body.Close()

	body := readBody(t, resp)
	if resp.StatusCode != fiber.StatusOK || body != "42" {
		t.Fatalf("expected 200 with subject, got status=%d body=%q", resp.StatusCode, body)
	}
}

func TestRequireRedirectsWhenUnauthenticated(t *testing.T) {
	app := fiber.New()
	app.Get("/private", Require(sourceFunc(func(fiber.Ctx) (Principal, bool, error) {
		return Principal{}, false, nil
	}), RedirectTo("/login")), func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	resp := performRequest(t, app, http.MethodGet, "/private")
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("expected redirect, got status %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/login" {
		t.Fatalf("expected Location /login, got %q", got)
	}
}

func TestRequireAnyRoleForbidsMissingRole(t *testing.T) {
	app := fiber.New()
	app.Get("/admin", RequireAnyRole(sourceFunc(func(fiber.Ctx) (Principal, bool, error) {
		return Principal{Subject: "42", Roles: []string{"operator"}}, true, nil
	}), []string{"admin"}), func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	resp := performRequest(t, app, http.MethodGet, "/admin")
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestRequireUsesErrorHandler(t *testing.T) {
	sourceErr := errors.New("source failed")
	app := fiber.New()
	app.Get("/private", Require(sourceFunc(func(fiber.Ctx) (Principal, bool, error) {
		return Principal{}, false, sourceErr
	}), ErrorHandler(func(c fiber.Ctx, err error) error {
		if !errors.Is(err, sourceErr) {
			t.Fatalf("expected source error, got %v", err)
		}
		return c.SendStatus(fiber.StatusTeapot)
	})), func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	resp := performRequest(t, app, http.MethodGet, "/private")
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusTeapot {
		t.Fatalf("expected 418, got %d", resp.StatusCode)
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

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}
