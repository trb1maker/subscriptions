//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	subscriptionsmigrations "github.com/trb1maker/subscriptions/migrations/subscriptions"
	"github.com/trb1maker/subscriptions/pkg/migrate"
	pgpool "github.com/trb1maker/subscriptions/pkg/postgres"
	subscriptionspostgres "github.com/trb1maker/subscriptions/services/subscriptions/internal/adapters/postgres"
	"github.com/trb1maker/subscriptions/services/subscriptions/internal/domain"
)

func TestRepositoryTariffSubscriptionAndExpiry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	dsn := startPostgres(t, ctx)
	pool, err := pgpool.NewPool(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	repo := subscriptionspostgres.NewRepository(pool)

	base := domain.Tariff{
		ID:                uuid.New(),
		Name:              "base",
		MonthlyPriceMinor: 0,
		MessageLimit:      10,
		Type:              domain.TariffB2C,
		IsBase:            true,
	}
	stored, err := repo.CreateTariff(ctx, base, "tariff-1", []byte("base"))
	require.NoError(t, err)
	require.Equal(t, base.ID, stored.ID)

	again, err := repo.CreateTariff(ctx, domain.Tariff{ID: uuid.New(), Name: "base", MessageLimit: 10, Type: domain.TariffB2C, IsBase: true}, "tariff-1", []byte("base"))
	require.NoError(t, err)
	require.Equal(t, base.ID, again.ID)

	_, err = repo.CreateTariff(ctx, domain.Tariff{ID: uuid.New(), Name: "other", MessageLimit: 10, Type: domain.TariffB2C, IsBase: true}, "tariff-2", []byte("other"))
	require.ErrorIs(t, err, domain.ErrBaseTariffExists)

	userID := uuid.New()
	start := time.Now().UTC().Add(-2 * time.Hour)
	sub := domain.Subscription{
		ID:               uuid.New(),
		UserID:           &userID,
		TariffID:         base.ID,
		Status:           domain.StatusActive,
		MessageAllowance: 4,
		PeriodStart:      start,
		PeriodEnd:        start.Add(time.Hour),
	}
	created, err := repo.CreateSubscription(ctx, sub, "sub-1", []byte("sub"))
	require.NoError(t, err)
	require.Equal(t, sub.ID, created.ID)

	_, err = repo.CreateSubscription(ctx, domain.Subscription{
		ID:               uuid.New(),
		UserID:           &userID,
		TariffID:         base.ID,
		Status:           domain.StatusActive,
		MessageAllowance: 10,
		PeriodStart:      start,
		PeriodEnd:        start.Add(2 * time.Hour),
	}, "sub-2", []byte("another"))
	require.ErrorIs(t, err, domain.ErrActiveSubscription)

	report, err := repo.CloseExpired(ctx, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, 1, report.Closed)
	require.Empty(t, report.Skipped)

	renewed, err := repo.Subscription(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StatusActive, renewed.Status)
	require.Equal(t, base.ID, renewed.TariffID)
	require.Equal(t, int64(10), renewed.MessageAllowance)
	require.True(t, renewed.PeriodEnd.After(time.Now()))
}

func startPostgres(t *testing.T, ctx context.Context) string {
	t.Helper()

	container, err := postgres.Run(ctx, "postgres:18.6-alpine3.24",
		postgres.WithDatabase("subscriptions"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(time.Minute),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, container.Terminate(context.Background()))
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, migrate.Up(ctx, dsn, subscriptionsmigrations.FS))

	return dsn
}
