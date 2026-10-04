package grpcapi_test

import (
	"context"
	"log/slog"
	"testing"
	"uuid"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	usagev1 "github.com/trb1maker/subscriptions/api/gen/usage/v1"
	"github.com/trb1maker/subscriptions/pkg/caller"
	grpcapi "github.com/trb1maker/subscriptions/services/usage/internal/adapters/grpc"
	"github.com/trb1maker/subscriptions/services/usage/internal/app"
	"github.com/trb1maker/subscriptions/services/usage/internal/domain"
)

func TestCheckLimit(t *testing.T) {
	t.Parallel()

	owner := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}
	balances := &memBalances{values: map[domain.Owner]int64{owner: 2}}
	srv := newServer(t, balances)

	allowed, err := srv.CheckLimit(ownerContext(owner), &usagev1.CheckLimitRequest{})
	require.NoError(t, err)
	require.True(t, allowed.GetAllowed())
	require.Equal(t, int64(2), allowed.GetRemaining())
	require.Equal(t, string(owner.Kind), allowed.GetOwnerKind())
	require.Equal(t, owner.ID.String(), allowed.GetOwnerId())

	balances.values[owner] = -1
	overdraft, err := srv.CheckLimit(ownerContext(owner), &usagev1.CheckLimitRequest{})
	require.NoError(t, err)
	require.False(t, overdraft.GetAllowed())
	require.Equal(t, int64(-1), overdraft.GetRemaining())
	require.Equal(t, owner.ID.String(), overdraft.GetOwnerId())

	missing, err := srv.CheckLimit(ownerContext(domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}), &usagev1.CheckLimitRequest{})
	require.NoError(t, err)
	require.False(t, missing.GetAllowed())
	require.Empty(t, missing.GetOwnerId())
	require.Empty(t, missing.GetOwnerKind())

	_, err = srv.CheckLimit(context.Background(), &usagev1.CheckLimitRequest{})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func newServer(t *testing.T, balances app.Balances) *grpcapi.Server {
	t.Helper()

	svc, err := app.New(emptyLedger{}, balances, directory{})
	require.NoError(t, err)

	return grpcapi.NewServer(svc, slog.New(slog.DiscardHandler))
}

func ownerContext(owner domain.Owner) context.Context {
	return caller.NewContext(context.Background(), caller.Caller{
		SubjectID: owner.ID.String(),
		Kind:      string(owner.Kind),
	})
}

type directory struct{}

func (directory) Lookup(context.Context, uuid.UUID, domain.OwnerKind) (domain.Subject, error) {
	return domain.Subject{}, nil
}

type emptyLedger struct{}

func (emptyLedger) Find(context.Context, uuid.UUID) (domain.Event, bool, error) {
	return domain.Event{}, false, nil
}

func (emptyLedger) Append(context.Context, domain.Event) error {
	return nil
}

func (emptyLedger) List(context.Context) ([]domain.Event, error) {
	return nil, nil
}

func (emptyLedger) ListOwner(context.Context, domain.Owner) ([]domain.Event, error) {
	return nil, nil
}

type memBalances struct {
	values map[domain.Owner]int64
}

func (b *memBalances) Restore(context.Context, domain.Owner, int64, []uuid.UUID) error {
	return nil
}

func (b *memBalances) Get(_ context.Context, owner domain.Owner) (int64, bool, error) {
	value, found := b.values[owner]

	return value, found, nil
}
