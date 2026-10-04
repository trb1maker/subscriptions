// Package stack поднимает Postgres, Redis, ClickHouse, NATS и бинарники сервисов для тестов.
package stack

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	clickhousecontainer "github.com/testcontainers/testcontainers-go/modules/clickhouse"
	natscontainer "github.com/testcontainers/testcontainers-go/modules/nats"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	authmigrations "github.com/trb1maker/subscriptions/migrations/auth"
	subscriptionsmigrations "github.com/trb1maker/subscriptions/migrations/subscriptions"
	usagemigrations "github.com/trb1maker/subscriptions/migrations/usage"
	"github.com/trb1maker/subscriptions/pkg/clickhouse"
	"github.com/trb1maker/subscriptions/pkg/migrate"
	pgpool "github.com/trb1maker/subscriptions/pkg/postgres"
	redispkg "github.com/trb1maker/subscriptions/pkg/redis"
)

const (
	webhookKey        = "webhook-secret"
	healthyFor        = 45 * time.Second
	healthPoll        = 200 * time.Millisecond
	shutdownFor       = 30 * time.Second
	processStopFor    = 15 * time.Second
	postgresReadyLogs = 2
	scriptPerm        = 0o755
)

// Gateway — один процесс gateway с заданной эмуляцией.
type Gateway struct {
	FailurePercent int
	DelayMin       time.Duration
	DelayMax       time.Duration
}

// Config задаёт расписание истечения и процессы gateway.
type Config struct {
	ExpireSchedule string
	Gateways       []Gateway
}

// Stack — инфраструктура и процессы одного прогона.
type Stack struct {
	Gateways []string
	NATSURL  string
	Redis    *goredis.Client
	Click    *sql.DB
	Pool     *pgxpool.Pool

	ctx        context.Context
	cancel     context.CancelFunc
	containers []testcontainers.Container
	cmds       []*exec.Cmd
	logs       *logBuf
	dirs       []string
}

