package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"

	"github.com/trb1maker/subscriptions/services/gateway/internal/domain"
)

const maxRequestBytes = 1 << 20

const (
	codeInvalidArgument = "invalid_argument"
	codeUnauthenticated = "unauthenticated"
	codeNotFound        = "not_found"
	codeConflict        = "conflict"
	codeUnavailable     = "unavailable"
	codeInternal        = "internal"
	codeForbidden       = "forbidden"
)

type errorBody struct {
	Error string `json:"error"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return domain.ErrInvalidArgument
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return domain.ErrInvalidArgument
	}

	if dec.More() {
		return domain.ErrInvalidArgument
	}

	return nil
}

func (h handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	code := codeInternal
	switch {
	case errors.Is(err, domain.ErrInvalidArgument):
		status, code = http.StatusBadRequest, codeInvalidArgument
	case errors.Is(err, domain.ErrUnauthenticated), errors.Is(err, domain.ErrInvalidWebhookKey):
		status, code = http.StatusUnauthorized, codeUnauthenticated
	case errors.Is(err, domain.ErrForbidden):
		status, code = http.StatusForbidden, codeForbidden
	case errors.Is(err, domain.ErrNotFound):
		status, code = http.StatusNotFound, codeNotFound
	case errors.Is(err, domain.ErrConflict):
		status, code = http.StatusConflict, codeConflict
	case errors.Is(err, domain.ErrUnavailable):
		status, code = http.StatusServiceUnavailable, codeUnavailable
	case errors.Is(err, domain.ErrInternal):
		status, code = http.StatusInternalServerError, codeInternal
	default:
		h.log.ErrorContext(r.Context(), "request failed", "error", err)
	}

	writeJSON(h.log, w, r, status, errorBody{Error: code})
}

func writeJSON(log *slog.Logger, w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.ErrorContext(r.Context(), "response failed", "error", err)
	}
}
