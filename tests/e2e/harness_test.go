//go:build integration

package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/tests/internal/httpjson"
	"github.com/trb1maker/subscriptions/tests/internal/stack"
)

var live *stack.Stack

func TestMain(m *testing.M) {
	code := 1
	func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		started, err := stack.Start(ctx, stack.Config{
			ExpireSchedule: "@every 1s",
			Gateways: []stack.Gateway{
				{FailurePercent: 0},
				{FailurePercent: 100},
			},
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)

			return
		}

		defer started.Close(ctx)
		live = started
		code = m.Run()
		if code != 0 {
			fmt.Fprintln(os.Stderr, started.Logs())
		}
	}()
	os.Exit(code)
}

func postJSON(t *testing.T, ctx context.Context, rawURL string, header map[string]string, body any, status int) map[string]any {
	t.Helper()

	return callJSON(t, ctx, http.MethodPost, rawURL, header, body, status)
}

func putJSON(t *testing.T, ctx context.Context, rawURL string, header map[string]string, body any, status int) map[string]any {
	t.Helper()

	return callJSON(t, ctx, http.MethodPut, rawURL, header, body, status)
}

func getJSON(t *testing.T, ctx context.Context, rawURL string, header map[string]string, status int) map[string]any {
	t.Helper()

	return callJSON(t, ctx, http.MethodGet, rawURL, header, nil, status)
}

func callJSON(t *testing.T, ctx context.Context, method, rawURL string, header map[string]string, body any, status int) map[string]any {
	t.Helper()

	resp, err := httpjson.Do(ctx, method, rawURL, header, body)
	require.NoError(t, err)
	require.Equal(t, status, resp.Status, resp.Raw)

	return resp.Body
}

func redisValue(t *testing.T, ctx context.Context, key string) int64 {
	t.Helper()

	value, err := live.Redis.Get(ctx, key).Result()
	if err != nil {
		return -1
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return -1
	}

	return parsed
}

func usageRow(t *testing.T, ctx context.Context, eventID string) (int64, string) {
	t.Helper()

	var tokens int64
	var outcome string
	err := live.Click.QueryRowContext(ctx,
		`SELECT tokens, outcome FROM usage.token_usage FINAL WHERE event_id = toUUID(?)`, eventID,
	).Scan(&tokens, &outcome)
	if err != nil {
		return 0, ""
	}

	return tokens, outcome
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
