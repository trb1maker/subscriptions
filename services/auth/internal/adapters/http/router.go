package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/trb1maker/subscriptions/pkg/health"
	"github.com/trb1maker/subscriptions/pkg/middleware"
)

// NewRouter собирает HTTP-маршруты Auth. Снаружи доступна только проверка здоровья.
func NewRouter(log *slog.Logger, checks ...health.Check) http.Handler {
	router := chi.NewRouter()
	middleware.Use(router, log)
	router.Method(http.MethodGet, "/health", health.Handler(log, checks...))

	return router
}
