package grpcserver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/trb1maker/subscriptions/pkg/caller"
	"github.com/trb1maker/subscriptions/pkg/metrics"
)

const defaultHandlerTimeout = 10 * time.Second

// Option подключает метрики gRPC.
type Option func(*serverConfig)

type serverConfig struct {
	metrics *metrics.Metrics
}

// WithMetrics считает входящие вызовы.
func WithMetrics(m *metrics.Metrics) Option {
	return func(cfg *serverConfig) {
		cfg.metrics = m
	}
}

// New собирает gRPC-сервер без TLS, с восстановлением после паники и дедлайном вызова.
// Если у входящего контекста уже есть дедлайн, он сохраняется.
// Входящая metadata вызывающего переносится в context.
func New(log *slog.Logger, opts ...Option) *grpc.Server {
	return newServer(log, options(opts))
}

// NewTLS собирает gRPC-сервер, который принимает только клиентов с сертификатом.
func NewTLS(log *slog.Logger, tlsCfg *tls.Config, opts ...Option) (*grpc.Server, error) {
	if tlsCfg == nil {
		return nil, errors.New("missing tls config")
	}

	return newServer(log, options(opts), grpc.Creds(credentials.NewTLS(tlsCfg))), nil
}

func options(opts []Option) serverConfig {
	cfg := serverConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	return cfg
}

func newServer(log *slog.Logger, cfg serverConfig, opts ...grpc.ServerOption) *grpc.Server {
	interceptors := []grpc.UnaryServerInterceptor{
		callerInterceptor(),
		recoverInterceptor(log),
		deadlineInterceptor(defaultHandlerTimeout),
	}
	if cfg.metrics != nil {
		interceptors = append([]grpc.UnaryServerInterceptor{serverMetrics(cfg.metrics)}, interceptors...)
	}

	options := []grpc.ServerOption{
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(interceptors...),
	}
	options = append(options, opts...)

	return grpc.NewServer(options...)
}

func serverMetrics(m *metrics.Metrics) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)

		method := ""
		if info != nil {
			method = info.FullMethod
		}

		m.ObserveServer(method, status.Code(err).String(), time.Since(start))

		return resp, err
	}
}

func callerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(caller.FromIncoming(ctx), req)
	}
}

func recoverInterceptor(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}

			method := ""
			if info != nil {
				method = info.FullMethod
			}

			log.ErrorContext(ctx, "panic recovered",
				"method", method,
				"panic", fmt.Sprint(recovered),
				"stack", string(debug.Stack()),
			)

			err = status.Error(codes.Internal, "internal")
		}()

		return handler(ctx, req)
	}
}

func deadlineInterceptor(timeout time.Duration) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if _, ok := ctx.Deadline(); ok {
			return handler(ctx, req)
		}

		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		return handler(ctx, req)
	}
}
