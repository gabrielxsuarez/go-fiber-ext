package sessionauth

import (
	"encoding/json"
	"errors"

	"github.com/gabrielxsuarez/go-fiber-ext/auth"
	"github.com/gabrielxsuarez/go-fiber-ext/sessionext"
	"github.com/gofiber/fiber/v3"
)

const DefaultPrincipalKey = "auth_principal"

type Config struct {
	PrincipalKey string
}

type Manager struct {
	principalKey string
}

func New(configs ...Config) *Manager {
	cfg := Config{}
	if len(configs) > 0 {
		cfg = configs[0]
	}

	key := cfg.PrincipalKey
	if key == "" {
		key = DefaultPrincipalKey
	}

	return &Manager{principalKey: key}
}

func (m *Manager) Current(c fiber.Ctx) (auth.Principal, bool, error) {
	raw, ok := sessionext.GetString(c, m.principalKey)
	if !ok || raw == "" {
		return auth.Principal{}, false, nil
	}

	var principal auth.Principal
	if err := json.Unmarshal([]byte(raw), &principal); err != nil {
		return auth.Principal{}, false, err
	}
	if principal.Subject == "" {
		return auth.Principal{}, false, nil
	}

	return principal, true, nil
}

func (m *Manager) Login(c fiber.Ctx, principal auth.Principal) error {
	if principal.Subject == "" {
		return errors.New("principal subject is required")
	}

	if err := sessionext.Regenerate(c); err != nil {
		return err
	}

	payload, err := json.Marshal(principal)
	if err != nil {
		return err
	}

	if err := sessionext.Set(c, m.principalKey, string(payload)); err != nil {
		return err
	}
	auth.SetCurrent(c, principal)
	return nil
}

func (m *Manager) Logout(c fiber.Ctx) error {
	return sessionext.Reset(c)
}

func (m *Manager) PrincipalKey() string {
	return m.principalKey
}
