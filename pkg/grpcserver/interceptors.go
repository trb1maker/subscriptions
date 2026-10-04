package grpcserver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/trb1maker/subscriptions/pkg/caller"
)

const defaultHandlerTimeout = 10 * time.Second

// New собирает gRPC-сервер без TLS, с восстановлением после паники и дедлайном вызова.
// Если у входящего контекста уже есть дедлайн, он сохраняется.
// Входящая metadata вызывающего переносится в context.
func New(log *slog.Logger) *grpc.Server {
	return newServer(log)
}

// NewTLS собирает gRPC-сервер, который принимает только клиентов с сертификатом.
func NewTLS(log *slog.Logger, tlsCfg *tls.Config) (*grpc.Server, error) {
	if tlsCfg == nil {
		return nil, errors.New("missing tls config")
	}

	return newServer(log, grpc.Creds(credentials.NewTLS(tlsCfg))), nil
}

func newServer(log *slog.Logger, opts ...grpc.ServerOption) *grpc.Server {
	options := []grpc.ServerOption{grpc.ChainUnaryInterceptor(
		callerInterceptor(),
		recoverInterceptor(log),
		deadlineInterceptor(defaultHandlerTimeout),
	)}
	options = append(options, opts...)

	return grpc.NewServer(options...)
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
