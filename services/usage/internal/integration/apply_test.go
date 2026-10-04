//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"io"
	"testing"
	"time"
	"uuid"

	natsserver "github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
	clickhousecontainer "github.com/testcontainers/testcontainers-go/modules/clickhouse"
	natscontainer "github.com/testcontainers/testcontainers-go/modules/nats"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/trb1maker/subscriptions/api/gen/events/v1"
	usagemigrations "github.com/trb1maker/subscriptions/migrations/usage"
	"github.com/trb1maker/subscriptions/pkg/clickhouse"
	"github.com/trb1maker/subscriptions/pkg/logger"
	"github.com/trb1maker/subscriptions/pkg/migrate"
	natspkg "github.com/trb1maker/subscriptions/pkg/nats"
	redispkg "github.com/trb1maker/subscriptions/pkg/redis"
	clickstore "github.com/trb1maker/subscriptions/services/usage/internal/adapters/clickhouse"
	natsadapter "github.com/trb1maker/subscriptions/services/usage/internal/adapters/nats"
	redisstore "github.com/trb1maker/subscriptions/services/usage/internal/adapters/redis"
	"github.com/trb1maker/subscriptions/services/usage/internal/app"
	"github.com/trb1maker/subscriptions/services/usage/internal/domain"
)

func TestEventsUpdateLimitAndRestore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	redisURL := startRedis(t, ctx)
	clickDSN := startClickHouse(t, ctx)
	natsURL := startNATS(t, ctx)

	require.NoError(t, migrate.UpClickHouse(ctx, clickDSN, usagemigrations.FS))

	redisClient, err := redispkg.New(ctx, redisURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = redisClient.Close() })

	clickDB, err := clickhouse.Open(ctx, clickDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clickDB.Close() })

	balances := redisstore.NewStore(redisClient)
	ledger := clickstore.NewStore(clickDB)
	svc, err := app.New(ledger, balances, rejectDirectory{})
	require.NoError(t, err)

	owner := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}
	start := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	payment := domain.Event{
		ID: uuid.New(), Type: domain.EventPaymentReceived, OccurredAt: start, Owner: owner,
		HasAllowance: true, Allowance: 10, PaymentID: "pay-1", AmountMinor: 9900,
	}
	failed := domain.Event{
		ID: uuid.New(), Type: domain.EventMessageFailed, OccurredAt: start.Add(time.Second), Owner: owner, Tokens: 3,
	}
	first := domain.Event{
		ID: uuid.New(), Type: domain.EventMessageProcessed, OccurredAt: start.Add(2 * time.Second), Owner: owner, Tokens: 5,
	}
	second := domain.Event{
		ID: uuid.New(), Type: domain.EventMessageProcessed, OccurredAt: start.Add(3 * time.Second), Owner: owner, Tokens: 7,
	}
	changed := domain.Event{
		ID: uuid.New(), Type: domain.EventSubscriptionChanged, OccurredAt: start.Add(4 * time.Second), Owner: owner,
		HasAllowance: true, Allowance: 4,
	}
	afterChange := domain.Event{
		ID: uuid.New(), Type: domain.EventMessageProcessed, OccurredAt: start.Add(5 * time.Second), Owner: owner, Tokens: 1,
	}
	ended := domain.Event{
		ID: uuid.New(), Type: domain.EventSubscriptionPeriodEnded, OccurredAt: start.Add(6 * time.Second), Owner: owner,
		HasAllowance: true, Allowance: 0,
	}

	for _, event := range []domain.Event{payment, first, failed, second} {
		require.NoError(t, svc.Apply(ctx, event))
	}

	remaining, found, err := balances.Get(ctx, owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(8), remaining)

	require.NoError(t, svc.Apply(ctx, changed))
	require.NoError(t, svc.Apply(ctx, afterChange))
	remaining, _, err = balances.Get(ctx, owner)
	require.NoError(t, err)
	require.Equal(t, int64(3), remaining)

	require.NoError(t, svc.Apply(ctx, ended))
	for _, event := range []domain.Event{payment, first, failed, second, changed, afterChange, ended} {
		require.NoError(t, svc.Apply(ctx, event))
	}

	conflict := ended
	conflict.Allowance = 5
	require.ErrorIs(t, svc.Apply(ctx, conflict), domain.ErrIdempotencyConflict)

	remaining, _, err = balances.Get(ctx, owner)
	require.NoError(t, err)
	require.Equal(t, int64(0), remaining)

	require.Equal(t, 1, countRows(t, ctx, clickDB, "SELECT count() FROM usage.payments FINAL"))
	require.Equal(t, 4, countRows(t, ctx, clickDB, "SELECT count() FROM usage.token_usage FINAL"))

	require.NoError(t, redisClient.FlushDB(ctx).Err())
	require.NoError(t, svc.Restore(ctx))
	remaining, found, err = balances.Get(ctx, owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(0), remaining)

	log, err := logger.New(io.Discard, "error")
	require.NoError(t, err)
	conn, jetStream, err := natspkg.Connect(ctx, natsURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Drain() })

	consumeCtx, consumeCancel := context.WithCancel(ctx)
	t.Cleanup(consumeCancel)
	done := make(chan error, 1)
	go func() {
		done <- natsadapter.Run(consumeCtx, jetStream, svc, log)
	}()

	published := domain.Event{
		ID: uuid.New(), Type: domain.EventMessageProcessed, OccurredAt: start.Add(7 * time.Second), Owner: owner, Tokens: 2,
	}
	require.Eventually(t, func() bool {
		return publish(jetStream, published) == nil
	}, 20*time.Second, 100*time.Millisecond)

	require.Eventually(t, func() bool {
		value, ok, getErr := balances.Get(ctx, owner)
		return getErr == nil && ok && value == -1
	}, 20*time.Second, 100*time.Millisecond)

	consumeCancel()
	require.NoError(t, <-done)
}

