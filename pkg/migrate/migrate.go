package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

const pgxDriver = "pgx/v5"

// Step — состояние одного шага миграции.
type Step struct {
	Version int64
	Source  string
	Applied bool
}

// Up применяет все ещё не применённые шаги из files.
func Up(ctx context.Context, dsn string, files fs.FS) error {
	return run(ctx, dsn, files, func(ctx context.Context, provider *goose.Provider) error {
		if _, err := provider.Up(ctx); err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}

		return nil
	})
}

// Down откатывает один последний применённый шаг.
func Down(ctx context.Context, dsn string, files fs.FS) error {
	return run(ctx, dsn, files, func(ctx context.Context, provider *goose.Provider) error {
		if _, err := provider.Down(ctx); err != nil {
			return fmt.Errorf("roll back migration: %w", err)
		}

		return nil
	})
}

// Status возвращает шаги и отметку, применён ли каждый из них.
func Status(ctx context.Context, dsn string, files fs.FS) ([]Step, error) {
	var rows []Step

	err := run(ctx, dsn, files, func(ctx context.Context, provider *goose.Provider) error {
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

func run(ctx context.Context, dsn string, files fs.FS, fn func(context.Context, *goose.Provider) error) error {
	db, err := sql.Open(pgxDriver, dsn)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}

	defer func() {
		_ = db.Close()
	}()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
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
