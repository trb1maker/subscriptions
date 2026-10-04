package domain

import "errors"

var (
	ErrInvalidName           = errors.New("invalid name")
	ErrInvalidPrice          = errors.New("invalid price")
	ErrInvalidMessageLimit   = errors.New("invalid message limit")
	ErrInvalidTariffType     = errors.New("invalid tariff type")
	ErrInvalidTariffID       = errors.New("invalid tariff id")
	ErrInvalidSubscriptionID = errors.New("invalid subscription id")
	ErrInvalidIdempotencyKey = errors.New("invalid idempotency key")
	ErrInvalidArgument       = errors.New("invalid argument")
	ErrUnauthenticated       = errors.New("unauthenticated")
	ErrForbidden             = errors.New("forbidden")
	ErrNotFound              = errors.New("not found")
	ErrTariffType            = errors.New("tariff type mismatch")
	ErrOrganizationMember    = errors.New("organization member")
	ErrActiveSubscription    = errors.New("active subscription exists")
	ErrSubscriptionInactive  = errors.New("subscription inactive")
	ErrBaseTariffExists      = errors.New("base tariff exists")
	ErrIdempotencyConflict   = errors.New("idempotency conflict")
	ErrUnavailable           = errors.New("unavailable")
	ErrInternal              = errors.New("internal")
)
