package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/trb1maker/subscriptions/pkg/health"
	"github.com/trb1maker/subscriptions/pkg/metrics"
	"github.com/trb1maker/subscriptions/pkg/middleware"
)

// NewRouter собирает HTTP-маршруты Subscriptions. Снаружи доступны проверка здоровья и метрики.
func NewRouter(log *slog.Logger, met *metrics.Metrics, checks ...health.Check) http.Handler {
	router := chi.NewRouter()
	opts := []middleware.Option{middleware.WithTracing()}
	if met != nil {
		opts = append(opts, middleware.WithMetrics(met))
	}

	middleware.Use(router, log, opts...)
	if met != nil {
		router.Method(http.MethodGet, "/metrics", met.Handler())
	}

	router.Method(http.MethodGet, "/health", health.Handler(log, checks...))

	return router
}
