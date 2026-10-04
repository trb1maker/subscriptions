package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Serve слушает server, пока не закроется ctx или сервер не остановится сам.
// Порт открывается до ожидания ctx, поэтому ошибка bind не теряется, если ctx уже отменён.
// По отмене ctx вызывает Shutdown и ждёт выход Serve.
// Если Shutdown не уложился в shutdownTimeout, соединения закрывает Close.
// http.ErrServerClosed ошибкой не считается.
func Serve(ctx context.Context, log *slog.Logger, server *http.Server, shutdownTimeout time.Duration) error {
	addr := server.Addr
	if addr == "" {
		addr = ":http"
	}

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

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		log.ErrorContext(shutdownCtx, "shutdown failed", "error", shutdownErr)

		if err := server.Close(); err != nil {
			log.ErrorContext(shutdownCtx, "close failed", "error", err)
		}
	}

	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.ErrorContext(shutdownCtx, "server stopped", "error", err)

		return fmt.Errorf("serve http: %w", err)
	}

	if shutdownErr != nil {
		return fmt.Errorf("shutdown http server: %w", shutdownErr)
	}

	return nil
}

func serveErr(ctx context.Context, log *slog.Logger, err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	log.ErrorContext(ctx, "server stopped", "error", err)

	return fmt.Errorf("serve http: %w", err)
}
