package sessionext

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	fibersession "github.com/gofiber/fiber/v3/middleware/session"
)

var ErrUnavailable = errors.New("session middleware is not available")

func FromContext(c fiber.Ctx) (*fibersession.Middleware, bool) {
	sess := fibersession.FromContext(c)
	return sess, sess != nil
}

func Must(c fiber.Ctx) (*fibersession.Middleware, error) {
	sess, ok := FromContext(c)
	if !ok {
		return nil, ErrUnavailable
	}
	return sess, nil
}

func Get(c fiber.Ctx, key any) any {
	sess, ok := FromContext(c)
	if !ok {
		return nil
	}
	return sess.Get(key)
}

func GetString(c fiber.Ctx, key any) (string, bool) {
	value := Get(c, key)
	if value == nil {
		return "", false
	}
	stringValue, ok := value.(string)
	return stringValue, ok
}

func Set(c fiber.Ctx, key, value any) error {
	sess, err := Must(c)
	if err != nil {
		return err
	}
	sess.Set(key, value)
	return nil
}

func Delete(c fiber.Ctx, key any) error {
	sess, err := Must(c)
	if err != nil {
		return err
	}
	sess.Delete(key)
	return nil
}

func Regenerate(c fiber.Ctx) error {
	sess, err := Must(c)
	if err != nil {
		return err
	}
	return sess.Regenerate()
}

func Reset(c fiber.Ctx) error {
	sess, err := Must(c)
	if err != nil {
		return err
	}
	return sess.Reset()
}

func Destroy(c fiber.Ctx) error {
	sess, err := Must(c)
	if err != nil {
		return err
	}
	return sess.Destroy()
}

func ID(c fiber.Ctx) (string, bool) {
	sess, ok := FromContext(c)
	if !ok {
		return "", false
	}
	return sess.ID(), true
}
