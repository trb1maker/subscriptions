// Command demo создаёт нагрузку на уже поднятый стенд и сам инфраструктуру не запускает.
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
	"uuid"

	"github.com/trb1maker/subscriptions/tests/internal/httpjson"
)

func main() {
	os.Exit(run())
}

func run() int {
	gateway := flag.String("gateway", "http://127.0.0.1:8080", "базовый URL Gateway")
	webhookKey := flag.String("webhook-key", "webhook-secret", "значение заголовка X-Webhook-Key")
	duration := flag.Duration("duration", 2*time.Minute, "длительность параллельной генерации")
	concurrency := flag.Int("concurrency", 8, "число параллельных генераций")
	flag.Parse()

	ctx := context.Background()
	if err := waitHealthy(ctx, *gateway); err != nil {
		fmt.Fprintf(os.Stderr, "gateway не ответил на /health: %v\nПоднимите стенд: task demo:up\n", err)

		return 1
	}

	users, err := prepareReady(ctx, *gateway, *webhookKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "сценарий не подготовлен: %v\n", err)

		return 1
	}

	fmt.Printf("генерация %s, параллелизм %d\n", *duration, *concurrency)
	total := generate(ctx, *gateway, users, *concurrency, *duration)
	fmt.Printf("запросы %d, успех %d, ошибка генерации %d, отказ %d, сбой клиента %d\n",
		total.requests, total.processed, total.failed, total.rejected, total.errors)
	fmt.Printf("задержка p50 %s, p95 %s\n", percentile(total.latency, 0.50), percentile(total.latency, 0.95))
	if total.requests == 0 {
		fmt.Fprintln(os.Stderr, "за время прогона не было ответов генерации")

		return 1
	}

	return 0
}

type account struct {
	token string
}

type stats struct {
	requests  int
	processed int
	failed    int
	rejected  int
	errors    int
	latency   []time.Duration
}

const readyFor = time.Minute

