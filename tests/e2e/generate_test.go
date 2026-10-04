//go:build integration

package e2e_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
	"uuid"

	natsserver "github.com/nats-io/nats.go/jetstream"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	clickhousecontainer "github.com/testcontainers/testcontainers-go/modules/clickhouse"
	natscontainer "github.com/testcontainers/testcontainers-go/modules/nats"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/trb1maker/subscriptions/api/gen/events/v1"
	authmigrations "github.com/trb1maker/subscriptions/migrations/auth"
	subscriptionsmigrations "github.com/trb1maker/subscriptions/migrations/subscriptions"
	usagemigrations "github.com/trb1maker/subscriptions/migrations/usage"
	"github.com/trb1maker/subscriptions/pkg/clickhouse"
	"github.com/trb1maker/subscriptions/pkg/migrate"
	natspkg "github.com/trb1maker/subscriptions/pkg/nats"
	pgpool "github.com/trb1maker/subscriptions/pkg/postgres"
	redispkg "github.com/trb1maker/subscriptions/pkg/redis"
)

const allowance int64 = 3

func TestGenerateDebitsClickHouse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	root := moduleRoot(t)
	authDSN := startPostgres(t, ctx)
	subDSN := withDatabase(t, authDSN, "subscriptions")
	createDatabase(t, ctx, authDSN, "subscriptions")
	require.NoError(t, migrate.Up(ctx, authDSN, authmigrations.FS))
	require.NoError(t, migrate.Up(ctx, subDSN, subscriptionsmigrations.FS))

	redisURL := startRedis(t, ctx)
	clickDSN := startClickHouse(t, ctx)
	natsURL := startNATS(t, ctx)
	require.NoError(t, migrate.UpClickHouse(ctx, clickDSN, usagemigrations.FS))

	certs := generateCerts(t, root)
	authHTTP := listenAddr(t)
	authGRPC := listenAddr(t)
	subscriptionsHTTP := listenAddr(t)
	subscriptionsGRPC := listenAddr(t)
	usageHTTP := listenAddr(t)
	usageGRPC := listenAddr(t)
	gatewayHTTP := listenAddr(t)
	failedGatewayHTTP := listenAddr(t)

	authBin := buildBinary(t, root, "./services/auth/cmd/auth")
	subscriptionsBin := buildBinary(t, root, "./services/subscriptions/cmd/subscriptions")
	usageBin := buildBinary(t, root, "./services/usage/cmd/usage")
	gatewayBin := buildBinary(t, root, "./services/gateway/cmd/gateway")

	startProcess(t, ctx, authBin, map[string]string{
		"HTTP_ADDR":         authHTTP,
		"GRPC_ADDR":         authGRPC,
		"LOG_LEVEL":         "error",
		"AUTH_DATABASE_URL": authDSN,
		"JWT_SECRET":        "test-secret",
		"TLS_CERT_FILE":     filepath.Join(certs, "auth.crt"),
		"TLS_KEY_FILE":      filepath.Join(certs, "auth.key"),
		"TLS_CA_FILE":       filepath.Join(certs, "ca.crt"),
	})
	startProcess(t, ctx, subscriptionsBin, map[string]string{
		"HTTP_ADDR":                    subscriptionsHTTP,
		"GRPC_ADDR":                    subscriptionsGRPC,
		"LOG_LEVEL":                    "error",
		"SUBSCRIPTIONS_DATABASE_URL":   subDSN,
		"AUTH_GRPC_ADDR":               authGRPC,
		"AUTH_GRPC_SERVER_NAME":        "localhost",
		"USAGE_GRPC_ADDR":              usageGRPC,
		"USAGE_GRPC_SERVER_NAME":       "localhost",
		"SUBSCRIPTIONS_NATS_URL":       natsURL,
		"SUBSCRIPTIONS_REQUEST_PEPPER": "pepper",
		"TLS_CERT_FILE":                filepath.Join(certs, "subscriptions.crt"),
		"TLS_KEY_FILE":                 filepath.Join(certs, "subscriptions.key"),
		"TLS_CA_FILE":                  filepath.Join(certs, "ca.crt"),
	})
	startProcess(t, ctx, usageBin, map[string]string{
		"HTTP_ADDR":             usageHTTP,
		"GRPC_ADDR":             usageGRPC,
		"LOG_LEVEL":             "error",
		"USAGE_REDIS_URL":       redisURL,
		"USAGE_CLICKHOUSE_DSN":  clickDSN,
		"USAGE_NATS_URL":        natsURL,
		"AUTH_GRPC_ADDR":        authGRPC,
		"AUTH_GRPC_SERVER_NAME": "localhost",
		"TLS_CERT_FILE":         filepath.Join(certs, "usage.crt"),
		"TLS_KEY_FILE":          filepath.Join(certs, "usage.key"),
		"TLS_CA_FILE":           filepath.Join(certs, "ca.crt"),
	})
	startProcess(t, ctx, gatewayBin, map[string]string{
		"HTTP_ADDR":                      gatewayHTTP,
		"LOG_LEVEL":                      "error",
		"AUTH_GRPC_ADDR":                 authGRPC,
		"AUTH_GRPC_SERVER_NAME":          "localhost",
		"SUBSCRIPTIONS_GRPC_ADDR":        subscriptionsGRPC,
		"SUBSCRIPTIONS_GRPC_SERVER_NAME": "localhost",
		"USAGE_GRPC_ADDR":                usageGRPC,
		"USAGE_GRPC_SERVER_NAME":         "localhost",
		"GATEWAY_NATS_URL":               natsURL,
		"WEBHOOK_KEY":                    "webhook-secret",
		"GENERATE_DELAY_MIN":             "0s",
		"GENERATE_DELAY_MAX":             "0s",
		"GENERATE_FAILURE_PERCENT":       "0",
		"TLS_CERT_FILE":                  filepath.Join(certs, "gateway.crt"),
		"TLS_KEY_FILE":                   filepath.Join(certs, "gateway.key"),
		"TLS_CA_FILE":                    filepath.Join(certs, "ca.crt"),
	})
	startProcess(t, ctx, gatewayBin, map[string]string{
		"HTTP_ADDR":                      failedGatewayHTTP,
		"LOG_LEVEL":                      "error",
		"AUTH_GRPC_ADDR":                 authGRPC,
		"AUTH_GRPC_SERVER_NAME":          "localhost",
		"SUBSCRIPTIONS_GRPC_ADDR":        subscriptionsGRPC,
		"SUBSCRIPTIONS_GRPC_SERVER_NAME": "localhost",
		"USAGE_GRPC_ADDR":                usageGRPC,
		"USAGE_GRPC_SERVER_NAME":         "localhost",
		"GATEWAY_NATS_URL":               natsURL,
		"WEBHOOK_KEY":                    "webhook-secret",
		"GENERATE_DELAY_MIN":             "0s",
		"GENERATE_DELAY_MAX":             "0s",
		"GENERATE_FAILURE_PERCENT":       "100",
		"TLS_CERT_FILE":                  filepath.Join(certs, "gateway.crt"),
		"TLS_KEY_FILE":                   filepath.Join(certs, "gateway.key"),
		"TLS_CA_FILE":                    filepath.Join(certs, "ca.crt"),
	})

	waitHealthy(t, ctx, authHTTP)
	waitHealthy(t, ctx, subscriptionsHTTP)
	waitHealthy(t, ctx, usageHTTP)
	waitHealthy(t, ctx, gatewayHTTP)
	waitHealthy(t, ctx, failedGatewayHTTP)

	base := "http://" + gatewayHTTP
	registered := postJSON(t, ctx, base+"/api/v1/users", map[string]string{"Idempotency-Key": "user-1"}, map[string]string{
		"email": "ada@example.com", "password": "password1",
	}, http.StatusCreated)
	userID := registered["user_id"].(string)
	userToken := registered["token"].(string)

	organization := postJSON(t, ctx, base+"/api/v1/organizations", map[string]string{
		"Authorization": "Bearer " + userToken, "Idempotency-Key": "org-1",
	}, map[string]string{"name": "Acme"}, http.StatusCreated)
	orgToken := organization["token"].(string)

	tariff := postJSON(t, ctx, base+"/api/v1/tariffs", map[string]string{
		"Authorization": "Bearer " + orgToken, "Idempotency-Key": "tariff-1",
	}, map[string]any{
		"name": "personal", "monthly_price_minor": 100, "message_limit": 10, "type": "b2c", "is_base_tariff": false,
	}, http.StatusCreated)
	tariffID := tariff["id"].(string)

	postJSON(t, ctx, base+"/api/v1/subscriptions", map[string]string{
		"Authorization": "Bearer " + userToken, "Idempotency-Key": "sub-1",
	}, map[string]string{"tariff_id": tariffID}, http.StatusCreated)

	redisClient := openRedis(t, ctx, redisURL)
	require.Eventually(t, func() bool {
		return publishAllowance(t, natsURL, userID) == nil
	}, 20*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool {
		return redisLimit(t, ctx, redisClient, userID) == allowance
	}, 20*time.Second, 100*time.Millisecond)

	eventID := uuid.New().String()
	generated := postJSON(t, ctx, base+"/api/v1/generate", map[string]string{
		"Authorization": "Bearer " + userToken, "Idempotency-Key": eventID,
	}, map[string]string{"prompt": "hello"}, http.StatusOK)
	require.Equal(t, "processed", generated["status"])
	require.NotEmpty(t, generated["text"])
	tokens := int64(generated["tokens"].(float64))
	require.Positive(t, tokens)

	clickDB := openClickHouse(t, ctx, clickDSN)
	require.Eventually(t, func() bool {
		return tokenRow(t, ctx, clickDB, eventID) == tokens
	}, 20*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool {
		return redisLimit(t, ctx, redisClient, userID) == allowance-1
	}, 20*time.Second, 100*time.Millisecond)

	failedID := uuid.New().String()
	failed := postJSON(t, ctx, "http://"+failedGatewayHTTP+"/api/v1/generate", map[string]string{
		"Authorization": "Bearer " + userToken, "Idempotency-Key": failedID,
	}, map[string]string{"prompt": "fail"}, http.StatusOK)
	require.Equal(t, "failed", failed["status"])
	require.Empty(t, failed["text"])
	failedTokens := int64(failed["tokens"].(float64))
	require.Positive(t, failedTokens)
	require.Eventually(t, func() bool {
		tokens, outcome := usageRow(t, ctx, clickDB, failedID)

		return outcome == "failed" && tokens == failedTokens && redisLimit(t, ctx, redisClient, userID) == allowance-1
	}, 20*time.Second, 100*time.Millisecond)

	payer := postJSON(t, ctx, base+"/api/v1/users", map[string]string{"Idempotency-Key": "user-2"}, map[string]string{
		"email": "grace@example.com", "password": "password1",
	}, http.StatusCreated)
	payerID := payer["user_id"].(string)
	payerToken := payer["token"].(string)
	created := postJSON(t, ctx, base+"/api/v1/subscriptions", map[string]string{
		"Authorization": "Bearer " + payerToken, "Idempotency-Key": "sub-2",
	}, map[string]string{"tariff_id": tariffID}, http.StatusCreated)
	subscriptionID := created["id"].(string)
	paymentID := uuid.New().String()
	webhook := map[string]any{
		"payment_id":      paymentID,
		"subscription_id": subscriptionID,
		"amount_minor":    100,
		"occurred_at":     time.Now().UTC().Format(time.RFC3339Nano),
	}
	postJSON(t, ctx, base+"/webhooks/payments", map[string]string{"X-Webhook-Key": "webhook-secret"}, webhook, http.StatusAccepted)
	require.Eventually(t, func() bool {
		return redisLimit(t, ctx, redisClient, payerID) == 10
	}, 20*time.Second, 100*time.Millisecond)

	paid := getJSON(t, ctx, base+"/api/v1/subscriptions/"+subscriptionID, map[string]string{
		"Authorization": "Bearer " + payerToken,
	}, http.StatusOK)
	require.Equal(t, float64(10), paid["message_allowance"])
	periodEnd := paid["current_period_end"].(string)
	require.NotEqual(t, created["current_period_end"], periodEnd)

	postJSON(t, ctx, base+"/webhooks/payments", map[string]string{"X-Webhook-Key": "webhook-secret"}, webhook, http.StatusAccepted)
	replayed := getJSON(t, ctx, base+"/api/v1/subscriptions/"+subscriptionID, map[string]string{
		"Authorization": "Bearer " + payerToken,
	}, http.StatusOK)
	require.Equal(t, periodEnd, replayed["current_period_end"])
	require.Equal(t, int64(10), redisLimit(t, ctx, redisClient, payerID))
}