func publish(js natsserver.JetStream, event domain.Event) error {
	body, err := proto.Marshal(eventMessage(event))
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = js.Publish(ctx, natsadapter.Subject(event.Owner), body, natsserver.WithMsgID(event.ID.String()))

	return err
}

func eventMessage(event domain.Event) *eventsv1.UsageEvent {
	message := &eventsv1.UsageEvent{
		EventId:    event.ID.String(),
		OccurredAt: event.OccurredAt.UTC().Format(time.RFC3339Nano),
		OwnerKind:  string(event.Owner.Kind),
		OwnerId:    event.Owner.ID.String(),
	}
	switch event.Type {
	case domain.EventMessageProcessed:
		message.Kind = &eventsv1.UsageEvent_MessageProcessed{MessageProcessed: &eventsv1.MessageProcessed{Tokens: event.Tokens}}
	case domain.EventMessageFailed:
		message.Kind = &eventsv1.UsageEvent_MessageFailed{MessageFailed: &eventsv1.MessageFailed{Tokens: event.Tokens}}
	case domain.EventPaymentReceived:
		message.Kind = &eventsv1.UsageEvent_PaymentReceived{PaymentReceived: &eventsv1.PaymentReceived{
			Allowance: event.Allowance, PaymentId: event.PaymentID, AmountMinor: event.AmountMinor,
		}}
	case domain.EventSubscriptionChanged:
		message.Kind = &eventsv1.UsageEvent_SubscriptionChanged{SubscriptionChanged: &eventsv1.SubscriptionChanged{Allowance: event.Allowance}}
	case domain.EventSubscriptionPeriodEnded:
		message.Kind = &eventsv1.UsageEvent_SubscriptionPeriodEnded{SubscriptionPeriodEnded: &eventsv1.SubscriptionPeriodEnded{Allowance: event.Allowance}}
	}

	return message
}

func countRows(t *testing.T, ctx context.Context, db *sql.DB, query string) int {
	t.Helper()

	var count uint64
	require.NoError(t, db.QueryRowContext(ctx, query).Scan(&count))

	return int(count)
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

type rejectDirectory struct{}

func (rejectDirectory) Lookup(context.Context, uuid.UUID, domain.OwnerKind) (domain.Subject, error) {
	return domain.Subject{}, domain.ErrUnavailable
}
