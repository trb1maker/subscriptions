package middleware_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/pkg/logger"
	"github.com/trb1maker/subscriptions/pkg/middleware"
)

func TestRequestID(t *testing.T) {
	t.Parallel()

	var got string
	router := newRouter(t, io.Discard, "info")
	router.Get("/", func(_ http.ResponseWriter, r *http.Request) {
		got = logger.RequestID(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, got, rec.Header().Get("X-Request-ID"))
	require.NotEmpty(t, got)
}

func TestRequestIDFromHeader(t *testing.T) {
	t.Parallel()

	const want = "req-from-caller"
	var got string
	router := newRouter(t, io.Discard, "info")
	router.Get("/", func(_ http.ResponseWriter, r *http.Request) {
		got = logger.RequestID(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", want)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, want, got)
	require.Equal(t, want, rec.Header().Get("X-Request-ID"))
}

func TestRecover(t *testing.T) {
	t.Parallel()

	router := newRouter(t, io.Discard, "info")
	router.Get("/", func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() {
		router.ServeHTTP(rec, req)
	})
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestAccessLogKeepsRequestID(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	router := newRouter(t, &buf, "debug")
	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Request-ID", "req-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	require.Equal(t, "request completed", line["msg"])
	require.Equal(t, "req-1", line["request_id"])
	require.Equal(t, http.StatusNoContent, rec.Code)
}

func newRouter(t *testing.T, w io.Writer, level string) chi.Router {
	t.Helper()

	log, err := logger.New(w, level)
	require.NoError(t, err)

	router := chi.NewRouter()
	middleware.Use(router, log)

	return router
}
