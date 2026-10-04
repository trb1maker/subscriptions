package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/trb1maker/subscriptions/services/gateway/internal/app"
	"github.com/trb1maker/subscriptions/services/gateway/internal/domain"
)

const webhookKeyHeader = "X-Webhook-Key"

func (h handler) paymentWebhook(w http.ResponseWriter, r *http.Request) {
	if err := app.AcceptPaymentWebhook(r.Header.Get(webhookKeyHeader), h.webhookKey); err != nil {
		h.log.WarnContext(r.Context(), "webhook rejected")
		h.writeError(w, r, err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.writeError(w, r, domain.ErrInvalidArgument)
			return
		}

		h.log.DebugContext(r.Context(), "webhook body discarded", "error", err)
	}

	w.WriteHeader(http.StatusAccepted)
}
