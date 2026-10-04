package domain

import "errors"

var (
	ErrInvalidArgument     = errors.New("invalid argument")
	ErrUnauthenticated     = errors.New("unauthenticated")
	ErrNotFound            = errors.New("not found")
	ErrIdempotencyConflict = errors.New("idempotency conflict")
	ErrUnavailable         = errors.New("unavailable")
)