func publishAllowance(t *testing.T, natsURL, userID string) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	conn, jetStream, err := natspkg.Connect(ctx, natsURL)
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

func redisLimit(t *testing.T, ctx context.Context, client *goredis.Client, userID string) int64 {
	t.Helper()

	value, err := client.Get(ctx, "limit:user:"+userID).Result()
	if err != nil {
		return -1
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return -1
	}

	return parsed
}

func tokenRow(t *testing.T, ctx context.Context, db *sql.DB, eventID string) int64 {
	t.Helper()

	tokens, outcome := usageRow(t, ctx, db, eventID)
	if outcome != "processed" {
		return 0
	}

	return tokens
}

func usageRow(t *testing.T, ctx context.Context, db *sql.DB, eventID string) (int64, string) {
	t.Helper()

	var tokens int64
	var outcome string
	err := db.QueryRowContext(ctx,
		`SELECT tokens, outcome FROM usage.token_usage FINAL WHERE event_id = toUUID(?)`, eventID,
	).Scan(&tokens, &outcome)
	if err != nil {
		return 0, ""
	}

	return tokens, outcome
}

func postJSON(t *testing.T, ctx context.Context, url string, header map[string]string, body any, status int) map[string]any {
	t.Helper()

	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	for key, value := range header {
		req.Header.Set(key, value)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, status, resp.StatusCode, string(raw))

	if len(raw) == 0 {
		return map[string]any{}
	}

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	return decoded
}

