package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

const (
	pgxDriver        = "pgx/v5"
	clickHouseDriver = "clickhouse"
)

type engine struct {
	dialect goose.Dialect
	driver  string
	name    string
}

var (
	postgresEngine = engine{dialect: goose.DialectPostgres, driver: pgxDriver, name: "postgres"}
	clickEngine    = engine{dialect: goose.DialectClickHouse, driver: clickHouseDriver, name: "clickhouse"}
)

// Step — состояние одного шага миграции.
type Step struct {
	Version int64
	Source  string
	Applied bool
}

// Up применяет все ещё не применённые шаги Postgres из files.
func Up(ctx context.Context, dsn string, files fs.FS) error {
	return up(ctx, postgresEngine, dsn, files)
}

// Down откатывает один последний применённый шаг Postgres.
func Down(ctx context.Context, dsn string, files fs.FS) error {
	return down(ctx, postgresEngine, dsn, files)
}

// Status возвращает шаги Postgres и отметку, применён ли каждый из них.
func Status(ctx context.Context, dsn string, files fs.FS) ([]Step, error) {
	return status(ctx, postgresEngine, dsn, files)
}

// UpClickHouse применяет все ещё не применённые шаги ClickHouse из files.
func UpClickHouse(ctx context.Context, dsn string, files fs.FS) error {
	return up(ctx, clickEngine, dsn, files)
}

// DownClickHouse откатывает один последний применённый шаг ClickHouse.
func DownClickHouse(ctx context.Context, dsn string, files fs.FS) error {
	return down(ctx, clickEngine, dsn, files)
}

// StatusClickHouse возвращает шаги ClickHouse и отметку, применён ли каждый из них.
func StatusClickHouse(ctx context.Context, dsn string, files fs.FS) ([]Step, error) {
	return status(ctx, clickEngine, dsn, files)
}

func up(ctx context.Context, db engine, dsn string, files fs.FS) error {
	return run(ctx, db, dsn, files, func(ctx context.Context, provider *goose.Provider) error {
		if _, err := provider.Up(ctx); err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}

		return nil
	})
}

func down(ctx context.Context, db engine, dsn string, files fs.FS) error {
	return run(ctx, db, dsn, files, func(ctx context.Context, provider *goose.Provider) error {
		if _, err := provider.Down(ctx); err != nil {
			return fmt.Errorf("roll back migration: %w", err)
		}

		return nil
	})
}

func status(ctx context.Context, db engine, dsn string, files fs.FS) ([]Step, error) {
	var rows []Step

	err := run(ctx, db, dsn, files, func(ctx context.Context, provider *goose.Provider) error {
		listed, err := provider.Status(ctx)
		if err != nil {
			return fmt.Errorf("read migration status: %w", err)
		}

		rows = make([]Step, 0, len(listed))
		for _, item := range listed {
			source := ""
			version := int64(0)
			if item.Source != nil {
				source = item.Source.Path
				version = item.Source.Version
			}

			rows = append(rows, Step{
				Version: version,
				Source:  source,
				Applied: item.State == goose.StateApplied,
			})
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return rows, nil
}

func run(ctx context.Context, db engine, dsn string, files fs.FS, fn func(context.Context, *goose.Provider) error) error {
	conn, err := sql.Open(db.driver, dsn)
	if err != nil {
		return fmt.Errorf("open %s: %w", db.name, err)
	}

	defer func() {
		_ = conn.Close()
	}()

	if err := conn.PingContext(ctx); err != nil {
		return fmt.Errorf("ping %s: %w", db.name, err)
	}

	provider, err := goose.NewProvider(
		db.dialect,
		conn,
		files,
		goose.WithDisableGlobalRegistry(true),
		goose.WithSlog(slog.New(slog.DiscardHandler)),
	)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	if err := fn(ctx, provider); err != nil {
		return err
	}

	return nil
}
