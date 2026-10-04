package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/pkg/caller"
	"github.com/trb1maker/subscriptions/pkg/logger"
	httpapi "github.com/trb1maker/subscriptions/services/gateway/internal/adapters/http"
	"github.com/trb1maker/subscriptions/services/gateway/internal/app"
	"github.com/trb1maker/subscriptions/services/gateway/internal/domain"
)

const webhookKey = "webhook-secret"

func TestHealth(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	newRouter(t, &fakeAuth{}).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "ok", body.Status)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

func TestRegister(t *testing.T) {
	t.Parallel()

	auth := &fakeAuth{registered: app.Registered{UserID: "user-1", Token: "tok"}}
	rec := postJSON(t, newRouter(t, auth), "/api/v1/users",
		`{"email":"a@b.c","password":"secret","organization_id":"org-1"}`,
		map[string]string{"Idempotency-Key": "key-1"},
	)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, 1, auth.registerCalls)
	require.Equal(t, "a@b.c", auth.email)
	require.Equal(t, "secret", auth.password)
	require.Equal(t, "key-1", auth.key)
	require.Equal(t, "org-1", auth.organizationID)
	require.Zero(t, auth.validateCalls)

	var body struct {
		UserID string `json:"user_id"`
		Token  string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "user-1", body.UserID)
	require.Equal(t, "tok", body.Token)
}

func TestRegisterRequiresIdempotencyKey(t *testing.T) {
	t.Parallel()

	auth := &fakeAuth{}
	rec := postJSON(t, newRouter(t, auth), "/api/v1/users", `{"email":"a@b.c","password":"secret"}`, nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_argument", errorCode(t, rec))
	require.Zero(t, auth.registerCalls)
}

func TestRegisterRejectsUnknownField(t *testing.T) {
	t.Parallel()

	auth := &fakeAuth{}
	rec := postJSON(t, newRouter(t, auth), "/api/v1/users", `{"email":"a@b.c","password":"secret","extra":1}`,
		map[string]string{"Idempotency-Key": "key-1"},
	)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_argument", errorCode(t, rec))
	require.Zero(t, auth.registerCalls)
}

func TestLogin(t *testing.T) {
	t.Parallel()

	auth := &fakeAuth{loginToken: "tok"}
	rec := postJSON(t, newRouter(t, auth), "/api/v1/login", `{"email":"a@b.c","password":"secret"}`, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, auth.loginCalls)

	var body struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "tok", body.Token)
}

func TestLoginUnauthenticated(t *testing.T) {
	t.Parallel()

	auth := &fakeAuth{loginErr: domain.ErrUnauthenticated}
	rec := postJSON(t, newRouter(t, auth), "/api/v1/login", `{"email":"a@b.c","password":"secret"}`, nil)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "unauthenticated", errorCode(t, rec))
}

func TestCreateOrganizationRequiresBearer(t *testing.T) {
	t.Parallel()

	auth := &fakeAuth{}
	rec := postJSON(t, newRouter(t, auth), "/api/v1/organizations", `{"name":"acme"}`,
		map[string]string{"Idempotency-Key": "org-1"},
	)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "unauthenticated", errorCode(t, rec))
	require.Zero(t, auth.validateCalls)
	require.Zero(t, auth.orgCalls)
}

func TestCreateOrganizationRejectsInvalidToken(t *testing.T) {
	t.Parallel()

	auth := &fakeAuth{validateErr: domain.ErrUnauthenticated}
	rec := postJSON(t, newRouter(t, auth), "/api/v1/organizations", `{"name":"acme"}`, map[string]string{
		"Idempotency-Key": "org-1",
		"Authorization":   "Bearer bad",
	})

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, 1, auth.validateCalls)
	require.Zero(t, auth.orgCalls)
}

func TestCreateOrganization(t *testing.T) {
	t.Parallel()

	auth := &fakeAuth{
		identity:  app.Identity{SubjectID: "user-1", Kind: "user", Roles: []string{"admin"}},
		orgResult: app.OrganizationCreated{OrganizationID: "org-1", Token: "org-tok"},
	}
	rec := postJSON(t, newRouter(t, auth), "/api/v1/organizations", `{"name":"acme"}`, map[string]string{
		"Idempotency-Key": "org-1",
		"Authorization":   "Bearer good",
	})

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, 1, auth.validateCalls)
	require.Equal(t, 1, auth.orgCalls)
	require.Equal(t, "acme", auth.name)
	require.Equal(t, "org-1", auth.key)

	got, ok := caller.FromContext(auth.orgCtx)
	require.True(t, ok)
	require.Equal(t, "user-1", got.SubjectID)
	require.Equal(t, "user", got.Kind)
	require.Equal(t, []string{"admin"}, got.Roles)
	require.NotEmpty(t, logger.RequestID(auth.orgCtx))

	var body struct {
		OrganizationID string `json:"organization_id"`
		Token          string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "org-1", body.OrganizationID)
	require.Equal(t, "org-tok", body.Token)
}

