package domain

import "errors"

var (
	ErrInvalidArgument   = errors.New("invalid argument")
	ErrUnauthenticated   = errors.New("unauthenticated")
	ErrInvalidWebhookKey = errors.New("invalid webhook key")
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("conflict")
	ErrUnavailable       = errors.New("unavailable")
	ErrInternal          = errors.New("internal")
	ErrForbidden         = errors.New("forbidden")
)
