package sessionext

import (
	"github.com/gofiber/fiber/v3"
	fibersession "github.com/gofiber/fiber/v3/middleware/session"
)

func New(configs ...Config) fiber.Handler {
	return fibersession.New(FiberConfig(configs...))
}

func NewWithStore(configs ...Config) (fiber.Handler, *fibersession.Store) {
	return fibersession.NewWithStore(FiberConfig(configs...))
}
