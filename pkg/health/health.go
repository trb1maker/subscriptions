package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

const (
	statusOK          = "ok"
	statusUnavailable = "unavailable"
)

// Check — именованная проверка зависимости. Ошибка значит, что зависимость не принимает запросы.
type Check struct {
	Name string
	Fn   func(context.Context) error
}

type response struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

// Handler отвечает по контракту GET /health.
// Без ошибок проверок — 200 и status ok. Иначе — 503 и checks только с упавшими зависимостями.
func Handler(log *slog.Logger, checks ...Check) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := response{Status: statusOK}

		for _, check := range checks {
			if err := check.Fn(r.Context()); err != nil {
				log.ErrorContext(r.Context(), "health check failed", "check", check.Name, "error", err)

				if body.Checks == nil {
					body.Status = statusUnavailable
					body.Checks = map[string]string{}
				}

				body.Checks[check.Name] = statusUnavailable
			}
		}

		status := http.StatusOK
		if body.Checks != nil {
			status = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)

		if err := json.NewEncoder(w).Encode(body); err != nil {
			log.ErrorContext(r.Context(), "health response failed", "error", err)
		}
	})
}