// Start собирает стенд. При ошибке уже поднятое гасится.
func Start(parent context.Context, cfg Config) (started *Stack, err error) {
	ctx, cancel := context.WithCancel(parent)
	started = &Stack{ctx: ctx, cancel: cancel, logs: &logBuf{}}
	defer func() {
		if err != nil {
			started.Close(ctx)
		}
	}()

	if len(cfg.Gateways) == 0 {
		cfg.Gateways = []Gateway{{FailurePercent: 0}}
	}

	root, err := moduleRoot()
	if err != nil {
		return started, err
	}

	authDSN, err := started.postgres(ctx)
	if err != nil {
		return started, err
	}

	subDSN, err := withDatabase(authDSN, "subscriptions")
	if err != nil {
		return started, err
	}

	if err = createDatabase(ctx, authDSN, "subscriptions"); err != nil {
		return started, err
	}

	if err = migrate.Up(ctx, authDSN, authmigrations.FS); err != nil {
		return started, fmt.Errorf("migrate auth: %w", err)
	}

	if err = migrate.Up(ctx, subDSN, subscriptionsmigrations.FS); err != nil {
		return started, fmt.Errorf("migrate subscriptions: %w", err)
	}

	redisURL, err := started.redis(ctx)
	if err != nil {
		return started, err
	}

	clickDSN, err := started.clickhouse(ctx)
	if err != nil {
		return started, err
	}

	if err = migrate.UpClickHouse(ctx, clickDSN, usagemigrations.FS); err != nil {
		return started, fmt.Errorf("migrate usage: %w", err)
	}

	natsURL, err := started.nats(ctx)
	if err != nil {
		return started, err
	}

	started.NATSURL = natsURL

	certs, err := generateCerts(root)
	if err != nil {
		return started, err
	}

	started.dirs = append(started.dirs, certs)

	bins, err := buildBinaries(root)
	if err != nil {
		return started, err
	}

	started.dirs = append(started.dirs, filepath.Dir(bins["auth"]))

	authHTTP, authGRPC, err := pair()
	if err != nil {
		return started, err
	}

	subscriptionsHTTP, subscriptionsGRPC, err := pair()
	if err != nil {
		return started, err
	}

	usageHTTP, usageGRPC, err := pair()
	if err != nil {
		return started, err
	}

	if err = started.process(ctx, bins["auth"], map[string]string{
		"HTTP_ADDR":                   authHTTP,
		"GRPC_ADDR":                   authGRPC,
		"LOG_LEVEL":                   "error",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "none",
		"AUTH_DATABASE_URL":           authDSN,
		"JWT_SECRET":                  "test-secret",
		"TLS_CERT_FILE":               filepath.Join(certs, "auth.crt"),
		"TLS_KEY_FILE":                filepath.Join(certs, "auth.key"),
		"TLS_CA_FILE":                 filepath.Join(certs, "ca.crt"),
	}); err != nil {
		return started, err
	}

	subscriptionsEnv := map[string]string{
		"HTTP_ADDR":                    subscriptionsHTTP,
		"GRPC_ADDR":                    subscriptionsGRPC,
		"LOG_LEVEL":                    "error",
		"OTEL_EXPORTER_OTLP_ENDPOINT":  "none",
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
	}
	if cfg.ExpireSchedule != "" {
		subscriptionsEnv["SUBSCRIPTIONS_EXPIRE_SCHEDULE"] = cfg.ExpireSchedule
	}

	if err = started.process(ctx, bins["subscriptions"], subscriptionsEnv); err != nil {
		return started, err
	}

	if err = started.process(ctx, bins["usage"], map[string]string{
		"HTTP_ADDR":                   usageHTTP,
		"GRPC_ADDR":                   usageGRPC,
		"LOG_LEVEL":                   "error",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "none",
		"USAGE_REDIS_URL":             redisURL,
		"USAGE_CLICKHOUSE_DSN":        clickDSN,
		"USAGE_NATS_URL":              natsURL,
		"AUTH_GRPC_ADDR":              authGRPC,
		"AUTH_GRPC_SERVER_NAME":       "localhost",
		"TLS_CERT_FILE":               filepath.Join(certs, "usage.crt"),
		"TLS_KEY_FILE":                filepath.Join(certs, "usage.key"),
		"TLS_CA_FILE":                 filepath.Join(certs, "ca.crt"),
	}); err != nil {
		return started, err
	}

	for _, gateway := range cfg.Gateways {
		addr, addrErr := freeAddr()
		if addrErr != nil {
			return started, addrErr
		}

		if err = started.process(ctx, bins["gateway"], map[string]string{
			"HTTP_ADDR":                      addr,
			"LOG_LEVEL":                      "error",
			"OTEL_EXPORTER_OTLP_ENDPOINT":    "none",
			"AUTH_GRPC_ADDR":                 authGRPC,
			"AUTH_GRPC_SERVER_NAME":          "localhost",
			"SUBSCRIPTIONS_GRPC_ADDR":        subscriptionsGRPC,
			"SUBSCRIPTIONS_GRPC_SERVER_NAME": "localhost",
			"USAGE_GRPC_ADDR":                usageGRPC,
			"USAGE_GRPC_SERVER_NAME":         "localhost",
			"GATEWAY_NATS_URL":               natsURL,
			"WEBHOOK_KEY":                    webhookKey,
			"GENERATE_DELAY_MIN":             gateway.DelayMin.String(),
			"GENERATE_DELAY_MAX":             gateway.DelayMax.String(),
			"GENERATE_FAILURE_PERCENT":       fmt.Sprintf("%d", gateway.FailurePercent),
			"TLS_CERT_FILE":                  filepath.Join(certs, "gateway.crt"),
			"TLS_KEY_FILE":                   filepath.Join(certs, "gateway.key"),
			"TLS_CA_FILE":                    filepath.Join(certs, "ca.crt"),
		}); err != nil {
			return started, err
		}

		started.Gateways = append(started.Gateways, "http://"+addr)
	}

	if err = waitHealthy(ctx, append([]string{authHTTP, subscriptionsHTTP, usageHTTP}, addrs(started.Gateways)...)); err != nil {
		return started, fmt.Errorf("wait healthy: %w\n%s", err, started.logs.String())
	}

	started.Redis, err = redispkg.New(ctx, redisURL)
	if err != nil {
		return started, fmt.Errorf("open redis: %w", err)
	}

	started.Click, err = clickhouse.Open(ctx, clickDSN)
	if err != nil {
		return started, fmt.Errorf("open clickhouse: %w", err)
	}

	started.Pool, err = pgpool.NewPool(ctx, subDSN)
	if err != nil {
		return started, fmt.Errorf("open postgres: %w", err)
	}

	return started, nil
}

