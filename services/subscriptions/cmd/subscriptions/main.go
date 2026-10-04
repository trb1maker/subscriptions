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
	natsio "github.com/nats-io/nats.go"
	"github.com/robfig/cron/v3"
	"google.golang.org/grpc"

	authv1 "github.com/trb1maker/subscriptions/api/gen/auth/v1"
	subscriptionsv1 "github.com/trb1maker/subscriptions/api/gen/subscriptions/v1"
	usagev1 "github.com/trb1maker/subscriptions/api/gen/usage/v1"
	"github.com/trb1maker/subscriptions/pkg/grpcclient"
	"github.com/trb1maker/subscriptions/pkg/grpcserver"
	"github.com/trb1maker/subscriptions/pkg/httpserver"
	"github.com/trb1maker/subscriptions/pkg/logger"
	"github.com/trb1maker/subscriptions/pkg/mtls"
	natspkg "github.com/trb1maker/subscriptions/pkg/nats"
	"github.com/trb1maker/subscriptions/pkg/postgres"
	authadapter "github.com/trb1maker/subscriptions/services/subscriptions/internal/adapters/auth"
	grpcapi "github.com/trb1maker/subscriptions/services/subscriptions/internal/adapters/grpc"
	httpapi "github.com/trb1maker/subscriptions/services/subscriptions/internal/adapters/http"
	natsadapter "github.com/trb1maker/subscriptions/services/subscriptions/internal/adapters/nats"
	subscriptionspostgres "github.com/trb1maker/subscriptions/services/subscriptions/internal/adapters/postgres"
	usageadapter "github.com/trb1maker/subscriptions/services/subscriptions/internal/adapters/usage"
	"github.com/trb1maker/subscriptions/services/subscriptions/internal/app"
)

const (
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
	expireTimeout     = 30 * time.Second
	servers           = 2
)

type config struct {
	HTTPAddr            string `env:"HTTP_ADDR"                  envDefault:":8082"`
	GRPCAddr            string `env:"GRPC_ADDR"                  envDefault:":9092"`
	LogLevel            string `env:"LOG_LEVEL"                  envDefault:"info"`
	DatabaseURL         string `env:"SUBSCRIPTIONS_DATABASE_URL,required"`
	AuthGRPCAddr        string `env:"AUTH_GRPC_ADDR,required"`
	AuthGRPCServerName  string `env:"AUTH_GRPC_SERVER_NAME"      envDefault:"localhost"`
	UsageGRPCAddr       string `env:"USAGE_GRPC_ADDR,required"`
	UsageGRPCServerName string `env:"USAGE_GRPC_SERVER_NAME"     envDefault:"localhost"`
	NATSURL             string `env:"SUBSCRIPTIONS_NATS_URL,required"`
	RequestPepper       string `env:"SUBSCRIPTIONS_REQUEST_PEPPER,required"`
	ExpireSchedule      string `env:"SUBSCRIPTIONS_EXPIRE_SCHEDULE" envDefault:"*/1 * * * *"`
	TLSCertFile         string `env:"TLS_CERT_FILE,required"`
	TLSKeyFile          string `env:"TLS_KEY_FILE,required"`
	TLSCAFile           string `env:"TLS_CA_FILE,required"`
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

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.ErrorContext(ctx, "postgres init failed", "error", err)

		return 1
	}
	defer pool.Close()

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

	usageTLS, err := mtls.ClientConfig(tlsFiles, cfg.UsageGRPCServerName)
	if err != nil {
		log.ErrorContext(ctx, "tls init failed", "error", err)

		return 1
	}

	usageConn, err := grpcclient.Dial(ctx, cfg.UsageGRPCAddr, usageTLS)
	if err != nil {
		log.ErrorContext(ctx, "usage client init failed", "error", err)

		return 1
	}
	defer closeClient(ctx, log, usageConn)

	natsConn, jetStream, err := natspkg.Connect(ctx, cfg.NATSURL)
	if err != nil {
		log.ErrorContext(ctx, "nats init failed", "error", err)

		return 1
	}
	defer closeNATS(ctx, log, natsConn)

	service, err := app.New(
		subscriptionspostgres.NewRepository(pool),
		authadapter.NewClient(authv1.NewAuthServiceClient(authConn), log),
		usageadapter.NewClient(usagev1.NewUsageServiceClient(usageConn), log),
		natsadapter.NewPublisher(jetStream, log),
		[]byte(cfg.RequestPepper),
		nil,
	)
	if err != nil {
		log.ErrorContext(ctx, "subscriptions service init failed", "error", err)

		return 1
	}

	scheduler := cron.New()
	if _, err := scheduler.AddFunc(cfg.ExpireSchedule, func() {
		jobCtx, cancel := context.WithTimeout(context.Background(), expireTimeout)
		defer cancel()

		report, jobErr := service.CloseExpired(jobCtx)
		if jobErr != nil {
			log.ErrorContext(jobCtx, "close expired subscriptions failed", "error", jobErr)

			return
		}

		for _, id := range report.Skipped {
			log.WarnContext(jobCtx, "base tariff missing", "subscription_id", id.String())
		}
	}); err != nil {
		log.ErrorContext(ctx, "cron init failed", "error", err)

		return 1
	}

	scheduler.Start()
	defer func() {
		stopCtx := scheduler.Stop()
		<-stopCtx.Done()
	}()

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

	subscriptionsv1.RegisterSubscriptionServiceServer(grpcServer, grpcapi.NewServer(service, log))

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewRouter(log, postgres.Check(pool)),
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
