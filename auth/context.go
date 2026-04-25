package auth

import "github.com/gofiber/fiber/v3"

type currentPrincipalKeyType int

const currentPrincipalKey currentPrincipalKeyType = iota

func SetCurrent(c fiber.Ctx, principal Principal) {
	fiber.StoreInContext(c, currentPrincipalKey, principal)
}

func Current(c fiber.Ctx) (Principal, bool) {
	return fiber.ValueFromContext[Principal](c, currentPrincipalKey)
}

func MustCurrent(c fiber.Ctx) Principal {
	principal, _ := Current(c)
	return principal
}
