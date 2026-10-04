package clickhouse

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/trb1maker/subscriptions/pkg/health"
)

const driver = "clickhouse"

// Open открывает соединение и проверяет, что ClickHouse принимает запросы.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}

	return db, nil
}

// Check — проверка ClickHouse для GET /health.
func Check(db *sql.DB) health.Check {
	return health.Check{
		Name: "clickhouse",
		Fn:   db.PingContext,
	}
}
