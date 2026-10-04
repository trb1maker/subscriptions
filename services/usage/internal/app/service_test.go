package app_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/services/usage/internal/app"
	"github.com/trb1maker/subscriptions/services/usage/internal/domain"
)

func TestApplyIsIdempotentAndFailedDoesNotSpend(t *testing.T) {
	t.Parallel()

	ledger := newLedger()
	balances := newBalances()
	svc := newService(t, ledger, balances, directory{})
	owner := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}
	when := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	payment := domain.Event{
		ID: uuid.New(), Type: domain.EventPaymentReceived, OccurredAt: when, Owner: owner,
		HasAllowance: true, Allowance: 2, PaymentID: "pay-1",
	}
	failed := domain.Event{
		ID: uuid.New(), Type: domain.EventMessageFailed, OccurredAt: when.Add(time.Second), Owner: owner, Tokens: 4,
	}
	spent := domain.Event{
		ID: uuid.New(), Type: domain.EventMessageProcessed, OccurredAt: when.Add(2 * time.Second), Owner: owner, Tokens: 1,
	}

	require.NoError(t, svc.Apply(context.Background(), payment))
	require.NoError(t, svc.Apply(context.Background(), payment))
	require.NoError(t, svc.Apply(context.Background(), failed))
	require.NoError(t, svc.Apply(context.Background(), spent))
	require.NoError(t, svc.Apply(context.Background(), spent))

	remaining, found, err := balances.Get(context.Background(), owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(1), remaining)
	require.Len(t, ledger.events, 3)
}

func TestApplyRejectsConflictingReplay(t *testing.T) {
	t.Parallel()

	svc := newService(t, newLedger(), newBalances(), directory{})
	owner := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}
	event := domain.Event{
		ID: uuid.New(), Type: domain.EventSubscriptionChanged, OccurredAt: time.Now().UTC(), Owner: owner,
		HasAllowance: true, Allowance: 3,
	}
	require.NoError(t, svc.Apply(context.Background(), event))

	event.Allowance = 9
	err := svc.Apply(context.Background(), event)
	require.ErrorIs(t, err, domain.ErrIdempotencyConflict)
}

func TestCheckLimitUsesOrganizationOfMember(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	orgID := uuid.New()
	balances := newBalances()
	org := domain.Owner{ID: orgID, Kind: domain.OwnerOrganization}
	require.NoError(t, balances.Restore(context.Background(), org, 5, nil))
	svc := newService(t, newLedger(), balances, directory{orgID: &orgID})

	result, err := svc.CheckLimit(context.Background(), domain.Owner{ID: userID, Kind: domain.OwnerUser})
	require.NoError(t, err)
	require.True(t, result.Allowed)
	require.Equal(t, int64(5), result.Remaining)
	require.Equal(t, org, result.Owner)

	require.NoError(t, balances.Restore(context.Background(), org, 0, nil))
	result, err = svc.CheckLimit(context.Background(), domain.Owner{ID: userID, Kind: domain.OwnerUser})
	require.NoError(t, err)
	require.False(t, result.Allowed)
	require.Equal(t, int64(0), result.Remaining)
	require.Equal(t, org, result.Owner)
}

func TestCheckLimitWithoutProjection(t *testing.T) {
	t.Parallel()

	svc := newService(t, newLedger(), newBalances(), directory{})
	caller := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}
	result, err := svc.CheckLimit(context.Background(), caller)
	require.NoError(t, err)
	require.False(t, result.Allowed)
	require.Equal(t, int64(0), result.Remaining)
	require.Equal(t, domain.Owner{}, result.Owner)

	_, err = svc.CheckLimit(context.Background(), domain.Owner{})
	require.ErrorIs(t, err, domain.ErrUnauthenticated)
}

func TestApplyFollowsEventTimeNotArrival(t *testing.T) {
	t.Parallel()

	balances := newBalances()
	svc := newService(t, newLedger(), balances, directory{})
	owner := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}
	when := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	spent := domain.Event{
		ID: uuid.New(), Type: domain.EventMessageProcessed, OccurredAt: when.Add(time.Second), Owner: owner, Tokens: 1,
	}
	payment := domain.Event{
		ID: uuid.New(), Type: domain.EventPaymentReceived, OccurredAt: when, Owner: owner,
		HasAllowance: true, Allowance: 10, PaymentID: "pay-1",
	}

	require.NoError(t, svc.Apply(context.Background(), spent))
	require.NoError(t, svc.Apply(context.Background(), payment))

	remaining, found, err := balances.Get(context.Background(), owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(9), remaining)
}

