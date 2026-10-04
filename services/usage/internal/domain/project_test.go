package domain_test

import (
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/services/usage/internal/domain"
)

func TestProjectAppliesSetsAndUniqueDecrements(t *testing.T) {
	t.Parallel()

	owner := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}
	start := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	payment := setEvent(owner, domain.EventPaymentReceived, start, 10)
	payment.PaymentID = "pay-1"
	failed := messageEvent(owner, domain.EventMessageFailed, start.Add(time.Second), 3)
	first := messageEvent(owner, domain.EventMessageProcessed, start.Add(2*time.Second), 5)
	second := messageEvent(owner, domain.EventMessageProcessed, start.Add(3*time.Second), 7)

	balance, err := domain.Project([]domain.Event{payment, first, failed, second, first})
	require.NoError(t, err)
	require.Equal(t, int64(8), balance)
}

func TestProjectReplacesBalanceOnLaterSet(t *testing.T) {
	t.Parallel()

	owner := domain.Owner{ID: uuid.New(), Kind: domain.OwnerOrganization}
	start := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	payment := setEvent(owner, domain.EventPaymentReceived, start, 10)
	payment.PaymentID = "pay-1"
	spent := messageEvent(owner, domain.EventMessageProcessed, start.Add(time.Second), 1)
	changed := setEvent(owner, domain.EventSubscriptionChanged, start.Add(2*time.Second), 4)
	ended := setEvent(owner, domain.EventSubscriptionPeriodEnded, start.Add(3*time.Second), 0)

	balance, err := domain.Project([]domain.Event{ended, spent, payment, changed})
	require.NoError(t, err)
	require.Equal(t, int64(0), balance)
}

func TestProjectKeepsOverdraftWithoutBaseline(t *testing.T) {
	t.Parallel()

	owner := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}
	start := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	first := messageEvent(owner, domain.EventMessageProcessed, start, 1)
	second := messageEvent(owner, domain.EventMessageProcessed, start.Add(time.Second), 1)

	balance, err := domain.Project([]domain.Event{first, second})
	require.NoError(t, err)
	require.Equal(t, int64(-2), balance)
}

func TestProjectRejectsConflictingDuplicate(t *testing.T) {
	t.Parallel()

	owner := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}
	start := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	event := setEvent(owner, domain.EventSubscriptionChanged, start, 4)
	other := event
	other.Allowance = 9

	_, err := domain.Project([]domain.Event{event, other})
	require.ErrorIs(t, err, domain.ErrIdempotencyConflict)
}

func TestProjectUsesIdentifierWhenTimeMatches(t *testing.T) {
	t.Parallel()

	owner := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}
	when := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	earlierID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	laterID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	baseline := setEvent(owner, domain.EventSubscriptionChanged, when, 10)
	baseline.ID = earlierID
	spent := messageEvent(owner, domain.EventMessageProcessed, when, 1)
	spent.ID = laterID

	balance, err := domain.Project([]domain.Event{baseline, spent})
	require.NoError(t, err)
	require.Equal(t, int64(9), balance)

	spent.ID = earlierID
	baseline.ID = laterID
	balance, err = domain.Project([]domain.Event{baseline, spent})
	require.NoError(t, err)
	require.Equal(t, int64(10), balance)
}

func TestLimitKeyUsesOwnerScope(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	user := domain.Owner{ID: id, Kind: domain.OwnerUser}
	org := domain.Owner{ID: id, Kind: domain.OwnerOrganization}

	require.Equal(t, "limit:user:"+id.String(), user.LimitKey())
	require.Equal(t, "limit:org:"+id.String(), org.LimitKey())
	require.Equal(t, "seen:org:"+id.String(), org.SeenKey())
}

func setEvent(owner domain.Owner, kind domain.EventType, at time.Time, allowance int64) domain.Event {
	return domain.Event{
		ID:           uuid.New(),
		Type:         kind,
		OccurredAt:   at,
		Owner:        owner,
		Allowance:    allowance,
		HasAllowance: true,
	}
}

func messageEvent(owner domain.Owner, kind domain.EventType, at time.Time, tokens int64) domain.Event {
	return domain.Event{
		ID:         uuid.New(),
		Type:       kind,
		OccurredAt: at,
		Owner:      owner,
		Tokens:     tokens,
	}
}