func getJSON(t *testing.T, ctx context.Context, rawURL string, header map[string]string, status int) map[string]any {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	require.NoError(t, err)
	for key, value := range header {
		req.Header.Set(key, value)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, status, resp.StatusCode, string(raw))

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	return decoded
}

func waitHealthy(t *testing.T, ctx context.Context, addr string) {
	t.Helper()

	require.Eventually(t, func() bool {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/health", nil)
		if err != nil {
			return false
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()

		return resp.StatusCode == http.StatusOK
	}, 30*time.Second, 200*time.Millisecond)
}

func startProcess(t *testing.T, ctx context.Context, bin string, env map[string]string) {
	t.Helper()

	cmd := exec.CommandContext(ctx, bin)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 15 * time.Second
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	logs := &bytes.Buffer{}
	cmd.Stdout = logs
	cmd.Stderr = logs
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("%s\n%s", bin, logs.String())
		}
	})
}

func buildBinary(t *testing.T, root, pkg string) string {
	t.Helper()

	out := filepath.Join(t.TempDir(), filepath.Base(pkg))
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	return out
}

func listenAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())

	return addr
}

func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	return filepath.Clean(filepath.Join(dir, "../.."))
}

func startPostgres(t *testing.T, ctx context.Context) string {
	t.Helper()

	container, err := postgres.Run(ctx, "postgres:18.6-alpine3.24",
		postgres.WithDatabase("auth"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(time.Minute),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	return dsn
}

func createDatabase(t *testing.T, ctx context.Context, dsn, name string) {
	t.Helper()

	pool, err := pgpool.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	_, err = pool.Exec(ctx, "CREATE DATABASE "+name)
	require.NoError(t, err)
}

func withDatabase(t *testing.T, dsn, name string) string {
	t.Helper()

	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	parsed.Path = "/" + name

	return parsed.String()
}

func generateCerts(t *testing.T, root string) string {
	t.Helper()

	dir := t.TempDir()
	script, err := os.ReadFile(filepath.Join(root, "deploy", "certs", "generate.sh"))
	require.NoError(t, err)
	target := filepath.Join(dir, "generate.sh")
	require.NoError(t, os.WriteFile(target, script, 0o755))

	cmd := exec.Command("sh", target)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	return dir
}

func startRedis(t *testing.T, ctx context.Context) string {
	t.Helper()

	container, err := rediscontainer.Run(ctx, "redis:8.10-alpine")
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	url, err := container.ConnectionString(ctx)
	require.NoError(t, err)

	return url
}

func startClickHouse(t *testing.T, ctx context.Context) string {
	t.Helper()

	container, err := clickhousecontainer.Run(ctx, "clickhouse/clickhouse-server:24.8-alpine",
		clickhousecontainer.WithUsername("default"),
		clickhousecontainer.WithPassword("secret"),
		clickhousecontainer.WithDatabase("default"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	dsn, err := container.ConnectionString(ctx)
	require.NoError(t, err)

	return dsn
}

func startNATS(t *testing.T, ctx context.Context) string {
	t.Helper()

	container, err := natscontainer.Run(ctx, "nats:2.14-alpine")
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	url, err := container.ConnectionString(ctx)
	require.NoError(t, err)

	return url
}

func openRedis(t *testing.T, ctx context.Context, url string) *goredis.Client {
	t.Helper()

	client, err := redispkg.New(ctx, url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	return client
}

func openClickHouse(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()

	db, err := clickhouse.Open(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return db
}
