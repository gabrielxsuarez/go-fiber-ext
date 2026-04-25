package auth

import "github.com/gofiber/fiber/v3"

type RequireConfig struct {
	RedirectPath           string
	UnauthenticatedStatus  int
	ForbiddenStatus        int
	UnauthenticatedHandler fiber.Handler
	ForbiddenHandler       fiber.Handler
	ErrorHandler           func(fiber.Ctx, error) error
}

type RequireOption func(*RequireConfig)

func RedirectTo(path string) RequireOption {
	return func(cfg *RequireConfig) {
		cfg.RedirectPath = path
	}
}

func UnauthenticatedStatus(status int) RequireOption {
	return func(cfg *RequireConfig) {
		cfg.UnauthenticatedStatus = status
	}
}

func ForbiddenStatus(status int) RequireOption {
	return func(cfg *RequireConfig) {
		cfg.ForbiddenStatus = status
	}
}

func UnauthenticatedHandler(handler fiber.Handler) RequireOption {
	return func(cfg *RequireConfig) {
		cfg.UnauthenticatedHandler = handler
	}
}

func ForbiddenHandler(handler fiber.Handler) RequireOption {
	return func(cfg *RequireConfig) {
		cfg.ForbiddenHandler = handler
	}
}

func ErrorHandler(handler func(fiber.Ctx, error) error) RequireOption {
	return func(cfg *RequireConfig) {
		cfg.ErrorHandler = handler
	}
}

func Optional(source Source) fiber.Handler {
	return func(c fiber.Ctx) error {
		if source == nil {
			return c.Next()
		}

		principal, ok, err := source.Current(c)
		if err != nil {
			return err
		}
		if ok {
			SetCurrent(c, principal)
		}
		return c.Next()
	}
}

func Require(source Source, options ...RequireOption) fiber.Handler {
	cfg := requireConfig(options...)

	return func(c fiber.Ctx) error {
		principal, ok, err := authenticate(c, source)
		if err != nil || !ok {
			return handleAuthFailure(c, cfg, err)
		}

		SetCurrent(c, principal)
		return c.Next()
	}
}

func RequireRole(source Source, role string, options ...RequireOption) fiber.Handler {
	return RequireAnyRole(source, []string{role}, options...)
}

func RequireAnyRole(source Source, roles []string, options ...RequireOption) fiber.Handler {
	cfg := requireConfig(options...)

	return func(c fiber.Ctx) error {
		if len(roles) == 0 {
			return handleError(c, cfg, ErrEmptyRoleSet)
		}

		principal, ok, err := authenticate(c, source)
		if err != nil || !ok {
			return handleAuthFailure(c, cfg, err)
		}
		if !principal.HasAnyRole(roles...) {
			return handleForbidden(c, cfg)
		}

		SetCurrent(c, principal)
		return c.Next()
	}
}

func authenticate(c fiber.Ctx, source Source) (Principal, bool, error) {
	if source == nil {
		return Principal{}, false, ErrMissingSource
	}

	principal, ok, err := source.Current(c)
	if err != nil {
		return Principal{}, false, err
	}
	return principal, ok, nil
}

func handleAuthFailure(c fiber.Ctx, cfg RequireConfig, err error) error {
	if err != nil {
		return handleError(c, cfg, err)
	}
	return handleUnauthenticated(c, cfg)
}

func handleError(c fiber.Ctx, cfg RequireConfig, err error) error {
	if cfg.ErrorHandler != nil {
		return cfg.ErrorHandler(c, err)
	}
	return err
}

func handleUnauthenticated(c fiber.Ctx, cfg RequireConfig) error {
	if cfg.UnauthenticatedHandler != nil {
		return cfg.UnauthenticatedHandler(c)
	}
	if cfg.RedirectPath != "" {
		return c.Redirect().To(cfg.RedirectPath)
	}
	return c.Status(cfg.UnauthenticatedStatus).SendString(ErrUnauthenticated.Error())
}

func handleForbidden(c fiber.Ctx, cfg RequireConfig) error {
	if cfg.ForbiddenHandler != nil {
		return cfg.ForbiddenHandler(c)
	}
	return c.Status(cfg.ForbiddenStatus).SendString(ErrForbidden.Error())
}

func requireConfig(options ...RequireOption) RequireConfig {
	cfg := RequireConfig{
		UnauthenticatedStatus: fiber.StatusUnauthorized,
		ForbiddenStatus:       fiber.StatusForbidden,
	}

	for _, option := range options {
		if option != nil {
			option(&cfg)
		}
	}

	if cfg.UnauthenticatedStatus == 0 {
		cfg.UnauthenticatedStatus = fiber.StatusUnauthorized
	}
	if cfg.ForbiddenStatus == 0 {
		cfg.ForbiddenStatus = fiber.StatusForbidden
	}

	return cfg
}
