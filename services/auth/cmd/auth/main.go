package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"google.golang.org/grpc"

	authv1 "github.com/trb1maker/subscriptions/api/gen/auth/v1"
	"github.com/trb1maker/subscriptions/pkg/grpcserver"
	"github.com/trb1maker/subscriptions/pkg/httpserver"
	"github.com/trb1maker/subscriptions/pkg/logger"
	"github.com/trb1maker/subscriptions/pkg/mtls"
	"github.com/trb1maker/subscriptions/pkg/observe"
	"github.com/trb1maker/subscriptions/pkg/postgres"
	grpcapi "github.com/trb1maker/subscriptions/services/auth/internal/adapters/grpc"
	httpapi "github.com/trb1maker/subscriptions/services/auth/internal/adapters/http"
	jwtissuer "github.com/trb1maker/subscriptions/services/auth/internal/adapters/jwt"
	"github.com/trb1maker/subscriptions/services/auth/internal/adapters/password"
	authpostgres "github.com/trb1maker/subscriptions/services/auth/internal/adapters/postgres"
	"github.com/trb1maker/subscriptions/services/auth/internal/app"
)

const (
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
	servers           = 2
	serviceName       = "auth"
)

type config struct {
	HTTPAddr     string        `env:"HTTP_ADDR"         envDefault:":8081"`
	GRPCAddr     string        `env:"GRPC_ADDR"         envDefault:":9091"`
	LogLevel     string        `env:"LOG_LEVEL"         envDefault:"info"`
	OTELEndpoint string        `env:"OTEL_EXPORTER_OTLP_ENDPOINT" envDefault:"localhost:54317"`
	DatabaseURL  string        `env:"AUTH_DATABASE_URL,required"`
	JWTSecret    string        `env:"JWT_SECRET,required"`
	JWTTTL       time.Duration `env:"JWT_TTL"           envDefault:"1h"`
	TLSCertFile  string        `env:"TLS_CERT_FILE,required"`
	TLSKeyFile   string        `env:"TLS_KEY_FILE,required"`
	TLSCAFile    string        `env:"TLS_CA_FILE,required"`
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

	obs, err := observe.Start(context.Background(), observe.Config{
		Service:  serviceName,
		Level:    cfg.LogLevel,
		Endpoint: cfg.OTELEndpoint,
	})
	if err != nil {
		fallback, fallbackErr := logger.New(os.Stderr, "info")
		if fallbackErr != nil {
			return 1
		}

		fallback.ErrorContext(context.Background(), "logger init failed", "error", err)

		return 1
	}

	defer obs.Shutdown(context.Background())
	log := obs.Log

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.ErrorContext(ctx, "postgres init failed", "error", err)

		return 1
	}
	defer pool.Close()
	obs.Watch(ctx, postgres.Check(pool))

	issuer, err := jwtissuer.NewIssuer(cfg.JWTSecret, cfg.JWTTTL)
	if err != nil {
		log.ErrorContext(ctx, "jwt init failed", "error", err)

		return 1
	}

	service, err := app.New(authpostgres.NewRepository(pool), password.Hasher{}, issuer, []byte(cfg.JWTSecret), nil)
	if err != nil {
		log.ErrorContext(ctx, "auth service init failed", "error", err)

		return 1
	}

	tlsCfg, err := mtls.ServerConfig(mtls.Files{
		CertFile: cfg.TLSCertFile,
		KeyFile:  cfg.TLSKeyFile,
		CAFile:   cfg.TLSCAFile,
	})
	if err != nil {
		log.ErrorContext(ctx, "tls init failed", "error", err)

		return 1
	}

	grpcServer, err := grpcserver.NewTLS(log, tlsCfg, grpcserver.WithMetrics(obs.Metrics))
	if err != nil {
		log.ErrorContext(ctx, "grpc init failed", "error", err)

		return 1
	}

	authv1.RegisterAuthServiceServer(grpcServer, grpcapi.NewServer(service, log))

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewRouter(log, obs.Metrics, postgres.Check(pool)),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	if err := serve(ctx, log, httpServer, grpcServer, cfg.GRPCAddr); err != nil {
		return 1
	}

	return 0
}

func serve(ctx context.Context, log *slog.Logger, httpServer *http.Server, grpcServer *grpc.Server, grpcAddr string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, servers)
	go func() {
		errCh <- httpserver.Serve(ctx, log, httpServer, shutdownTimeout)
	}()
	go func() {
		errCh <- grpcserver.Serve(ctx, log, grpcServer, grpcAddr, shutdownTimeout)
	}()

	var first error
	for range servers {
		if err := <-errCh; err != nil && first == nil {
			first = err
			cancel()
		}
	}

	return first
}
