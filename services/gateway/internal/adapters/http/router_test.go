package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/pkg/logger"
	httpapi "github.com/trb1maker/subscriptions/services/gateway/internal/adapters/http"
)

func TestHealth(t *testing.T) {
	t.Parallel()

	log, err := logger.New(io.Discard, "info")
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	httpapi.NewRouter(log).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "ok", body.Status)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}
