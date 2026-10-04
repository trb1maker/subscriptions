//go:build integration

package e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"
	"uuid"

	natsserver "github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/trb1maker/subscriptions/api/gen/events/v1"
	natspkg "github.com/trb1maker/subscriptions/pkg/nats"
	"github.com/trb1maker/subscriptions/tests/internal/stack"
)

const allowance int64 = 3

func TestGenerateDebitsClickHouse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	base := live.Gateways[0]
	registered := postJSON(t, ctx, base+"/api/v1/users", map[string]string{"Idempotency-Key": "user-1"}, map[string]string{
		"email": "ada@example.com", "password": "password1",
	}, http.StatusCreated)
	userID := registered["user_id"].(string)
	userToken := registered["token"].(string)

	organization := postJSON(t, ctx, base+"/api/v1/organizations", withKey(bearer(userToken), "org-1"), map[string]string{"name": "Acme"}, http.StatusCreated)
	orgToken := organization["token"].(string)

	tariff := postJSON(t, ctx, base+"/api/v1/tariffs", withKey(bearer(orgToken), "tariff-1"), map[string]any{
		"name": "personal", "monthly_price_minor": 100, "message_limit": 10, "type": "b2c", "is_base_tariff": false,
	}, http.StatusCreated)
	tariffID := tariff["id"].(string)

	postJSON(t, ctx, base+"/api/v1/subscriptions", withKey(bearer(userToken), "sub-1"), map[string]string{"tariff_id": tariffID}, http.StatusCreated)

	require.Eventually(t, func() bool {
		return publishAllowance(t, userID) == nil
	}, 20*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool {
		return redisValue(t, ctx, "limit:user:"+userID) == allowance
	}, 20*time.Second, 100*time.Millisecond)
	time.Sleep(time.Second)

	eventID := uuid.New().String()
	generated := postJSON(t, ctx, base+"/api/v1/generate", withKey(bearer(userToken), eventID), map[string]string{"prompt": "hello"}, http.StatusOK)
	require.Equal(t, "processed", generated["status"])
	require.NotEmpty(t, generated["text"])
	tokens := int64(generated["tokens"].(float64))
	require.Positive(t, tokens)

	require.Eventually(t, func() bool {
		got, outcome := usageRow(t, ctx, eventID)

		return outcome == "processed" && got == tokens
	}, 20*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool {
		return redisValue(t, ctx, "limit:user:"+userID) == allowance-1
	}, 20*time.Second, 100*time.Millisecond)

	failedID := uuid.New().String()
	failed := postJSON(t, ctx, live.Gateways[1]+"/api/v1/generate", withKey(bearer(userToken), failedID), map[string]string{"prompt": "fail"}, http.StatusOK)
	require.Equal(t, "failed", failed["status"])
	require.Empty(t, failed["text"])
	failedTokens := int64(failed["tokens"].(float64))
	require.Positive(t, failedTokens)
	require.Eventually(t, func() bool {
		got, outcome := usageRow(t, ctx, failedID)

		return outcome == "failed" && got == failedTokens && redisValue(t, ctx, "limit:user:"+userID) == allowance-1
	}, 20*time.Second, 100*time.Millisecond)

	payer := postJSON(t, ctx, base+"/api/v1/users", map[string]string{"Idempotency-Key": "user-2"}, map[string]string{
		"email": "grace@example.com", "password": "password1",
	}, http.StatusCreated)
	payerID := payer["user_id"].(string)
	payerToken := payer["token"].(string)
	created := postJSON(t, ctx, base+"/api/v1/subscriptions", withKey(bearer(payerToken), "sub-2"), map[string]string{"tariff_id": tariffID}, http.StatusCreated)
	subscriptionID := created["id"].(string)
	paymentID := uuid.New().String()
	webhook := map[string]any{
		"payment_id":      paymentID,
		"subscription_id": subscriptionID,
		"amount_minor":    100,
		"occurred_at":     time.Now().UTC().Format(time.RFC3339Nano),
	}
	postJSON(t, ctx, base+"/webhooks/payments", map[string]string{"X-Webhook-Key": stack.WebhookKey()}, webhook, http.StatusAccepted)
	require.Eventually(t, func() bool {
		return redisValue(t, ctx, "limit:user:"+payerID) == 10
	}, 20*time.Second, 100*time.Millisecond)

	paid := getJSON(t, ctx, base+"/api/v1/subscriptions/"+subscriptionID, bearer(payerToken), http.StatusOK)
	require.Equal(t, float64(10), paid["message_allowance"])
	periodEnd := paid["current_period_end"].(string)
	require.NotEqual(t, created["current_period_end"], periodEnd)

	postJSON(t, ctx, base+"/webhooks/payments", map[string]string{"X-Webhook-Key": stack.WebhookKey()}, webhook, http.StatusAccepted)
	replayed := getJSON(t, ctx, base+"/api/v1/subscriptions/"+subscriptionID, bearer(payerToken), http.StatusOK)
	require.Equal(t, periodEnd, replayed["current_period_end"])
	require.Equal(t, int64(10), redisValue(t, ctx, "limit:user:"+payerID))
}

func publishAllowance(t *testing.T, userID string) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	conn, jetStream, err := natspkg.Connect(ctx, live.NATSURL)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Drain() }()

	eventID := uuid.New()
	when := time.Now().UTC().Truncate(time.Millisecond)
	body, err := proto.Marshal(&eventsv1.UsageEvent{
		EventId:    eventID.String(),
		OccurredAt: when.Format(time.RFC3339Nano),
		OwnerKind:  "user",
		OwnerId:    userID,
		Kind: &eventsv1.UsageEvent_PaymentReceived{PaymentReceived: &eventsv1.PaymentReceived{
			Allowance: allowance, PaymentId: "pay-e2e", AmountMinor: 100,
		}},
	})
	if err != nil {
		return err
	}

	_, err = jetStream.Publish(ctx, "usage.owner.user."+userID, body, natsserver.WithMsgID(eventID.String()))

	return err
}
