//go:build load

package load_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/tests/internal/httpjson"
	"github.com/trb1maker/subscriptions/tests/internal/stack"
)

const (
	owners         = 3
	allowance      = 30
	workers        = 4
	loadFor        = 2 * time.Second
	failurePercent = 25
	settleFor      = 90 * time.Second
)

func TestLimitsMatchEventsUnderLoad(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	started, err := stack.Start(ctx, stack.Config{
		Gateways: []stack.Gateway{{FailurePercent: failurePercent}},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(started.Logs())
		}

		started.Close()
	})

	gateway := started.Gateways[0]
	_, adminToken := register(t, ctx, gateway, "")
	organization := call(t, ctx, http.MethodPost, gateway+"/api/v1/organizations", withKey(bearer(adminToken), uuid.New().String()), map[string]string{
		"name": "Load " + uuid.New().String(),
	}, http.StatusCreated)
	orgToken := organization["token"].(string)
	tariff := call(t, ctx, http.MethodPost, gateway+"/api/v1/tariffs", withKey(bearer(orgToken), uuid.New().String()), map[string]any{
		"name": "load", "monthly_price_minor": 100, "message_limit": allowance, "type": "b2c", "is_base_tariff": false,
	}, http.StatusCreated)
	tariffID := tariff["id"].(string)

	userIDs := make([]string, 0, owners)
	tokens := make([]string, 0, owners)
	for range owners {
		userID, token := register(t, ctx, gateway, "")
		created := call(t, ctx, http.MethodPost, gateway+"/api/v1/subscriptions", withKey(bearer(token), uuid.New().String()), map[string]string{
			"tariff_id": tariffID,
		}, http.StatusCreated)
		call(t, ctx, http.MethodPost, gateway+"/webhooks/payments", map[string]string{"X-Webhook-Key": stack.WebhookKey()}, map[string]any{
			"payment_id":      uuid.New().String(),
			"subscription_id": created["id"],
			"amount_minor":    100,
			"occurred_at":     time.Now().UTC().Format(time.RFC3339Nano),
		}, http.StatusAccepted)
		userIDs = append(userIDs, userID)
		tokens = append(tokens, token)
	}

	require.Eventually(t, func() bool {
		for _, userID := range userIDs {
			if redisValue(ctx, started, "limit:user:"+userID) != allowance {
				return false
			}
		}

		return true
	}, 20*time.Second, 100*time.Millisecond)
	time.Sleep(time.Second)

	var mu sync.Mutex
	processed := map[string]int{}
	failed := map[string]int{}
	var wg sync.WaitGroup
	deadline := time.Now().Add(loadFor)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for time.Now().Before(deadline) {
				index := rand.IntN(len(tokens))
				userID := userIDs[index]
				eventID := uuid.New().String()
				reqCtx, reqCancel := context.WithTimeout(ctx, 5*time.Second)
				resp, reqErr := httpjson.Do(reqCtx, http.MethodPost, gateway+"/api/v1/generate", withKey(bearer(tokens[index]), eventID), map[string]string{
					"prompt": eventID,
				})
				reqCancel()
				if reqErr != nil || resp.Status != http.StatusOK {
					continue
				}

				mu.Lock()
				switch resp.Body["status"] {
				case "processed":
					processed[userID]++
				case "failed":
					failed[userID]++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	var processedTotal, failedTotal int
	for _, userID := range userIDs {
		processedTotal += processed[userID]
		failedTotal += failed[userID]
	}

	require.Positive(t, processedTotal)
	require.Positive(t, failedTotal)

	settle := time.Now().Add(settleFor)
	var detail string
	for {
		detail = ""
		ready := true
		for _, userID := range userIDs {
			gotProcessed, gotFailed, countErr := usageCounts(ctx, started, userID)
			limit := redisValue(ctx, started, "limit:user:"+userID)
			want := int64(allowance - processed[userID])
			if countErr != nil || gotProcessed != processed[userID] || gotFailed != failed[userID] || limit != want {
				ready = false
				detail += fmt.Sprintf("%s http %d/%d ch %d/%d redis %d want %d err %v; ",
					userID, processed[userID], failed[userID], gotProcessed, gotFailed, limit, want, countErr)
			}
		}

		if ready {
			break
		}

		if time.Now().After(settle) {
			t.Fatal(detail)
		}

		time.Sleep(200 * time.Millisecond)
	}
}

func register(t *testing.T, ctx context.Context, gateway, organizationID string) (string, string) {
	t.Helper()

	body := map[string]string{
		"email": uuid.New().String() + "@example.com", "password": "password1",
	}
	if organizationID != "" {
		body["organization_id"] = organizationID
	}

	registered := call(t, ctx, http.MethodPost, gateway+"/api/v1/users", map[string]string{"Idempotency-Key": uuid.New().String()}, body, http.StatusCreated)

	return registered["user_id"].(string), registered["token"].(string)
}

func call(t *testing.T, ctx context.Context, method, rawURL string, header map[string]string, body any, status int) map[string]any {
	t.Helper()

	resp, err := httpjson.Do(ctx, method, rawURL, header, body)
	require.NoError(t, err)
	require.Equal(t, status, resp.Status, resp.Raw)

	return resp.Body
}

func redisValue(ctx context.Context, started *stack.Stack, key string) int64 {
	value, err := started.Redis.Get(ctx, key).Int64()
	if err != nil {
		return -1
	}

	return value
}

func usageCounts(ctx context.Context, started *stack.Stack, userID string) (int, int, error) {
	var processed, failed uint64
	err := started.Click.QueryRowContext(ctx, `
		SELECT countIf(outcome = 'processed'), countIf(outcome = 'failed')
		FROM usage.token_usage FINAL
		WHERE owner_id = toUUID(?)`, userID,
	).Scan(&processed, &failed)
	if err != nil {
		return 0, 0, err
	}

	return int(processed), int(failed), nil
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func withKey(header map[string]string, key string) map[string]string {
	copied := make(map[string]string, len(header)+1)
	for name, value := range header {
		copied[name] = value
	}

	copied["Idempotency-Key"] = key

	return copied
}