func TestCreateOrganizationUnavailable(t *testing.T) {
	t.Parallel()

	auth := &fakeAuth{validateErr: domain.ErrUnavailable}
	rec := postJSON(t, newRouter(t, auth), "/api/v1/organizations", `{"name":"acme"}`, map[string]string{
		"Authorization": "Bearer good",
	})

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Equal(t, "unavailable", errorCode(t, rec))
	require.Zero(t, auth.orgCalls)
}

func TestMapsAuthErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "not found", err: domain.ErrNotFound, status: http.StatusNotFound, code: "not_found"},
		{name: "conflict", err: domain.ErrConflict, status: http.StatusConflict, code: "conflict"},
		{name: "internal", err: domain.ErrInternal, status: http.StatusInternalServerError, code: "internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			auth := &fakeAuth{registerErr: tt.err}
			rec := postJSON(t, newRouter(t, auth), "/api/v1/users", `{"email":"a@b.c","password":"secret"}`,
				map[string]string{"Idempotency-Key": "key-1"},
			)

			require.Equal(t, tt.status, rec.Code)
			require.Equal(t, tt.code, errorCode(t, rec))
		})
	}
}

func TestPaymentWebhook(t *testing.T) {
	t.Parallel()

	router := newRouter(t, &fakeAuth{})

	missing := postRaw(t, router, "/webhooks/payments", nil)
	require.Equal(t, http.StatusUnauthorized, missing.Code)
	require.Equal(t, "unauthenticated", errorCode(t, missing))

	wrong := postRaw(t, router, "/webhooks/payments", map[string]string{"X-Webhook-Key": "other"})
	require.Equal(t, http.StatusUnauthorized, wrong.Code)

	ok := postRaw(t, router, "/webhooks/payments", map[string]string{"X-Webhook-Key": webhookKey})
	require.Equal(t, http.StatusAccepted, ok.Code)
	require.Empty(t, ok.Body.Bytes())
}

func TestPaymentWebhookRejectsLargeBody(t *testing.T) {
	t.Parallel()

	const maxBody = 1 << 20

	req := httptest.NewRequest(http.MethodPost, "/webhooks/payments", strings.NewReader(strings.Repeat("a", maxBody+1)))
	req.Header.Set("X-Webhook-Key", webhookKey)
	rec := httptest.NewRecorder()
	newRouter(t, &fakeAuth{}).ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_argument", errorCode(t, rec))
}

type fakeAuth struct {
	registered     app.Registered
	registerErr    error
	registerCalls  int
	email          string
	password       string
	key            string
	organizationID string

	loginToken string
	loginErr   error
	loginCalls int

	orgResult app.OrganizationCreated
	orgErr    error
	orgCalls  int
	orgCtx    context.Context
	name      string

	identity      app.Identity
	validateErr   error
	validateCalls int
}

func (f *fakeAuth) Register(_ context.Context, email, password, idempotencyKey, organizationID string) (app.Registered, error) {
	f.registerCalls++
	f.email = email
	f.password = password
	f.key = idempotencyKey
	f.organizationID = organizationID

	return f.registered, f.registerErr
}

func (f *fakeAuth) Login(context.Context, string, string) (string, error) {
	f.loginCalls++

	return f.loginToken, f.loginErr
}

func (f *fakeAuth) CreateOrganization(ctx context.Context, name, idempotencyKey string) (app.OrganizationCreated, error) {
	f.orgCalls++
	f.orgCtx = ctx
	f.name = name
	f.key = idempotencyKey

	return f.orgResult, f.orgErr
}

func (f *fakeAuth) ValidateToken(context.Context, string) (app.Identity, error) {
	f.validateCalls++

	return f.identity, f.validateErr
}

func newRouter(t *testing.T, auth app.Auth) http.Handler {
	t.Helper()

	log, err := logger.New(io.Discard, "error")
	require.NoError(t, err)

	return httpapi.NewRouter(log, auth, webhookKey)
}

func postJSON(t *testing.T, handler http.Handler, path, body string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for key, value := range header {
		req.Header.Set(key, value)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

func postRaw(t *testing.T, handler http.Handler, path string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("ignored"))
	for key, value := range header {
		req.Header.Set(key, value)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var body struct {
		Error string `json:"error"`
	}
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	return body.Error
}
