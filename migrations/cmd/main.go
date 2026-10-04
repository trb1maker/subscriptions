package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	authmigrations "github.com/trb1maker/subscriptions/migrations/auth"
	"github.com/trb1maker/subscriptions/pkg/logger"
	"github.com/trb1maker/subscriptions/pkg/migrate"
)

const (
	usageHint = "migrate <service> <up|down|status>"
	argCount  = 2
	exitUsage = 2
)

type migrationSet struct {
	files fs.FS
	env   string
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
		err = migrate.Up(ctx, dsn, set.files)
	case "down":
		err = migrate.Down(ctx, dsn, set.files)
	case "status":
		err = printStatus(ctx, log, dsn, set.files)
	}
	if err != nil {
		log.ErrorContext(ctx, "migration failed", "error", err)

		return 1
	}

	return 0
}

func printStatus(ctx context.Context, log *slog.Logger, dsn string, files fs.FS) error {
	rows, err := migrate.Status(ctx, dsn, files)
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
		"auth": {files: authmigrations.FS, env: "AUTH_DATABASE_URL"},
	}
}
