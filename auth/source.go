package auth

import "github.com/gofiber/fiber/v3"

type Source interface {
	Current(fiber.Ctx) (Principal, bool, error)
}
