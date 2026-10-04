package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/trb1maker/subscriptions/pkg/health"
	"github.com/trb1maker/subscriptions/pkg/middleware"
	"github.com/trb1maker/subscriptions/services/gateway/internal/app"
)

type handler struct {
	log        *slog.Logger
	auth       app.Auth
	webhookKey string
}

// NewRouter собирает внешние маршруты Gateway.
func NewRouter(log *slog.Logger, auth app.Auth, webhookKey string) http.Handler {
	h := handler{log: log, auth: auth, webhookKey: webhookKey}

	router := chi.NewRouter()
	middleware.Use(router, log)
	router.Method(http.MethodGet, "/health", health.Handler(log))
	router.Method(http.MethodPost, "/webhooks/payments", http.HandlerFunc(h.paymentWebhook))
	router.Route("/api/v1", func(api chi.Router) {
		api.Method(http.MethodPost, "/users", http.HandlerFunc(h.register))
		api.Method(http.MethodPost, "/login", http.HandlerFunc(h.login))
		api.Group(func(protected chi.Router) {
			protected.Use(h.authenticate)
			protected.Method(http.MethodPost, "/organizations", http.HandlerFunc(h.createOrganization))
		})
	})

	return router
}
