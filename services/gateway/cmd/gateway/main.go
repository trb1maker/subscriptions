package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"google.golang.org/grpc"

	authv1 "github.com/trb1maker/subscriptions/api/gen/auth/v1"
	"github.com/trb1maker/subscriptions/pkg/grpcclient"
	"github.com/trb1maker/subscriptions/pkg/httpserver"
	"github.com/trb1maker/subscriptions/pkg/logger"
	"github.com/trb1maker/subscriptions/pkg/mtls"
	authadapter "github.com/trb1maker/subscriptions/services/gateway/internal/adapters/auth"
	httpapi "github.com/trb1maker/subscriptions/services/gateway/internal/adapters/http"
)

const (
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
)

type config struct {
	HTTPAddr           string `env:"HTTP_ADDR"             envDefault:":8080"`
	LogLevel           string `env:"LOG_LEVEL"             envDefault:"info"`
	AuthGRPCAddr       string `env:"AUTH_GRPC_ADDR,required"`
	AuthGRPCServerName string `env:"AUTH_GRPC_SERVER_NAME" envDefault:"localhost"`
	WebhookKey         string `env:"WEBHOOK_KEY,required"`
	TLSCertFile        string `env:"TLS_CERT_FILE,required"`
	TLSKeyFile         string `env:"TLS_KEY_FILE,required"`
	TLSCAFile          string `env:"TLS_CA_FILE,required"`
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

	if cfg.WebhookKey == "" {
		log.ErrorContext(context.Background(), "config load failed", "error", errors.New("empty webhook key"))

		return 1
	}

	tlsCfg, err := mtls.ClientConfig(mtls.Files{
		CertFile: cfg.TLSCertFile,
		KeyFile:  cfg.TLSKeyFile,
		CAFile:   cfg.TLSCAFile,
	}, cfg.AuthGRPCServerName)
	if err != nil {
		log.ErrorContext(context.Background(), "tls init failed", "error", err)

		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn, err := grpcclient.Dial(ctx, cfg.AuthGRPCAddr, tlsCfg)
	if err != nil {
		log.ErrorContext(ctx, "auth client init failed", "error", err)

		return 1
	}
	defer closeClient(ctx, log, conn)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewRouter(log, authadapter.NewClient(authv1.NewAuthServiceClient(conn), log), cfg.WebhookKey),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	if err := httpserver.Serve(ctx, log, server, shutdownTimeout); err != nil {
		return 1
	}

	return 0
}

func closeClient(ctx context.Context, log *slog.Logger, conn *grpc.ClientConn) {
	if err := conn.Close(); err != nil {
		log.ErrorContext(context.WithoutCancel(ctx), "grpc client close failed", "error", err)
	}
}