// Close останавливает процессы и контейнеры. Повторный вызов безопасен.
func (s *Stack) Close(ctx context.Context) {
	if s == nil || s.cancel == nil {
		return
	}

	s.cancel()
	s.cancel = nil
	for _, cmd := range s.cmds {
		_ = cmd.Wait()
	}

	if s.Redis != nil {
		_ = s.Redis.Close()
	}

	if s.Click != nil {
		_ = s.Click.Close()
	}

	if s.Pool != nil {
		s.Pool.Close()
	}

	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownFor)
	defer cancel()
	for _, container := range s.containers {
		_ = container.Terminate(stopCtx)
	}

	for _, dir := range s.dirs {
		_ = os.RemoveAll(dir)
	}
}

// Logs возвращает stdout и stderr процессов.
func (s *Stack) Logs() string {
	if s == nil || s.logs == nil {
		return ""
	}

	return s.logs.String()
}

// FinishPeriod сдвигает конец периода в прошлое, чтобы cron закрыл подписку.
func (s *Stack) FinishPeriod(ctx context.Context, subscriptionID string) error {
	end := time.Now().Add(-time.Second).UTC()
	_, err := s.Pool.Exec(ctx, `
		UPDATE subscriptions
		SET current_period_start = $1, current_period_end = $2
		WHERE id = $3`, end.Add(-time.Minute), end, subscriptionID)
	if err != nil {
		return fmt.Errorf("finish period: %w", err)
	}

	return nil
}

// WebhookKey — секрет заголовка X-Webhook-Key на этом стенде.
func WebhookKey() string {
	return webhookKey
}

func (s *Stack) postgres(ctx context.Context) (string, error) {
	container, err := postgres.Run(ctx, "postgres:18.6-alpine3.24",
		postgres.WithDatabase("auth"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(postgresReadyLogs).
				WithStartupTimeout(time.Minute),
		),
	)
	if err != nil {
		return "", fmt.Errorf("start postgres: %w", err)
	}

	s.containers = append(s.containers, container)
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return "", fmt.Errorf("postgres dsn: %w", err)
	}

	return dsn, nil
}

func (s *Stack) redis(ctx context.Context) (string, error) {
	container, err := rediscontainer.Run(ctx, "redis:8.10-alpine")
	if err != nil {
		return "", fmt.Errorf("start redis: %w", err)
	}

	s.containers = append(s.containers, container)
	raw, err := container.ConnectionString(ctx)
	if err != nil {
		return "", fmt.Errorf("redis url: %w", err)
	}

	return raw, nil
}

func (s *Stack) clickhouse(ctx context.Context) (string, error) {
	container, err := clickhousecontainer.Run(ctx, "clickhouse:26.7",
		clickhousecontainer.WithUsername("default"),
		clickhousecontainer.WithPassword("secret"),
		clickhousecontainer.WithDatabase("default"),
	)
	if err != nil {
		return "", fmt.Errorf("start clickhouse: %w", err)
	}

	s.containers = append(s.containers, container)
	dsn, err := container.ConnectionString(ctx)
	if err != nil {
		return "", fmt.Errorf("clickhouse dsn: %w", err)
	}

	return dsn, nil
}

