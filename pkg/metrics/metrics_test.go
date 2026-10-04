package metrics_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/pkg/health"
	"github.com/trb1maker/subscriptions/pkg/metrics"
)

func TestHTTPMetricsUseRoutePattern(t *testing.T) {
	t.Parallel()

	met := metrics.New("gateway")
	router := chi.NewRouter()
	router.Use(met.Middleware())
	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	router.Get("/v1/items/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	router.Method(http.MethodGet, "/metrics", met.Handler())

	hit(router, "/health")
	hit(router, "/v1/items/abc")
	hit(router, "/v1/items/abc")

	body := scrape(t, met)
	require.Contains(t, body, `http_requests_total{endpoint="/v1/items/{id}",service="gateway",status="204"} 2`)
	require.NotContains(t, body, `endpoint="/health"`)
	require.NotContains(t, body, `endpoint="/metrics"`)
	require.Contains(t, body, `service_healthy`)
}

func TestHTTPErrorCounter(t *testing.T) {
	t.Parallel()

	met := metrics.New("gateway")
	router := chi.NewRouter()
	router.Use(met.Middleware())
	router.Post("/api/v1/generate", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	hit(router, "/api/v1/generate")

	body := scrape(t, met)
	require.Contains(t, body, `http_request_errors_total{endpoint="/api/v1/generate",error_type="502",service="gateway"} 1`)
}

func TestWatchHealth(t *testing.T) {
	t.Parallel()

	met := metrics.New("auth")
	var down atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	met.WatchHealth(ctx, healthTick, health.Check{
		Name: "postgres",
		Fn: func(context.Context) error {
			if down.Load() {
				return errors.New("down")
			}

			return nil
		},
	})

	require.Eventually(t, func() bool {
		return strings.Contains(scrape(t, met), `service_healthy{service="auth"} 1`)
	}, time.Second, healthTick)

	down.Store(true)
	require.Eventually(t, func() bool {
		return strings.Contains(scrape(t, met), `service_healthy{service="auth"} 0`)
	}, time.Second, healthTick)

	down.Store(false)
	require.Eventually(t, func() bool {
		return strings.Contains(scrape(t, met), `service_healthy{service="auth"} 1`)
	}, time.Second, healthTick)

	cancel()
	require.Eventually(t, func() bool {
		return strings.Contains(scrape(t, met), `service_healthy{service="auth"} 0`)
	}, time.Second, healthTick)
}

func hit(router http.Handler, path string) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if path == "/api/v1/generate" {
		req = httptest.NewRequest(http.MethodPost, path, nil)
	}

	router.ServeHTTP(httptest.NewRecorder(), req)
}

func scrape(t *testing.T, met *metrics.Metrics) string {
	t.Helper()

	rec := httptest.NewRecorder()
	met.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	return rec.Body.String()
}

const healthTick = 20 * time.Millisecond
