package health_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/pkg/health"
	"github.com/trb1maker/subscriptions/pkg/logger"
)

func TestHandlerWithoutChecks(t *testing.T) {
	t.Parallel()

	rec := serve(t, io.Discard, "info")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestHandlerAllChecksPass(t *testing.T) {
	t.Parallel()

	rec := serve(t, io.Discard, "info",
		health.Check{Name: "postgres", Fn: func(context.Context) error { return nil }},
		health.Check{Name: "redis", Fn: func(context.Context) error { return nil }},
	)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestHandlerReportsFailedChecks(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	rec := serve(t, &buf, "info",
		health.Check{Name: "postgres", Fn: func(context.Context) error { return errors.New("db down") }},
		health.Check{Name: "redis", Fn: func(context.Context) error { return nil }},
		health.Check{Name: "nats", Fn: func(context.Context) error { return errors.New("nats down") }},
	)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "unavailable", body.Status)
	require.Equal(t, map[string]string{
		"postgres": "unavailable",
		"nats":     "unavailable",
	}, body.Checks)
	require.NotContains(t, rec.Body.String(), "db down")
	require.NotContains(t, rec.Body.String(), "nats down")

	logText := buf.String()
	require.Contains(t, logText, "postgres")
	require.Contains(t, logText, "db down")
	require.Contains(t, logText, "nats")
	require.Contains(t, logText, "nats down")
	require.NotContains(t, logText, `"check":"redis"`)
}

func serve(t *testing.T, w io.Writer, level string, checks ...health.Check) *httptest.ResponseRecorder {
	t.Helper()

	log, err := logger.New(w, level)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	health.Handler(log, checks...).ServeHTTP(rec, req)

	return rec
}
