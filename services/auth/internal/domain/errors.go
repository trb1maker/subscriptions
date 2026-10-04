package domain

import "errors"

var (
	ErrInvalidEmail          = errors.New("invalid email")
	ErrInvalidPassword       = errors.New("invalid password")
	ErrInvalidName           = errors.New("invalid name")
	ErrInvalidIdempotencyKey = errors.New("invalid idempotency key")
	ErrEmailTaken            = errors.New("email taken")
	ErrInvalidCredentials    = errors.New("invalid credentials")
	ErrIdempotencyConflict   = errors.New("idempotency conflict")
	ErrOrganizationNotFound  = errors.New("organization not found")
	ErrInvalidOrganizationID = errors.New("invalid organization id")
	ErrNotFound              = errors.New("not found")
	ErrInvalidToken          = errors.New("invalid token")
)
