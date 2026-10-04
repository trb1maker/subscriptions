package app

import (
	"context"
	"uuid"
)

// LimitStatus — можно ли начать генерацию и чей остаток проверен.
type LimitStatus struct {
	Allowed   bool
	Remaining int64
	OwnerID   uuid.UUID
	OwnerKind string
}

// Usage — проверка остатка перед генерацией.
type Usage interface {
	CheckLimit(ctx context.Context) (LimitStatus, error)
}
