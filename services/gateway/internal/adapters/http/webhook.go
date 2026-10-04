package httpapi

import (
	"net/http"
	"time"
	"uuid"

	"github.com/trb1maker/subscriptions/services/gateway/internal/app"
	"github.com/trb1maker/subscriptions/services/gateway/internal/domain"
)

const webhookKeyHeader = "X-Webhook-Key"

type paymentWebhookRequest struct {
	PaymentID      string `json:"payment_id"`
	SubscriptionID string `json:"subscription_id"`
	AmountMinor    int64  `json:"amount_minor"`
	OccurredAt     string `json:"occurred_at"`
}

func (h handler) paymentWebhook(w http.ResponseWriter, r *http.Request) {
	if err := app.AcceptPaymentWebhook(r.Header.Get(webhookKeyHeader), h.webhookKey); err != nil {
		h.log.WarnContext(r.Context(), "webhook rejected")
		h.writeError(w, r, err)
		return
	}

	var body paymentWebhookRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeError(w, r, err)
		return
	}

	occurredAt, err := time.Parse(time.RFC3339Nano, body.OccurredAt)
	if err != nil || !validPaymentWebhook(body) {
		h.writeError(w, r, domain.ErrInvalidArgument)
		return
	}

	if err := h.subscriptions.ProcessPayment(r.Context(), body.PaymentID, body.SubscriptionID, body.AmountMinor, occurredAt); err != nil {
		h.writeError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func validPaymentWebhook(body paymentWebhookRequest) bool {
	paymentID, err := uuid.Parse(body.PaymentID)
	if err != nil || paymentID == uuid.Nil() {
		return false
	}

	subscriptionID, err := uuid.Parse(body.SubscriptionID)
	if err != nil || subscriptionID == uuid.Nil() {
		return false
	}

	return body.AmountMinor >= 0 && body.OccurredAt != ""
}
