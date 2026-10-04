package grpcserver

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const defaultHandlerTimeout = 10 * time.Second

// New собирает gRPC-сервер с восстановлением после паники и дедлайном вызова.
// Если у входящего контекста уже есть дедлайн, он сохраняется.
func New(log *slog.Logger) *grpc.Server {
	return grpc.NewServer(grpc.ChainUnaryInterceptor(
		recoverInterceptor(log),
		deadlineInterceptor(defaultHandlerTimeout),
	))
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
