package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

	subscriptions := &fakeSubscriptions{}
	body := `{"payment_id":"11111111-1111-1111-1111-111111111111","subscription_id":"22222222-2222-2222-2222-222222222222","amount_minor":100,"occurred_at":"2026-10-04T12:00:00Z"}`
	ok := postJSON(t, newRouterFull(t, &fakeAuth{}, subscriptions, &stubGenerator{}), "/webhooks/payments", body,
		map[string]string{"X-Webhook-Key": webhookKey},
	)
	require.Equal(t, http.StatusAccepted, ok.Code)
	require.Empty(t, ok.Body.Bytes())
	require.Equal(t, 1, subscriptions.paymentCalls)
	require.Equal(t, "11111111-1111-1111-1111-111111111111", subscriptions.paymentID)
	require.Equal(t, int64(100), subscriptions.paymentAmount)

	broken := postJSON(t, newRouter(t, &fakeAuth{}), "/webhooks/payments", `{"payment_id":"not-a-uuid"}`,
		map[string]string{"X-Webhook-Key": webhookKey},
	)
	require.Equal(t, http.StatusBadRequest, broken.Code)
	require.Equal(t, "invalid_argument", errorCode(t, broken))

	subscriptions.paymentErr = domain.ErrNotFound
	notFound := postJSON(t, newRouterFull(t, &fakeAuth{}, subscriptions, &stubGenerator{}), "/webhooks/payments", body,
		map[string]string{"X-Webhook-Key": webhookKey},
	)
	require.Equal(t, http.StatusNotFound, notFound.Code)

	subscriptions.paymentErr = domain.ErrConflict
	conflict := postJSON(t, newRouterFull(t, &fakeAuth{}, subscriptions, &stubGenerator{}), "/webhooks/payments", body,
		map[string]string{"X-Webhook-Key": webhookKey},
	)
	require.Equal(t, http.StatusConflict, conflict.Code)

	subscriptions.paymentErr = domain.ErrUnavailable
	unavailable := postJSON(t, newRouterFull(t, &fakeAuth{}, subscriptions, &stubGenerator{}), "/webhooks/payments", body,
		map[string]string{"X-Webhook-Key": webhookKey},
	)
	require.Equal(t, http.StatusServiceUnavailable, unavailable.Code)
}

