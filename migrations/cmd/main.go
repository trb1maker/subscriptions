package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	authmigrations "github.com/trb1maker/subscriptions/migrations/auth"
	subscriptionsmigrations "github.com/trb1maker/subscriptions/migrations/subscriptions"
	usagemigrations "github.com/trb1maker/subscriptions/migrations/usage"
	"github.com/trb1maker/subscriptions/pkg/logger"
	"github.com/trb1maker/subscriptions/pkg/migrate"
)

const (
	usageHint = "migrate <service> <up|down|status>"
	argCount  = 2
	exitUsage = 2
)

type migrationSet struct {
	files      fs.FS
	env        string
	clickHouse bool
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	log, err := logger.New(os.Stderr, "info")
	if err != nil {
		return 1
	}

	ctx := context.Background()
	if len(args) != argCount {
		log.ErrorContext(ctx, "usage", "hint", usageHint)

		return exitUsage
	}

	set, ok := services()[args[0]]
	if !ok || !knownCommand(args[1]) {
		log.ErrorContext(ctx, "usage", "hint", usageHint)

		return exitUsage
	}

	dsn := os.Getenv(set.env)
	if dsn == "" {
		log.ErrorContext(ctx, "database url is empty", "env", set.env)

		return 1
	}

	switch args[1] {
	case "up":
		if set.clickHouse {
			err = migrate.UpClickHouse(ctx, dsn, set.files)
		} else {
			err = migrate.Up(ctx, dsn, set.files)
		}
	case "down":
		if set.clickHouse {
			err = migrate.DownClickHouse(ctx, dsn, set.files)
		} else {
			err = migrate.Down(ctx, dsn, set.files)
		}
	case "status":
		err = printStatus(ctx, log, dsn, set)
	}
	if err != nil {
		log.ErrorContext(ctx, "migration failed", "error", err)

		return 1
	}

	return 0
}

func printStatus(ctx context.Context, log *slog.Logger, dsn string, set migrationSet) error {
	var (
		rows []migrate.Step
		err  error
	)
	if set.clickHouse {
		rows, err = migrate.StatusClickHouse(ctx, dsn, set.files)
	} else {
		rows, err = migrate.Status(ctx, dsn, set.files)
	}
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}

	for _, row := range rows {
		log.InfoContext(ctx, "migration status", "version", row.Version, "source", row.Source, "applied", row.Applied)
	}

	return nil
}

func knownCommand(command string) bool {
	switch command {
	case "up", "down", "status":
		return true
	default:
		return false
	}
}

func services() map[string]migrationSet {
	return map[string]migrationSet{
		"auth":          {files: authmigrations.FS, env: "AUTH_DATABASE_URL"},
		"subscriptions": {files: subscriptionsmigrations.FS, env: "SUBSCRIPTIONS_DATABASE_URL"},
		"usage":         {files: usagemigrations.FS, env: "USAGE_CLICKHOUSE_DSN", clickHouse: true},
	}
}
