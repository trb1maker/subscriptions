package httpapi

import (
	"net/http"
	"strings"

	"github.com/trb1maker/subscriptions/pkg/caller"
	"github.com/trb1maker/subscriptions/services/gateway/internal/domain"
)

const idempotencyHeader = "Idempotency-Key"

type registerRequest struct {
	Email          string `json:"email"`
	Password       string `json:"password"`
	OrganizationID string `json:"organization_id"`
}

type registerResponse struct {
	UserID string `json:"user_id"`
	Token  string `json:"token"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
}

type createOrganizationRequest struct {
	Name string `json:"name"`
}

type createOrganizationResponse struct {
	OrganizationID string `json:"organization_id"`
	Token          string `json:"token"`
}

func (h handler) register(w http.ResponseWriter, r *http.Request) {
	var body registerRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeError(w, r, err)
		return
	}

	key, err := idempotencyKey(r)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	created, err := h.auth.Register(r.Context(), body.Email, body.Password, key, body.OrganizationID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	writeJSON(h.log, w, r, http.StatusCreated, registerResponse{UserID: created.UserID, Token: created.Token})
}

func (h handler) login(w http.ResponseWriter, r *http.Request) {
	var body loginRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeError(w, r, err)
		return
	}

	token, err := h.auth.Login(r.Context(), body.Email, body.Password)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	writeJSON(h.log, w, r, http.StatusOK, loginResponse{Token: token})
}

func (h handler) createOrganization(w http.ResponseWriter, r *http.Request) {
	if _, ok := caller.FromContext(r.Context()); !ok {
		h.writeError(w, r, domain.ErrUnauthenticated)
		return
	}

	var body createOrganizationRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeError(w, r, err)
		return
	}

	key, err := idempotencyKey(r)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	created, err := h.auth.CreateOrganization(r.Context(), body.Name, key)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	writeJSON(h.log, w, r, http.StatusCreated, createOrganizationResponse{
		OrganizationID: created.OrganizationID,
		Token:          created.Token,
	})
}

func (h handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := bearerToken(r.Header.Get("Authorization"))
		if err != nil {
			h.writeError(w, r, err)
			return
		}

		identity, err := h.auth.ValidateToken(r.Context(), token)
		if err != nil {
			h.writeError(w, r, err)
			return
		}

		ctx := caller.NewContext(r.Context(), caller.Caller{
			SubjectID: identity.SubjectID,
			Kind:      identity.Kind,
			Roles:     identity.Roles,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func idempotencyKey(r *http.Request) (string, error) {
	key := strings.TrimSpace(r.Header.Get(idempotencyHeader))
	if key == "" {
		return "", domain.ErrInvalidArgument
	}

	return key, nil
}

func bearerToken(header string) (string, error) {
	const scheme = "Bearer "
	if len(header) < len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return "", domain.ErrUnauthenticated
	}

	token := strings.TrimSpace(header[len(scheme):])
	if token == "" {
		return "", domain.ErrUnauthenticated
	}

	return token, nil
}
