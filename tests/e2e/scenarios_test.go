//go:build integration

package e2e_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/tests/internal/stack"
)

const baseMessageLimit = 4

var (
	baseMu    sync.Mutex
	baseReady bool
	baseID    string
)

func TestTariffChangeCarriesPaidAllowance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	base := live.Gateways[0]
	_, userToken := registerUser(t, ctx, base)
	orgToken := organizationToken(t, ctx, base, userToken)
	currentID := createTariff(t, ctx, base, orgToken, "paid-from", 100, 10, "b2c", false)
	nextID := createTariff(t, ctx, base, orgToken, "paid-to", 200, 30, "b2c", false)
	created := createSubscription(t, ctx, base, userToken, currentID)

	changed := putJSON(t, ctx, base+"/api/v1/subscriptions/"+created["id"].(string), withKey(bearer(userToken), uuid.New().String()), map[string]string{
		"tariff_id": nextID,
	}, http.StatusOK)
	require.Equal(t, nextID, changed["tariff_id"])
	require.Equal(t, float64(40), changed["message_allowance"])
	require.Equal(t, created["current_period_start"], changed["current_period_start"])
	require.Equal(t, created["current_period_end"], changed["current_period_end"])
}

func TestTariffChangeBurnsBaseAllowance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	base := live.Gateways[0]
	_, userToken := registerUser(t, ctx, base)
	orgToken := organizationToken(t, ctx, base, userToken)
	ensureBase(t, ctx, base, orgToken)
	paidID := createTariff(t, ctx, base, orgToken, "paid-after-base", 100, 20, "b2c", false)
	created := createSubscription(t, ctx, base, userToken, baseID)
	require.Equal(t, float64(baseMessageLimit), created["message_allowance"])

	changed := putJSON(t, ctx, base+"/api/v1/subscriptions/"+created["id"].(string), withKey(bearer(userToken), uuid.New().String()), map[string]string{
		"tariff_id": paidID,
	}, http.StatusOK)
	require.Equal(t, paidID, changed["tariff_id"])
	require.Equal(t, float64(20), changed["message_allowance"])
	require.Equal(t, created["current_period_start"], changed["current_period_start"])
	require.Equal(t, created["current_period_end"], changed["current_period_end"])
}

func TestSubscriptionExpiryMovesB2CToBase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	base := live.Gateways[0]
	_, userToken := registerUser(t, ctx, base)
	orgToken := organizationToken(t, ctx, base, userToken)
	ensureBase(t, ctx, base, orgToken)
	paidID := createTariff(t, ctx, base, orgToken, "paid-expiring", 100, 10, "b2c", false)
	created := createSubscription(t, ctx, base, userToken, paidID)
	subscriptionID := created["id"].(string)
	require.NoError(t, live.FinishPeriod(ctx, subscriptionID))

	var expired map[string]any
	require.Eventually(t, func() bool {
		expired = getJSON(t, ctx, base+"/api/v1/subscriptions/"+subscriptionID, bearer(userToken), http.StatusOK)

		return expired["tariff_id"] == baseID && expired["status"] == "active"
	}, 20*time.Second, 200*time.Millisecond)
	require.Equal(t, float64(10+baseMessageLimit), expired["message_allowance"])
	require.NotEqual(t, created["current_period_end"], expired["current_period_end"])
}

func TestSubscriptionExpiryBurnsOrganizationLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	gateway := live.Gateways[0]
	_, adminToken := registerUser(t, ctx, gateway)
	orgID, orgToken := createOrganization(t, ctx, gateway, adminToken)
	_, memberToken := registerMember(t, ctx, gateway, orgID)
	tariffID := createTariff(t, ctx, gateway, orgToken, "team-expiring", 500, 6, "b2b", false)
	created := createSubscription(t, ctx, gateway, orgToken, tariffID)
	subscriptionID := created["id"].(string)
	pay(t, ctx, gateway, subscriptionID, 500)
	require.Eventually(t, func() bool {
		return redisValue(t, ctx, "limit:org:"+orgID) == 6
	}, 20*time.Second, 100*time.Millisecond)

	require.NoError(t, live.FinishPeriod(ctx, subscriptionID))
	require.Eventually(t, func() bool {
		got := getJSON(t, ctx, gateway+"/api/v1/subscriptions/"+subscriptionID, bearer(orgToken), http.StatusOK)

		return got["status"] == "expired"
	}, 20*time.Second, 200*time.Millisecond)
	require.Eventually(t, func() bool {
		return redisValue(t, ctx, "limit:org:"+orgID) == 0
	}, 20*time.Second, 100*time.Millisecond)

	denied := postJSON(t, ctx, gateway+"/api/v1/generate", withKey(bearer(memberToken), uuid.New().String()), map[string]string{"prompt": "after"}, http.StatusForbidden)
	require.Equal(t, "subscription_inactive", denied["error"])
}

func TestOrganizationMembersShareLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	gateway := live.Gateways[0]
	_, adminToken := registerUser(t, ctx, gateway)
	orgID, orgToken := createOrganization(t, ctx, gateway, adminToken)
	firstID, firstToken := registerMember(t, ctx, gateway, orgID)
	secondID, secondToken := registerMember(t, ctx, gateway, orgID)
	tariffID := createTariff(t, ctx, gateway, orgToken, "team-shared", 800, 5, "b2b", false)
	created := createSubscription(t, ctx, gateway, orgToken, tariffID)
	pay(t, ctx, gateway, created["id"].(string), 800)
	require.Eventually(t, func() bool {
		return redisValue(t, ctx, "limit:org:"+orgID) == 5
	}, 20*time.Second, 100*time.Millisecond)
	time.Sleep(time.Second)

	for _, token := range []string{firstToken, secondToken} {
		eventID := uuid.New().String()
		generated := postJSON(t, ctx, gateway+"/api/v1/generate", withKey(bearer(token), eventID), map[string]string{"prompt": "shared"}, http.StatusOK)
		require.Equal(t, "processed", generated["status"])
		require.Eventually(t, func() bool {
			_, outcome := usageRow(t, ctx, eventID)

			return outcome == "processed"
		}, 20*time.Second, 100*time.Millisecond)
	}

	require.Eventually(t, func() bool {
		return redisValue(t, ctx, "limit:org:"+orgID) == 3
	}, 20*time.Second, 200*time.Millisecond)
	require.Equal(t, int64(-1), redisValue(t, ctx, "limit:user:"+firstID))
	require.Equal(t, int64(-1), redisValue(t, ctx, "limit:user:"+secondID))
}

func registerUser(t *testing.T, ctx context.Context, gateway string) (string, string) {
	t.Helper()

	registered := postJSON(t, ctx, gateway+"/api/v1/users", map[string]string{"Idempotency-Key": uuid.New().String()}, map[string]string{
		"email": uuid.New().String() + "@example.com", "password": "password1",
	}, http.StatusCreated)

	return registered["user_id"].(string), registered["token"].(string)
}

func organizationToken(t *testing.T, ctx context.Context, gateway, userToken string) string {
	t.Helper()

	_, token := createOrganization(t, ctx, gateway, userToken)

	return token
}

func createOrganization(t *testing.T, ctx context.Context, gateway, userToken string) (string, string) {
	t.Helper()

	organization := postJSON(t, ctx, gateway+"/api/v1/organizations", withKey(bearer(userToken), uuid.New().String()), map[string]string{
		"name": "Org " + uuid.New().String(),
	}, http.StatusCreated)

	return organization["organization_id"].(string), organization["token"].(string)
}

func registerMember(t *testing.T, ctx context.Context, gateway, orgID string) (string, string) {
	t.Helper()

	registered := postJSON(t, ctx, gateway+"/api/v1/users", map[string]string{"Idempotency-Key": uuid.New().String()}, map[string]string{
		"email": uuid.New().String() + "@example.com", "password": "password1", "organization_id": orgID,
	}, http.StatusCreated)

	return registered["user_id"].(string), registered["token"].(string)
}

func createTariff(t *testing.T, ctx context.Context, gateway, orgToken, name string, price, limit int, kind string, base bool) string {
	t.Helper()

	tariff := postJSON(t, ctx, gateway+"/api/v1/tariffs", withKey(bearer(orgToken), uuid.New().String()), map[string]any{
		"name": name + "-" + uuid.New().String(), "monthly_price_minor": price, "message_limit": limit, "type": kind, "is_base_tariff": base,
	}, http.StatusCreated)

	return tariff["id"].(string)
}

func createSubscription(t *testing.T, ctx context.Context, gateway, token, tariffID string) map[string]any {
	t.Helper()

	return postJSON(t, ctx, gateway+"/api/v1/subscriptions", withKey(bearer(token), uuid.New().String()), map[string]string{
		"tariff_id": tariffID,
	}, http.StatusCreated)
}

func pay(t *testing.T, ctx context.Context, gateway, subscriptionID string, amount int) {
	t.Helper()

	postJSON(t, ctx, gateway+"/webhooks/payments", map[string]string{"X-Webhook-Key": stack.WebhookKey()}, map[string]any{
		"payment_id":      uuid.New().String(),
		"subscription_id": subscriptionID,
		"amount_minor":    amount,
		"occurred_at":     time.Now().UTC().Format(time.RFC3339Nano),
	}, http.StatusAccepted)
}

func ensureBase(t *testing.T, ctx context.Context, gateway, orgToken string) {
	t.Helper()

	baseMu.Lock()
	defer baseMu.Unlock()

	if baseReady {
		return
	}

	baseID = createTariff(t, ctx, gateway, orgToken, "base", 0, baseMessageLimit, "b2c", true)
	baseReady = true
}
