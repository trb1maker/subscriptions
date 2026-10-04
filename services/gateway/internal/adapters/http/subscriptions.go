package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/trb1maker/subscriptions/services/gateway/internal/app"
)

type createTariffRequest struct {
	Name              string `json:"name"`
	MonthlyPriceMinor int64  `json:"monthly_price_minor"`
	MessageLimit      int32  `json:"message_limit"`
	Type              string `json:"type"`
	IsBaseTariff      bool   `json:"is_base_tariff"`
}

type tariffResponse struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	MonthlyPriceMinor int64  `json:"monthly_price_minor"`
	MessageLimit      int32  `json:"message_limit"`
	Type              string `json:"type"`
	IsBaseTariff      bool   `json:"is_base_tariff"`
}

type tariffListResponse struct {
	Tariffs []tariffResponse `json:"tariffs"`
}

type createSubscriptionRequest struct {
	TariffID string `json:"tariff_id"`
}

type changeSubscriptionRequest struct {
	TariffID string `json:"tariff_id"`
}

type subscriptionResponse struct {
	ID                 string `json:"id"`
	TariffID           string `json:"tariff_id"`
	Status             string `json:"status"`
	MessageAllowance   int64  `json:"message_allowance"`
	CurrentPeriodStart string `json:"current_period_start"`
	CurrentPeriodEnd   string `json:"current_period_end"`
}

func (h handler) createTariff(w http.ResponseWriter, r *http.Request) {
	var body createTariffRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeError(w, r, err)
		return
	}

	key, err := idempotencyKey(r)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	tariff, err := h.subscriptions.CreateTariff(r.Context(), key, body.Name, body.MonthlyPriceMinor, body.MessageLimit, body.Type, body.IsBaseTariff)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	writeJSON(h.log, w, r, http.StatusCreated, tariffJSON(tariff))
}

func (h handler) listTariffs(w http.ResponseWriter, r *http.Request) {
	tariffs, err := h.subscriptions.ListTariffs(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	out := make([]tariffResponse, 0, len(tariffs))
	for _, tariff := range tariffs {
		out = append(out, tariffJSON(tariff))
	}

	writeJSON(h.log, w, r, http.StatusOK, tariffListResponse{Tariffs: out})
}

func (h handler) createSubscription(w http.ResponseWriter, r *http.Request) {
	var body createSubscriptionRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeError(w, r, err)
		return
	}

	key, err := idempotencyKey(r)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	sub, err := h.subscriptions.CreateSubscription(r.Context(), key, body.TariffID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	writeJSON(h.log, w, r, http.StatusCreated, subscriptionJSON(sub))
}

func (h handler) changeSubscription(w http.ResponseWriter, r *http.Request) {
	var body changeSubscriptionRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeError(w, r, err)
		return
	}

	key, err := idempotencyKey(r)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	sub, err := h.subscriptions.ChangeSubscription(r.Context(), key, chi.URLParam(r, "id"), body.TariffID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	writeJSON(h.log, w, r, http.StatusOK, subscriptionJSON(sub))
}

func (h handler) getSubscription(w http.ResponseWriter, r *http.Request) {
	sub, err := h.subscriptions.GetSubscription(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	writeJSON(h.log, w, r, http.StatusOK, subscriptionJSON(sub))
}

func tariffJSON(tariff app.Tariff) tariffResponse {
	return tariffResponse{
		ID:                tariff.ID,
		Name:              tariff.Name,
		MonthlyPriceMinor: tariff.MonthlyPriceMinor,
		MessageLimit:      tariff.MessageLimit,
		Type:              tariff.Type,
		IsBaseTariff:      tariff.IsBase,
	}
}

func subscriptionJSON(sub app.Subscription) subscriptionResponse {
	return subscriptionResponse{
		ID:                 sub.ID,
		TariffID:           sub.TariffID,
		Status:             sub.Status,
		MessageAllowance:   sub.MessageAllowance,
		CurrentPeriodStart: sub.PeriodStart.UTC().Format(time.RFC3339),
		CurrentPeriodEnd:   sub.PeriodEnd.UTC().Format(time.RFC3339),
	}
}