func prepareReady(ctx context.Context, gateway, webhookKey string) ([]account, error) {
	deadline := time.Now().Add(readyFor)
	var last error
	for {
		users, err := prepare(ctx, gateway, webhookKey)
		if err == nil {
			return users, nil
		}

		last = err
		if time.Now().After(deadline) {
			return nil, last
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func prepare(ctx context.Context, gateway, webhookKey string) ([]account, error) {
	admin, err := register(ctx, gateway, "")
	if err != nil {
		return nil, err
	}

	org, err := call(ctx, http.MethodPost, gateway+"/api/v1/organizations", withKey(bearer(admin.token), uuid.New().String()), map[string]string{
		"name": "Demo " + uuid.New().String(),
	}, http.StatusCreated)
	if err != nil {
		return nil, err
	}

	orgToken := org["token"].(string)
	orgID := org["organization_id"].(string)
	personalID, err := tariff(ctx, gateway, orgToken, "demo-personal", 100, 1000, "b2c")
	if err != nil {
		return nil, err
	}

	nextID, err := tariff(ctx, gateway, orgToken, "demo-next", 200, 1500, "b2c")
	if err != nil {
		return nil, err
	}

	teamID, err := tariff(ctx, gateway, orgToken, "demo-team", 500, 1000, "b2b")
	if err != nil {
		return nil, err
	}

	var users []account
	var firstSubscription string
	for range 4 {
		user, regErr := register(ctx, gateway, "")
		if regErr != nil {
			return nil, regErr
		}

		subscriptionID, payErr := subscribeAndPay(ctx, gateway, webhookKey, user.token, personalID, 100)
		if payErr != nil {
			return nil, payErr
		}

		if firstSubscription == "" {
			firstSubscription = subscriptionID
		}

		users = append(users, user)
	}

	changed, err := call(ctx, http.MethodPut, gateway+"/api/v1/subscriptions/"+firstSubscription, withKey(bearer(users[0].token), uuid.New().String()), map[string]string{
		"tariff_id": nextID,
	}, http.StatusOK)
	if err != nil {
		return nil, err
	}

	fmt.Printf("смена тарифа: остаток %.0f\n", changed["message_allowance"].(float64))

	if _, err = subscribeAndPay(ctx, gateway, webhookKey, orgToken, teamID, 500); err != nil {
		return nil, err
	}

	for range 2 {
		member, regErr := register(ctx, gateway, orgID)
		if regErr != nil {
			return nil, regErr
		}

		users = append(users, member)
	}

	fmt.Printf("подготовлено участников: %d\n", len(users))

	return users, nil
}

func tariff(ctx context.Context, gateway, orgToken, name string, price, limit int, kind string) (string, error) {
	created, err := call(ctx, http.MethodPost, gateway+"/api/v1/tariffs", withKey(bearer(orgToken), uuid.New().String()), map[string]any{
		"name": name, "monthly_price_minor": price, "message_limit": limit, "type": kind, "is_base_tariff": false,
	}, http.StatusCreated)
	if err != nil {
		return "", err
	}

	return created["id"].(string), nil
}

func subscribeAndPay(ctx context.Context, gateway, webhookKey, token, tariffID string, amount int) (string, error) {
	created, err := call(ctx, http.MethodPost, gateway+"/api/v1/subscriptions", withKey(bearer(token), uuid.New().String()), map[string]string{
		"tariff_id": tariffID,
	}, http.StatusCreated)
	if err != nil {
		return "", err
	}

	subscriptionID := created["id"].(string)
	_, err = call(ctx, http.MethodPost, gateway+"/webhooks/payments", map[string]string{"X-Webhook-Key": webhookKey}, map[string]any{
		"payment_id":      uuid.New().String(),
		"subscription_id": subscriptionID,
		"amount_minor":    amount,
		"occurred_at":     time.Now().UTC().Format(time.RFC3339Nano),
	}, http.StatusAccepted)
	if err != nil {
		return "", err
	}

	return subscriptionID, nil
}

func register(ctx context.Context, gateway, organizationID string) (account, error) {
	body := map[string]string{"email": uuid.New().String() + "@example.com", "password": "password1"}
	if organizationID != "" {
		body["organization_id"] = organizationID
	}

	registered, err := call(ctx, http.MethodPost, gateway+"/api/v1/users", map[string]string{"Idempotency-Key": uuid.New().String()}, body, http.StatusCreated)
	if err != nil {
		return account{}, err
	}

	return account{token: registered["token"].(string)}, nil
}

func call(ctx context.Context, method, rawURL string, header map[string]string, body any, status int) (map[string]any, error) {
	resp, err := httpjson.Do(ctx, method, rawURL, header, body)
	if err != nil {
		return nil, err
	}

	if resp.Status != status {
		return nil, fmt.Errorf("%s %s: статус %d, тело %s", method, rawURL, resp.Status, resp.Raw)
	}

	return resp.Body, nil
}

func generate(ctx context.Context, gateway string, users []account, concurrency int, duration time.Duration) stats {
	var mu sync.Mutex
	var total stats
	var wg sync.WaitGroup
	deadline := time.Now().Add(duration)
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for time.Now().Before(deadline) {
				user := users[rand.IntN(len(users))]
				started := time.Now()
				reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				resp, err := httpjson.Do(reqCtx, http.MethodPost, gateway+"/api/v1/generate", withKey(bearer(user.token), uuid.New().String()), map[string]string{
					"prompt": uuid.New().String(),
				})
				cancel()
				elapsed := time.Since(started)

				mu.Lock()
				total.latency = append(total.latency, elapsed)
				switch {
				case err != nil:
					total.errors++
				case resp.Status == http.StatusOK && resp.Body["status"] == "processed":
					total.requests++
					total.processed++
				case resp.Status == http.StatusOK && resp.Body["status"] == "failed":
					total.requests++
					total.failed++
				default:
					total.requests++
					total.rejected++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	return total
}

func percentile(samples []time.Duration, p float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}

	ordered := append([]time.Duration(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	index := int(float64(len(ordered)-1) * p)

	return ordered[index].Round(time.Millisecond)
}

func waitHealthy(ctx context.Context, gateway string) error {
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		resp, err := httpjson.Do(reqCtx, http.MethodGet, gateway+"/health", nil, nil)
		cancel()
		if err == nil && resp.Status == http.StatusOK {
			return nil
		}

		last = err
		if err == nil {
			last = fmt.Errorf("статус %d", resp.Status)
		}

		time.Sleep(500 * time.Millisecond)
	}

	return last
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
