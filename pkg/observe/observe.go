package observe

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/trb1maker/subscriptions/pkg/health"
	"github.com/trb1maker/subscriptions/pkg/logger"
	"github.com/trb1maker/subscriptions/pkg/metrics"
	"github.com/trb1maker/subscriptions/pkg/telemetry"
)

const healthEvery = 5 * time.Second

// Config — имя сервиса, уровень логов и адрес OTLP.
type Config struct {
	Service  string
	Level    string
	Endpoint string
}

// App — логгер stdout и реестр метрик одного процесса.
type App struct {
	Log     *slog.Logger
	Metrics *metrics.Metrics
	diag    *slog.Logger
	stop    func(context.Context) error
}

// Start пишет JSON в stdout и поднимает трассировку.
// Сбор логов в ClickHouse делает Vector, процесс туда не подключается.
func Start(ctx context.Context, cfg Config) (*App, error) {
	diag, err := logger.New(os.Stderr, "error")
	if err != nil {
		return nil, fmt.Errorf("diagnostic logger: %w", err)
	}

	log, err := logger.New(os.Stdout, cfg.Level)
	if err != nil {
		return nil, fmt.Errorf("logger: %w", err)
	}

	stop, err := telemetry.Start(ctx, diag, cfg.Service, cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("start telemetry: %w", err)
	}

	return &App{
		Log:     log.With("service", cfg.Service),
		Metrics: metrics.New(cfg.Service),
		diag:    diag,
		stop:    stop,
	}, nil
}

// Shutdown сбрасывает батч трейсов.
func (a *App) Shutdown(ctx context.Context) {
	if a == nil || a.stop == nil {
		return
	}

	if err := a.stop(ctx); err != nil {
		a.diag.ErrorContext(ctx, "otlp shutdown failed", "error", err)
	}
}

// Watch обновляет service_healthy по тем же проверкам, что и GET /health.
func (a *App) Watch(ctx context.Context, checks ...health.Check) {
	a.Metrics.WatchHealth(ctx, healthEvery, checks...)
}