func TestPaymentWebhookRejectsLargeBody(t *testing.T) {
	t.Parallel()

	const maxBody = 1 << 20

	req := httptest.NewRequest(http.MethodPost, "/webhooks/payments", strings.NewReader(strings.Repeat("a", maxBody+1)))
	req.Header.Set("Content-Type", "application/json")
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

type fakeSubscriptions struct {
	tariff      app.Tariff
	tariffErr   error
	tariffCalls int
	tariffCtx   context.Context
	tariffName  string
	tariffKey   string
	tariffPrice int64
	tariffLimit int32
	tariffType  string
	tariffBase  bool
	list        []app.Tariff
	sub         app.Subscription
	subErr      error
	subCalls    int
	subKey      string
	subTariffID string
	subID       string

	paymentCalls          int
	paymentErr            error
	paymentID             string
	paymentSubscriptionID string
	paymentAmount         int64
	paymentOccurredAt     time.Time
}

func (f *fakeSubscriptions) CreateTariff(ctx context.Context, idempotencyKey, name string, price int64, limit int32, kind string, base bool) (app.Tariff, error) {
	f.tariffCalls++
	f.tariffCtx = ctx
	f.tariffKey = idempotencyKey
	f.tariffName = name
	f.tariffPrice = price
	f.tariffLimit = limit
	f.tariffType = kind
	f.tariffBase = base

	return f.tariff, f.tariffErr
}

func (f *fakeSubscriptions) ListTariffs(context.Context) ([]app.Tariff, error) {
	return f.list, f.tariffErr
}

func (f *fakeSubscriptions) CreateSubscription(_ context.Context, idempotencyKey, tariffID string) (app.Subscription, error) {
	f.subCalls++
	f.subKey = idempotencyKey
	f.subTariffID = tariffID

	return f.sub, f.subErr
}

func (f *fakeSubscriptions) ChangeSubscription(_ context.Context, idempotencyKey, subscriptionID, tariffID string) (app.Subscription, error) {
	f.subCalls++
	f.subKey = idempotencyKey
	f.subID = subscriptionID
	f.subTariffID = tariffID

	return f.sub, f.subErr
}

func (f *fakeSubscriptions) GetSubscription(_ context.Context, subscriptionID string) (app.Subscription, error) {
	f.subCalls++
	f.subID = subscriptionID

	return f.sub, f.subErr
}

func (f *fakeSubscriptions) CheckSubscription(context.Context) (app.SubscriptionStatus, error) {
	return app.SubscriptionStatus{}, nil
}

func (f *fakeSubscriptions) ProcessPayment(_ context.Context, paymentID, subscriptionID string, amountMinor int64, occurredAt time.Time) error {
	f.paymentCalls++
	f.paymentID = paymentID
	f.paymentSubscriptionID = subscriptionID
	f.paymentAmount = amountMinor
	f.paymentOccurredAt = occurredAt

	return f.paymentErr
}

func TestGenerate(t *testing.T) {
	t.Parallel()

	generator := &stubGenerator{result: app.Generated{Status: "processed", Text: "emulated response", Tokens: 6}}
	auth := &fakeAuth{identity: app.Identity{SubjectID: "user-1", Kind: "user", Roles: []string{"user"}}}
	key := "11111111-1111-1111-1111-111111111111"
	rec := postJSON(t, newRouterWithGenerator(t, auth, generator), "/api/v1/generate",
		`{"prompt":"hello"}`,
		map[string]string{"Authorization": "Bearer good", "Idempotency-Key": key},
	)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, generator.calls)
	require.Equal(t, "hello", generator.prompt)
	require.Equal(t, key, generator.key)

	var body struct {
		Status string `json:"status"`
		Text   string `json:"text"`
		Tokens int64  `json:"tokens"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "processed", body.Status)
	require.Equal(t, "emulated response", body.Text)
	require.Equal(t, int64(6), body.Tokens)
}

func TestGenerateRequiresTokenAndKey(t *testing.T) {
	t.Parallel()

	generator := &stubGenerator{}
	router := newRouterWithGenerator(t, &fakeAuth{}, generator)

	missingToken := postJSON(t, router, "/api/v1/generate", `{"prompt":"hello"}`, map[string]string{"Idempotency-Key": "key"})
	require.Equal(t, http.StatusUnauthorized, missingToken.Code)
	require.Equal(t, "unauthenticated", errorCode(t, missingToken))
	require.Zero(t, generator.calls)

	auth := &fakeAuth{identity: app.Identity{SubjectID: "user-1", Kind: "user"}}
	missingKey := postJSON(t, newRouterWithGenerator(t, auth, generator), "/api/v1/generate", `{"prompt":"hello"}`,
		map[string]string{"Authorization": "Bearer good"},
	)
	require.Equal(t, http.StatusBadRequest, missingKey.Code)
	require.Equal(t, "invalid_argument", errorCode(t, missingKey))
	require.Zero(t, generator.calls)
}

func TestGenerateMapsDenial(t *testing.T) {
	t.Parallel()

	auth := &fakeAuth{identity: app.Identity{SubjectID: "user-1", Kind: "user"}}
	inactive := &stubGenerator{err: domain.ErrSubscriptionInactive}
	rec := postJSON(t, newRouterWithGenerator(t, auth, inactive), "/api/v1/generate", `{"prompt":"hello"}`,
		map[string]string{"Authorization": "Bearer good", "Idempotency-Key": "11111111-1111-1111-1111-111111111111"},
	)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "subscription_inactive", errorCode(t, rec))

	limited := &stubGenerator{err: domain.ErrLimitExceeded}
	rec = postJSON(t, newRouterWithGenerator(t, auth, limited), "/api/v1/generate", `{"prompt":"hello"}`,
		map[string]string{"Authorization": "Bearer good", "Idempotency-Key": "11111111-1111-1111-1111-111111111111"},
	)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "limit_exceeded", errorCode(t, rec))

	conflict := &stubGenerator{err: domain.ErrConflict}
	rec = postJSON(t, newRouterWithGenerator(t, auth, conflict), "/api/v1/generate", `{"prompt":"hello"}`,
		map[string]string{"Authorization": "Bearer good", "Idempotency-Key": "11111111-1111-1111-1111-111111111111"},
	)
	require.Equal(t, http.StatusConflict, rec.Code)
	require.Equal(t, "conflict", errorCode(t, rec))
}

type stubGenerator struct {
	result app.Generated
	err    error
	calls  int
	prompt string
	key    string
}

func (s *stubGenerator) Generate(_ context.Context, prompt, key string) (app.Generated, error) {
	s.calls++
	s.prompt = prompt
	s.key = key

	return s.result, s.err
}

func TestCreateTariffRequiresAdminRoleAtService(t *testing.T) {
	t.Parallel()

	subs := &fakeSubscriptions{tariffErr: domain.ErrForbidden}
	auth := &fakeAuth{identity: app.Identity{SubjectID: "user-1", Kind: "user", Roles: []string{"user"}}}
	rec := postJSON(t, newRouterWith(t, auth, subs), "/api/v1/tariffs",
		`{"name":"base","monthly_price_minor":0,"message_limit":10,"type":"b2c","is_base_tariff":true}`,
		map[string]string{"Authorization": "Bearer good", "Idempotency-Key": "tariff-1"},
	)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "forbidden", errorCode(t, rec))
	require.Equal(t, 1, subs.tariffCalls)
}

func TestCreateTariffAndSubscription(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	subs := &fakeSubscriptions{
		tariff: app.Tariff{ID: "tariff-1", Name: "base", MonthlyPriceMinor: 0, MessageLimit: 10, Type: "b2c", IsBase: true},
		sub: app.Subscription{
			ID: "sub-1", TariffID: "tariff-1", Status: "active", MessageAllowance: 10,
			PeriodStart: start, PeriodEnd: start.AddDate(0, 1, 0),
		},
	}
	auth := &fakeAuth{identity: app.Identity{SubjectID: "org-1", Kind: "organization", Roles: []string{"admin"}}}
	router := newRouterWith(t, auth, subs)

	created := postJSON(t, router, "/api/v1/tariffs",
		`{"name":"base","monthly_price_minor":0,"message_limit":10,"type":"b2c","is_base_tariff":true}`,
		map[string]string{"Authorization": "Bearer good", "Idempotency-Key": "tariff-1"},
	)
	require.Equal(t, http.StatusCreated, created.Code)
	require.Equal(t, "base", subs.tariffName)
	require.Equal(t, "tariff-1", subs.tariffKey)
	require.True(t, subs.tariffBase)
	got, ok := caller.FromContext(subs.tariffCtx)
	require.True(t, ok)
	require.Equal(t, "org-1", got.SubjectID)
	require.Equal(t, []string{"admin"}, got.Roles)

	subscribed := postJSON(t, router, "/api/v1/subscriptions", `{"tariff_id":"tariff-1"}`,
		map[string]string{"Authorization": "Bearer good", "Idempotency-Key": "sub-1"},
	)
	require.Equal(t, http.StatusCreated, subscribed.Code)
	require.Equal(t, "sub-1", subs.subKey)
	require.Equal(t, "tariff-1", subs.subTariffID)

	var body struct {
		ID               string `json:"id"`
		MessageAllowance int64  `json:"message_allowance"`
		Status           string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(subscribed.Body.Bytes(), &body))
	require.Equal(t, "sub-1", body.ID)
	require.Equal(t, int64(10), body.MessageAllowance)
	require.Equal(t, "active", body.Status)
}

func newRouterWith(t *testing.T, auth app.Auth, subscriptions app.Subscriptions) http.Handler {
	t.Helper()

	return newRouterFull(t, auth, subscriptions, &stubGenerator{})
}

func newRouterWithGenerator(t *testing.T, auth app.Auth, generator app.Generator) http.Handler {
	t.Helper()

	return newRouterFull(t, auth, &fakeSubscriptions{}, generator)
}

func newRouterFull(t *testing.T, auth app.Auth, subscriptions app.Subscriptions, generator app.Generator) http.Handler {
	t.Helper()

	log, err := logger.New(io.Discard, "error")
	require.NoError(t, err)

	return httpapi.NewRouter(log, auth, subscriptions, generator, webhookKey)
}

func newRouter(t *testing.T, auth app.Auth) http.Handler {
	t.Helper()

	return newRouterFull(t, auth, &fakeSubscriptions{}, &stubGenerator{})
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