func TestApplyFailedFirstDoesNotSpend(t *testing.T) {
	t.Parallel()

	balances := newBalances()
	svc := newService(t, newLedger(), balances, directory{})
	owner := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}
	when := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

	require.NoError(t, svc.Apply(context.Background(), domain.Event{
		ID: uuid.New(), Type: domain.EventMessageFailed, OccurredAt: when, Owner: owner, Tokens: 4,
	}))

	remaining, found, err := balances.Get(context.Background(), owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(0), remaining)

	require.NoError(t, svc.Apply(context.Background(), domain.Event{
		ID: uuid.New(), Type: domain.EventMessageProcessed, OccurredAt: when.Add(time.Second), Owner: owner, Tokens: 1,
	}))

	remaining, found, err = balances.Get(context.Background(), owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(-1), remaining)
}

func TestRestoreRebuildsProjection(t *testing.T) {
	t.Parallel()

	ledger := newLedger()
	balances := newBalances()
	svc := newService(t, ledger, balances, directory{})
	owner := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}
	when := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	require.NoError(t, svc.Apply(context.Background(), domain.Event{
		ID: uuid.New(), Type: domain.EventPaymentReceived, OccurredAt: when, Owner: owner,
		HasAllowance: true, Allowance: 4, PaymentID: "pay-1",
	}))
	require.NoError(t, svc.Apply(context.Background(), domain.Event{
		ID: uuid.New(), Type: domain.EventMessageProcessed, OccurredAt: when.Add(time.Second), Owner: owner, Tokens: 1,
	}))

	balances.values = map[domain.Owner]balanceState{}
	require.NoError(t, svc.Restore(context.Background()))

	remaining, found, err := balances.Get(context.Background(), owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(3), remaining)
}

func newService(t *testing.T, ledger app.Ledger, balances app.Balances, directory app.Directory) *app.Service {
	t.Helper()

	svc, err := app.New(ledger, balances, directory)
	require.NoError(t, err)

	return svc
}

type directory struct {
	orgID *uuid.UUID
}

func (d directory) Lookup(context.Context, uuid.UUID, domain.OwnerKind) (domain.Subject, error) {
	return domain.Subject{OrganizationID: d.orgID}, nil
}

type memLedger struct {
	events map[uuid.UUID]domain.Event
}

func newLedger() *memLedger {
	return &memLedger{events: map[uuid.UUID]domain.Event{}}
}

func (l *memLedger) Find(_ context.Context, id uuid.UUID) (domain.Event, bool, error) {
	event, found := l.events[id]

	return event, found, nil
}

func (l *memLedger) Append(_ context.Context, event domain.Event) error {
	if _, found := l.events[event.ID]; found {
		return nil
	}

	l.events[event.ID] = event

	return nil
}

func (l *memLedger) List(context.Context) ([]domain.Event, error) {
	events := make([]domain.Event, 0, len(l.events))
	for _, event := range l.events {
		events = append(events, event)
	}

	return events, nil
}

func (l *memLedger) ListOwner(_ context.Context, owner domain.Owner) ([]domain.Event, error) {
	events := make([]domain.Event, 0, len(l.events))
	for _, event := range l.events {
		if event.Owner != owner {
			continue
		}

		events = append(events, event)
	}

	return events, nil
}

type balanceState struct {
	value int64
	found bool
}

type memBalances struct {
	values map[domain.Owner]balanceState
}

func newBalances() *memBalances {
	return &memBalances{values: map[domain.Owner]balanceState{}}
}

func (b *memBalances) Restore(_ context.Context, owner domain.Owner, balance int64, _ []uuid.UUID) error {
	b.values[owner] = balanceState{value: balance, found: true}

	return nil
}

func (b *memBalances) Get(_ context.Context, owner domain.Owner) (int64, bool, error) {
	state, found := b.values[owner]
	if !found || !state.found {
		return 0, false, nil
	}

	return state.value, true, nil
}
