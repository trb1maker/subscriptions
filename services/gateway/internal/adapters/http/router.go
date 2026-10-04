package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/trb1maker/subscriptions/pkg/health"
	"github.com/trb1maker/subscriptions/pkg/middleware"
)

// NewRouter собирает внешние маршруты Gateway.
func NewRouter(log *slog.Logger) http.Handler {
	router := chi.NewRouter()
	middleware.Use(router, log)
	router.Method(http.MethodGet, "/health", health.Handler(log))

	return router
}
