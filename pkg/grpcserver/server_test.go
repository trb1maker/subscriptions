package grpcserver_test

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/trb1maker/subscriptions/pkg/grpcserver"
	"github.com/trb1maker/subscriptions/pkg/logger"
)

func TestServeShutdown(t *testing.T) {
	t.Parallel()

	log, err := logger.New(io.Discard, "error")
	require.NoError(t, err)

	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- grpcserver.Serve(ctx, log, grpc.NewServer(), addr, time.Second)
	}()

	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 10*time.Millisecond)
		if err != nil {
			return false
		}

		require.NoError(t, conn.Close())

		return true
	}, time.Second, 10*time.Millisecond)

	cancel()
	require.NoError(t, <-serveErr)
}

func TestServeListenError(t *testing.T) {
	t.Parallel()

	log, err := logger.New(io.Discard, "error")
	require.NoError(t, err)

	err = grpcserver.Serve(context.Background(), log, grpc.NewServer(), "127.0.0.1:-1", time.Second)
	require.Error(t, err)
}

func freeAddr(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	return addr
}