func (s *Stack) nats(ctx context.Context) (string, error) {
	container, err := natscontainer.Run(ctx, "nats:2.14-alpine")
	if err != nil {
		return "", fmt.Errorf("start nats: %w", err)
	}

	s.containers = append(s.containers, container)
	raw, err := container.ConnectionString(ctx)
	if err != nil {
		return "", fmt.Errorf("nats url: %w", err)
	}

	return raw, nil
}

func (s *Stack) process(ctx context.Context, bin string, env map[string]string) error {
	cmd := exec.CommandContext(ctx, bin)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = processStopFor
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	cmd.Stdout = s.logs
	cmd.Stderr = s.logs
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", bin, err)
	}

	s.cmds = append(s.cmds, cmd)

	return nil
}

func waitHealthy(ctx context.Context, addrs []string) error {
	deadline := time.Now().Add(healthyFor)
	for _, addr := range addrs {
		for {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("health: %w", err)
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/health", nil)
			if err == nil {
				resp, doErr := http.DefaultClient.Do(req)
				if doErr == nil {
					_ = resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						break
					}
				}
			}

			if time.Now().After(deadline) {
				return fmt.Errorf("health timeout for %s", addr)
			}

			time.Sleep(healthPoll)
		}
	}

	return nil
}

func addrs(bases []string) []string {
	out := make([]string, 0, len(bases))
	for _, base := range bases {
		parsed, err := url.Parse(base)
		if err != nil {
			continue
		}

		out = append(out, parsed.Host)
	}

	return out
}

func buildBinaries(root string) (map[string]string, error) {
	dir, err := os.MkdirTemp("", "subscriptions-bins-")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}

	names := []string{"auth", "subscriptions", "usage", "gateway"}
	bins := make(map[string]string, len(names))
	for _, name := range names {
		out := filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-o", out, "./services/"+name+"/cmd/"+name)
		cmd.Dir = root
		output, buildErr := cmd.CombinedOutput()
		if buildErr != nil {
			_ = os.RemoveAll(dir)

			return nil, fmt.Errorf("build %s: %w\n%s", name, buildErr, output)
		}

		bins[name] = out
	}

	return bins, nil
}

func generateCerts(root string) (string, error) {
	dir, err := os.MkdirTemp("", "subscriptions-certs-")
	if err != nil {
		return "", fmt.Errorf("cert dir: %w", err)
	}

	script, err := os.ReadFile(filepath.Join(root, "deploy", "certs", "generate.sh"))
	if err != nil {
		_ = os.RemoveAll(dir)

		return "", fmt.Errorf("read cert script: %w", err)
	}

	target := filepath.Join(dir, "generate.sh")
	if err = os.WriteFile(target, script, scriptPerm); err != nil {
		_ = os.RemoveAll(dir)

		return "", fmt.Errorf("write cert script: %w", err)
	}

	cmd := exec.Command("sh", target)
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(dir)

		return "", fmt.Errorf("generate certs: %w\n%s", err, output)
	}

	return dir, nil
}

func createDatabase(ctx context.Context, dsn, name string) error {
	pool, err := pgpool.NewPool(ctx, dsn)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer pool.Close()

	if _, err = pool.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		return fmt.Errorf("create database %s: %w", name, err)
	}

	return nil
}

func withDatabase(dsn, name string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse dsn: %w", err)
	}

	parsed.Path = "/" + name

	return parsed.String(), nil
}

func pair() (string, string, error) {
	left, err := freeAddr()
	if err != nil {
		return "", "", err
	}

	right, err := freeAddr()
	if err != nil {
		return "", "", err
	}

	return left, right, nil
}

func freeAddr() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen: %w", err)
	}

	addr := listener.Addr().String()
	if err = listener.Close(); err != nil {
		return "", fmt.Errorf("close listener: %w", err)
	}

	return addr, nil
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("workdir: %w", err)
	}

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", dir)
		}

		dir = parent
	}
}

type logBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	n, err := l.buf.Write(p)
	if err != nil {
		return n, fmt.Errorf("buffer log: %w", err)
	}

	return n, nil
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.buf.String()
}
