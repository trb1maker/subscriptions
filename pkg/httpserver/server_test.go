package httpserver_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/pkg/httpserver"
	"github.com/trb1maker/subscriptions/pkg/logger"
)

func TestServeShutdownWaitsForRequest(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	server := &http.Server{
		Addr: freeAddr(t),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(entered)
			<-release
			w.WriteHeader(http.StatusNoContent)
		}),
		ReadHeaderTimeout: time.Second,
	}

	log, err := logger.New(io.Discard, "info")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpserver.Serve(ctx, log, server, time.Second)
	}()

	respCh := make(chan *http.Response, 1)
	getErr := make(chan error, 1)
	go func() {
		resp, err := getReady("http://" + server.Addr + "/")
		if err != nil {
			getErr <- err
			return
		}

		respCh <- resp
	}()

	<-entered
	cancel()
	close(release)

	var resp *http.Response
	select {
	case resp = <-respCh:
	case err := <-getErr:
		require.NoError(t, err)
	}
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
	require.NoError(t, <-serveErr)
}

func TestServeListenError(t *testing.T) {
	t.Parallel()

	log, err := logger.New(io.Discard, "error")
	require.NoError(t, err)

	err = httpserver.Serve(context.Background(), log, listenServer(t, "127.0.0.1:-1"), time.Second)
	require.Error(t, err)
}

func TestServeShutdownTimeoutClosesServer(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	server := &http.Server{
		Addr: freeAddr(t),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(entered)
			<-r.Context().Done()
			w.WriteHeader(http.StatusNoContent)
		}),
		ReadHeaderTimeout: time.Second,
	}

	log, err := logger.New(io.Discard, "error")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpserver.Serve(ctx, log, server, 100*time.Millisecond)
	}()

	go func() {
		_, _ = getReady("http://" + server.Addr + "/")
	}()

	<-entered
	cancel()

	select {
	case err := <-serveErr:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(time.Second):
		t.Fatal("serve did not return")
	}

	dial, err := net.DialTimeout("tcp", server.Addr, time.Second)
	require.Error(t, err)
	if dial != nil {
		require.NoError(t, dial.Close())
	}
}

func TestServeCancelledContextReturnsListenError(t *testing.T) {
	t.Parallel()

	log, err := logger.New(io.Discard, "error")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = httpserver.Serve(ctx, log, listenServer(t, "127.0.0.1:-1"), time.Second)
	require.Error(t, err)
	require.NotErrorIs(t, err, context.Canceled)
}

func listenServer(t *testing.T, addr string) *http.Server {
	t.Helper()

	return &http.Server{
		Addr:              addr,
		Handler:           http.NewServeMux(),
		ReadHeaderTimeout: time.Second,
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	return addr
}

func getReady(url string) (*http.Response, error) {
	const pollInterval = 10 * time.Millisecond

	deadline := time.Now().Add(time.Second)
	var lastErr error

	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			return resp, nil
		}

		lastErr = err
		time.Sleep(pollInterval)
	}

	return nil, lastErr
}
