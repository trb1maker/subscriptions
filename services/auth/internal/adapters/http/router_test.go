package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/pkg/health"
	"github.com/trb1maker/subscriptions/pkg/logger"
	httpapi "github.com/trb1maker/subscriptions/services/auth/internal/adapters/http"
)

func TestHealthWithoutChecks(t *testing.T) {
	t.Parallel()

	body := getHealth(t, httpapi.NewRouter(discardLog(t)))
	require.Equal(t, http.StatusOK, body.code)
	require.Equal(t, "ok", body.status)
}

func TestHealthReportsPostgres(t *testing.T) {
	t.Parallel()

	router := httpapi.NewRouter(discardLog(t), health.Check{
		Name: "postgres",
		Fn: func(context.Context) error {
			return errors.New("down")
		},
	})
	body := getHealth(t, router)
	require.Equal(t, http.StatusServiceUnavailable, body.code)
	require.Equal(t, "unavailable", body.status)
	require.Equal(t, map[string]string{"postgres": "unavailable"}, body.checks)
}

func discardLog(t *testing.T) *slog.Logger {
	t.Helper()

	log, err := logger.New(io.Discard, "error")
	require.NoError(t, err)

	return log
}

type healthBody struct {
	code   int
	status string
	checks map[string]string
}

func getHealth(t *testing.T, handler http.Handler) healthBody {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	return healthBody{code: rec.Code, status: body.Status, checks: body.Checks}
}
