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
	log           *slog.Logger
	auth          app.Auth
	subscriptions app.Subscriptions
	generator     app.Generator
	webhookKey    string
}

// NewRouter собирает внешние маршруты Gateway.
func NewRouter(
	log *slog.Logger,
	auth app.Auth,
	subscriptions app.Subscriptions,
	generator app.Generator,
	webhookKey string,
) http.Handler {
	h := handler{
		log:           log,
		auth:          auth,
		subscriptions: subscriptions,
		generator:     generator,
		webhookKey:    webhookKey,
	}

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
			protected.Method(http.MethodPost, "/tariffs", http.HandlerFunc(h.createTariff))
			protected.Method(http.MethodGet, "/tariffs", http.HandlerFunc(h.listTariffs))
			protected.Method(http.MethodPost, "/subscriptions", http.HandlerFunc(h.createSubscription))
			protected.Method(http.MethodPut, "/subscriptions/{id}", http.HandlerFunc(h.changeSubscription))
			protected.Method(http.MethodGet, "/subscriptions/{id}", http.HandlerFunc(h.getSubscription))
			protected.Method(http.MethodPost, "/generate", http.HandlerFunc(h.generate))
		})
	})

	return router
}
