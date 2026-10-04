package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
)

// Serve слушает addr, пока не закроется ctx или сервер не остановится сам.
// Порт открывается до ожидания ctx, поэтому ошибка bind не теряется, если ctx уже отменён.
// По отмене ctx вызывает GracefulStop и ждёт выход Serve.
// Если остановка не уложилась в shutdownTimeout, соединения закрывает Stop.
// grpc.ErrServerStopped ошибкой не считается.
func Serve(ctx context.Context, log *slog.Logger, server *grpc.Server, addr string, shutdownTimeout time.Duration) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return serveErr(ctx, log, err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(ln)
	}()

	select {
	case err := <-errCh:
		return serveErr(ctx, log, err)
	case <-ctx.Done():
	}

	stopped := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(stopped)
	}()

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	var shutdownErr error
	select {
	case <-stopped:
	case <-shutdownCtx.Done():
		shutdownErr = fmt.Errorf("shutdown grpc server: %w", shutdownCtx.Err())
		log.ErrorContext(shutdownCtx, "shutdown failed", "error", shutdownErr)
		server.Stop()
	}

	if err := <-errCh; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		log.ErrorContext(shutdownCtx, "server stopped", "error", err)

		return fmt.Errorf("serve grpc: %w", err)
	}

	return shutdownErr
}

func serveErr(ctx context.Context, log *slog.Logger, err error) error {
	if err == nil || errors.Is(err, grpc.ErrServerStopped) {
		return nil
	}

	log.ErrorContext(ctx, "server stopped", "error", err)

	return fmt.Errorf("serve grpc: %w", err)
}
