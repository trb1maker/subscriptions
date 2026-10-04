package grpcclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/trb1maker/subscriptions/pkg/caller"
	"github.com/trb1maker/subscriptions/pkg/metrics"
)

// Option подключает метрики исходящих вызовов.
type Option func(*clientConfig)

type clientConfig struct {
	metrics *metrics.Metrics
}

// WithMetrics считает исходящие вызовы до оборачивания ошибки.
func WithMetrics(m *metrics.Metrics) Option {
	return func(cfg *clientConfig) {
		cfg.metrics = m
	}
}

// Dial соединяется с target по mTLS и прокидывает вызывающего из context.
func Dial(ctx context.Context, target string, tlsCfg *tls.Config, opts ...Option) (*grpc.ClientConn, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("dial grpc: %w", err)
	}

	if tlsCfg == nil {
		return nil, errors.New("missing tls config")
	}

	cfg := clientConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	interceptors := []grpc.UnaryClientInterceptor{UnaryClientInterceptor()}
	if cfg.metrics != nil {
		interceptors = append(interceptors, clientMetrics(cfg.metrics))
	}

	conn, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithChainUnaryInterceptor(interceptors...),
	)
	if err != nil {
		return nil, fmt.Errorf("dial grpc: %w", err)
	}

	return conn, nil
}

func clientMetrics(m *metrics.Metrics) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		start := time.Now()
		err := invoker(ctx, method, req, reply, cc, opts...)
		m.ObserveClient(method, status.Code(err).String(), time.Since(start))

		return err
	}
}

// UnaryClientInterceptor дописывает request id и вызывающего в исходящий вызов.
func UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if err := invoker(caller.AppendOutgoing(ctx), method, req, reply, cc, opts...); err != nil {
			return fmt.Errorf("invoke grpc: %w", err)
		}

		return nil
	}
}
