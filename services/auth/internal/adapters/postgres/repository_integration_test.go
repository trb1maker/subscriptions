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

	authmigrations "github.com/trb1maker/subscriptions/migrations/auth"
	"github.com/trb1maker/subscriptions/pkg/migrate"
	pgpool "github.com/trb1maker/subscriptions/pkg/postgres"
	authpostgres "github.com/trb1maker/subscriptions/services/auth/internal/adapters/postgres"
	"github.com/trb1maker/subscriptions/services/auth/internal/domain"
)

func TestRepositoryRegisterAndOrganization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	dsn := startPostgres(t, ctx)
	pool, err := pgpool.NewPool(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	repo := authpostgres.NewRepository(pool)

	org, err := repo.CreateOrganization(ctx, domain.Organization{ID: uuid.New(), Name: "Acme"}, "org-1", []byte("org-hash"))
	require.NoError(t, err)

	again, err := repo.CreateOrganization(ctx, domain.Organization{ID: uuid.New(), Name: "Other"}, "org-1", []byte("org-hash"))
	require.NoError(t, err)
	require.Equal(t, org.ID, again.ID)

	_, err = repo.CreateOrganization(ctx, domain.Organization{ID: uuid.New(), Name: "Other"}, "org-1", []byte("other-hash"))
	require.ErrorIs(t, err, domain.ErrIdempotencyConflict)

	user := domain.User{
		ID:             uuid.New(),
		Email:          "ada@example.com",
		PasswordHash:   "hash",
		OrganizationID: &org.ID,
		Role:           domain.RoleUser,
	}
	stored, err := repo.RegisterUser(ctx, user, "user-1", []byte("user-hash"))
	require.NoError(t, err)
	require.Equal(t, user.ID, stored.ID)

	replayed, err := repo.RegisterUser(ctx, domain.User{ID: uuid.New(), Email: user.Email, Role: domain.RoleUser}, "user-1", []byte("user-hash"))
	require.NoError(t, err)
	require.Equal(t, user.ID, replayed.ID)
	require.Equal(t, user.PasswordHash, replayed.PasswordHash)

	_, err = repo.RegisterUser(ctx, user, "user-1", []byte("changed"))
	require.ErrorIs(t, err, domain.ErrIdempotencyConflict)

	_, err = repo.RegisterUser(ctx, domain.User{ID: uuid.New(), Email: user.Email, Role: domain.RoleUser}, "user-2", []byte("another"))
	require.ErrorIs(t, err, domain.ErrEmailTaken)

	missing := uuid.New()
	_, err = repo.RegisterUser(ctx, domain.User{
		ID:             uuid.New(),
		Email:          "other@example.com",
		PasswordHash:   "hash",
		OrganizationID: &missing,
		Role:           domain.RoleUser,
	}, "user-3", []byte("third"))
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound)

	replayedByRead, err := repo.ReplayUser(ctx, "user-1", []byte("user-hash"))
	require.NoError(t, err)
	require.Equal(t, user.ID, replayedByRead.ID)

	_, err = repo.ReplayUser(ctx, "missing", []byte("user-hash"))
	require.ErrorIs(t, err, domain.ErrNotFound)

	_, err = repo.ReplayUser(ctx, "user-1", []byte("changed"))
	require.ErrorIs(t, err, domain.ErrIdempotencyConflict)

	_, err = repo.ReplayUser(ctx, "org-1", []byte("org-hash"))
	require.ErrorIs(t, err, domain.ErrIdempotencyConflict)

	found, err := repo.UserByEmail(ctx, user.Email)
	require.NoError(t, err)
	require.Equal(t, user.ID, found.ID)
	require.Equal(t, &org.ID, found.OrganizationID)

	_, err = repo.UserByEmail(ctx, "missing@example.com")
	require.ErrorIs(t, err, domain.ErrNotFound)
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
	t.Cleanup(func() {
		require.NoError(t, container.Terminate(context.Background()))
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, migrate.Up(ctx, dsn, authmigrations.FS))

	return dsn
}
