package auth

import "errors"

var (
	ErrMissingSource   = errors.New("auth source is nil")
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
	ErrEmptyRoleSet    = errors.New("role set is empty")
)
