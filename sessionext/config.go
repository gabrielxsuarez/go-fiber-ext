package sessionext

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
	fibersession "github.com/gofiber/fiber/v3/middleware/session"
)

const (
	DefaultIdleTimeout    = 30 * time.Minute
	DefaultCookiePath     = "/"
	DefaultCookieSameSite = fiber.CookieSameSiteLaxMode
)

type Config struct {
	AppName           string
	CookieName        string
	Development       bool
	CookieSecure      *bool
	CookieHTTPOnly    *bool
	CookieDomain      string
	CookiePath        string
	CookieSameSite    string
	CookieSessionOnly bool
	IdleTimeout       time.Duration
	AbsoluteTimeout   time.Duration
	Storage           fiber.Storage
	Store             *fibersession.Store
	Extractor         extractors.Extractor
	KeyGenerator      func() string
	Next              func(fiber.Ctx) bool
	ErrorHandler      func(fiber.Ctx, error)
}

func Bool(value bool) *bool {
	return &value
}

func DefaultCookieName(appName string) string {
	name := sanitizeAppName(appName)
	if name == "" {
		return "session"
	}
	return name + "_session"
}

func FiberConfig(configs ...Config) fibersession.Config {
	cfg := Config{}
	if len(configs) > 0 {
		cfg = configs[0]
	}

	cookieName := cfg.CookieName
	if cookieName == "" {
		cookieName = DefaultCookieName(cfg.AppName)
	}

	cookiePath := cfg.CookiePath
	if cookiePath == "" {
		cookiePath = DefaultCookiePath
	}

	cookieSameSite := cfg.CookieSameSite
	if cookieSameSite == "" {
		cookieSameSite = DefaultCookieSameSite
	}

	idleTimeout := cfg.IdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = DefaultIdleTimeout
	}

	cookieSecure := !cfg.Development
	if cfg.CookieSecure != nil {
		cookieSecure = *cfg.CookieSecure
	}

	cookieHTTPOnly := true
	if cfg.CookieHTTPOnly != nil {
		cookieHTTPOnly = *cfg.CookieHTTPOnly
	}

	extractor := cfg.Extractor
	if extractor.Extract == nil {
		extractor = extractors.FromCookie(cookieName)
	}

	return fibersession.Config{
		Storage:           cfg.Storage,
		Store:             cfg.Store,
		Next:              cfg.Next,
		ErrorHandler:      cfg.ErrorHandler,
		KeyGenerator:      cfg.KeyGenerator,
		CookieDomain:      cfg.CookieDomain,
		CookiePath:        cookiePath,
		CookieSameSite:    cookieSameSite,
		Extractor:         extractor,
		IdleTimeout:       idleTimeout,
		AbsoluteTimeout:   cfg.AbsoluteTimeout,
		CookieSecure:      cookieSecure,
		CookieHTTPOnly:    cookieHTTPOnly,
		CookieSessionOnly: cfg.CookieSessionOnly,
	}
}

func sanitizeAppName(appName string) string {
	appName = strings.ToLower(strings.TrimSpace(appName))
	var builder strings.Builder
	lastUnderscore := false

	for _, r := range appName {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastUnderscore = false
			continue
		}

		if builder.Len() > 0 && !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}

	return strings.Trim(builder.String(), "_")
}
