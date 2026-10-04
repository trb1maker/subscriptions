package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	natsio "github.com/nats-io/nats.go"
	goredis "github.com/redis/go-redis/v9"
	"google.golang.org/grpc"

	authv1 "github.com/trb1maker/subscriptions/api/gen/auth/v1"
	usagev1 "github.com/trb1maker/subscriptions/api/gen/usage/v1"
	"github.com/trb1maker/subscriptions/pkg/clickhouse"
	"github.com/trb1maker/subscriptions/pkg/grpcclient"
	"github.com/trb1maker/subscriptions/pkg/grpcserver"
	"github.com/trb1maker/subscriptions/pkg/httpserver"
	"github.com/trb1maker/subscriptions/pkg/logger"
	"github.com/trb1maker/subscriptions/pkg/mtls"
	natspkg "github.com/trb1maker/subscriptions/pkg/nats"
	redispkg "github.com/trb1maker/subscriptions/pkg/redis"
	authadapter "github.com/trb1maker/subscriptions/services/usage/internal/adapters/auth"
	clickstore "github.com/trb1maker/subscriptions/services/usage/internal/adapters/clickhouse"
	grpcapi "github.com/trb1maker/subscriptions/services/usage/internal/adapters/grpc"
	httpapi "github.com/trb1maker/subscriptions/services/usage/internal/adapters/http"
	natsadapter "github.com/trb1maker/subscriptions/services/usage/internal/adapters/nats"
	redisstore "github.com/trb1maker/subscriptions/services/usage/internal/adapters/redis"
	"github.com/trb1maker/subscriptions/services/usage/internal/app"
)

const (
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
	workers           = 3
)

type config struct {
	HTTPAddr           string `env:"HTTP_ADDR"             envDefault:":8083"`
	GRPCAddr           string `env:"GRPC_ADDR"             envDefault:":9093"`
	LogLevel           string `env:"LOG_LEVEL"             envDefault:"info"`
	RedisURL           string `env:"USAGE_REDIS_URL,required"`
	ClickHouseDSN      string `env:"USAGE_CLICKHOUSE_DSN,required"`
	NATSURL            string `env:"USAGE_NATS_URL,required"`
	AuthGRPCAddr       string `env:"AUTH_GRPC_ADDR,required"`
	AuthGRPCServerName string `env:"AUTH_GRPC_SERVER_NAME" envDefault:"localhost"`
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	redisClient, err := redispkg.New(ctx, cfg.RedisURL)
	if err != nil {
		log.ErrorContext(ctx, "redis init failed", "error", err)

		return 1
	}
	defer closeRedis(ctx, log, redisClient)

	clickDB, err := clickhouse.Open(ctx, cfg.ClickHouseDSN)
	if err != nil {
		log.ErrorContext(ctx, "clickhouse init failed", "error", err)

		return 1
	}
	defer closeClickHouse(ctx, log, clickDB)

	natsConn, jetStream, err := natspkg.Connect(ctx, cfg.NATSURL)
	if err != nil {
		log.ErrorContext(ctx, "nats init failed", "error", err)

		return 1
	}
	defer closeNATS(ctx, log, natsConn)

	tlsFiles := mtls.Files{CertFile: cfg.TLSCertFile, KeyFile: cfg.TLSKeyFile, CAFile: cfg.TLSCAFile}
	clientTLS, err := mtls.ClientConfig(tlsFiles, cfg.AuthGRPCServerName)
	if err != nil {
		log.ErrorContext(ctx, "tls init failed", "error", err)

		return 1
	}

	authConn, err := grpcclient.Dial(ctx, cfg.AuthGRPCAddr, clientTLS)
	if err != nil {
		log.ErrorContext(ctx, "auth client init failed", "error", err)

		return 1
	}
	defer closeClient(ctx, log, authConn)

	service, err := app.New(
		clickstore.NewStore(clickDB),
		redisstore.NewStore(redisClient),
		authadapter.NewClient(authv1.NewAuthServiceClient(authConn), log),
	)
	if err != nil {
		log.ErrorContext(ctx, "usage service init failed", "error", err)

		return 1
	}

	if err := service.Restore(ctx); err != nil {
		log.ErrorContext(ctx, "restore limits failed", "error", err)

		return 1
	}

	serverTLS, err := mtls.ServerConfig(tlsFiles)
	if err != nil {
		log.ErrorContext(ctx, "tls init failed", "error", err)

		return 1
	}

	grpcServer, err := grpcserver.NewTLS(log, serverTLS)
	if err != nil {
		log.ErrorContext(ctx, "grpc init failed", "error", err)

		return 1
	}

	usagev1.RegisterUsageServiceServer(grpcServer, grpcapi.NewServer(service, log))

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewRouter(log, redispkg.Check(redisClient), clickhouse.Check(clickDB)),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	if err := serve(ctx, log, httpServer, grpcServer, cfg.GRPCAddr, func(ctx context.Context) error {
		return natsadapter.Run(ctx, jetStream, service, log)
	}); err != nil {
		return 1
	}

	return 0
}

func serve(
	ctx context.Context,
	log *slog.Logger,
	httpServer *http.Server,
	grpcServer *grpc.Server,
	grpcAddr string,
	consume func(context.Context) error,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, workers)
	go func() {
		errCh <- httpserver.Serve(ctx, log, httpServer, shutdownTimeout)
	}()
	go func() {
		errCh <- grpcserver.Serve(ctx, log, grpcServer, grpcAddr, shutdownTimeout)
	}()
	go func() {
		errCh <- consume(ctx)
	}()

	var first error
	for range workers {
		if err := <-errCh; err != nil && first == nil {
			first = err
			cancel()
		}
	}

	return first
}

func closeRedis(ctx context.Context, log *slog.Logger, client *goredis.Client) {
	if err := client.Close(); err != nil {
		log.ErrorContext(context.WithoutCancel(ctx), "redis close failed", "error", err)
	}
}

func closeClickHouse(ctx context.Context, log *slog.Logger, db *sql.DB) {
	if err := db.Close(); err != nil {
		log.ErrorContext(context.WithoutCancel(ctx), "clickhouse close failed", "error", err)
	}
}

func closeNATS(ctx context.Context, log *slog.Logger, conn *natsio.Conn) {
	if err := conn.Drain(); err != nil {
		log.ErrorContext(context.WithoutCancel(ctx), "nats close failed", "error", err)
	}
}

func closeClient(ctx context.Context, log *slog.Logger, conn *grpc.ClientConn) {
	if err := conn.Close(); err != nil {
		log.ErrorContext(context.WithoutCancel(ctx), "grpc client close failed", "error", err)
	}
}
