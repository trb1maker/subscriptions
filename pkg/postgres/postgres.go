package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/trb1maker/subscriptions/pkg/health"
)

// NewPool открывает пул и проверяет, что Postgres принимает запросы.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}

// Check — проверка Postgres для GET /health.
func Check(pool *pgxpool.Pool) health.Check {
	return health.Check{
		Name: "postgres",
		Fn:   pool.Ping,
	}
}
