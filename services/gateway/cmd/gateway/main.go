package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/trb1maker/subscriptions/pkg/httpserver"
	"github.com/trb1maker/subscriptions/pkg/logger"
	httpapi "github.com/trb1maker/subscriptions/services/gateway/internal/adapters/http"
)

const (
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
)

type config struct {
	HTTPAddr string `env:"HTTP_ADDR" envDefault:":8080"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
}

func main() {
	os.Exit(run())
}

func run() int {
	var cfg config
	if err := env.Parse(&cfg); err != nil {
		log, logErr := logger.New(os.Stderr, "info")
		if logErr != nil {
			return 1
		}

		log.ErrorContext(context.Background(), "config load failed", "error", err)
		return 1
	}

	log, err := logger.New(os.Stdout, cfg.LogLevel)
	if err != nil {
		fallback, fallbackErr := logger.New(os.Stderr, "info")
		if fallbackErr != nil {
			return 1
		}

		fallback.ErrorContext(context.Background(), "logger init failed", "error", err)
		return 1
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewRouter(log),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := httpserver.Serve(ctx, log, server, shutdownTimeout); err != nil {
		return 1
	}

	return 0
}
