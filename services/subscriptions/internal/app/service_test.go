package app_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/services/subscriptions/internal/app"
	"github.com/trb1maker/subscriptions/services/subscriptions/internal/domain"
)

func TestCreateTariffReplayAndBaseConflict(t *testing.T) {
	t.Parallel()

	svc := newService(t, newMemStore(), &fakeDirectory{}, time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC))
	admin := domain.Owner{ID: uuid.New(), Kind: domain.OwnerOrganization, Roles: []string{domain.RoleAdmin}}
	user := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser, Roles: []string{"user"}}

	_, err := svc.CreateTariff(context.Background(), user, "base", 0, 10, "b2c", true, "tariff-1")
	require.ErrorIs(t, err, domain.ErrForbidden)

	first, err := svc.CreateTariff(context.Background(), admin, "base", 0, 10, "b2c", true, "tariff-1")
	require.NoError(t, err)

	again, err := svc.CreateTariff(context.Background(), admin, "base", 0, 10, "b2c", true, "tariff-1")
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)

	_, err = svc.CreateTariff(context.Background(), admin, "other", 0, 10, "b2c", true, "tariff-1")
	require.ErrorIs(t, err, domain.ErrIdempotencyConflict)

	_, err = svc.CreateTariff(context.Background(), admin, "other", 0, 10, "b2c", true, "tariff-2")
	require.ErrorIs(t, err, domain.ErrBaseTariffExists)

	_, err = svc.CreateTariff(context.Background(), admin, "org", 100, 50, "b2b", true, "tariff-3")
	require.ErrorIs(t, err, domain.ErrTariffType)
}

func TestCreateSubscriptionRejectsOrganizationMemberAndSecondActive(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	orgID := uuid.New()
	directory := &fakeDirectory{orgs: map[uuid.UUID]uuid.UUID{}}
	memberID := uuid.New()
	directory.orgs[memberID] = orgID
	svc := newService(t, store, directory, time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC))
	admin := domain.Owner{ID: uuid.New(), Kind: domain.OwnerOrganization, Roles: []string{domain.RoleAdmin}}
	user := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser, Roles: []string{"user"}}

	tariff, err := svc.CreateTariff(context.Background(), admin, "personal", 100, 20, "b2c", false, "tariff-1")
	require.NoError(t, err)

	member := domain.Owner{ID: memberID, Kind: domain.OwnerUser}
	_, err = svc.CreateSubscription(context.Background(), member, tariff.ID, "sub-1")
	require.ErrorIs(t, err, domain.ErrOrganizationMember)

	first, err := svc.CreateSubscription(context.Background(), user, tariff.ID, "sub-1")
	require.NoError(t, err)
	require.Equal(t, int64(20), first.MessageAllowance)
	require.Equal(t, user.ID, *first.UserID)

	again, err := svc.CreateSubscription(context.Background(), user, tariff.ID, "sub-1")
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)

	_, err = svc.CreateSubscription(context.Background(), user, tariff.ID, "sub-2")
	require.ErrorIs(t, err, domain.ErrActiveSubscription)

	_, err = svc.GetSubscription(context.Background(), domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}, first.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestChangeSubscriptionKeepsPeriodAndCarriesAllowance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	svc := newService(t, newMemStore(), &fakeDirectory{}, now)
	admin := domain.Owner{ID: uuid.New(), Kind: domain.OwnerOrganization, Roles: []string{domain.RoleAdmin}}
	user := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}

	oldTariff, err := svc.CreateTariff(context.Background(), admin, "old", 100, 20, "b2c", false, "tariff-1")
	require.NoError(t, err)
	nextTariff, err := svc.CreateTariff(context.Background(), admin, "new", 200, 30, "b2c", false, "tariff-2")
	require.NoError(t, err)
	sub, err := svc.CreateSubscription(context.Background(), user, oldTariff.ID, "sub-1")
	require.NoError(t, err)

	changed, err := svc.ChangeSubscription(context.Background(), user, sub.ID, nextTariff.ID, "change-1")
	require.NoError(t, err)
	require.Equal(t, nextTariff.ID, changed.TariffID)
	require.Equal(t, sub.PeriodStart, changed.PeriodStart)
	require.Equal(t, sub.PeriodEnd, changed.PeriodEnd)
	require.Equal(t, int64(50), changed.MessageAllowance)

	replayed, err := svc.ChangeSubscription(context.Background(), user, sub.ID, nextTariff.ID, "change-1")
	require.NoError(t, err)
	require.Equal(t, changed.MessageAllowance, replayed.MessageAllowance)
}

func TestCheckSubscriptionUsesOrganizationAndIgnoresExpiredPeriod(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	store := newMemStore()
	orgID := uuid.New()
	userID := uuid.New()
	directory := &fakeDirectory{orgs: map[uuid.UUID]uuid.UUID{userID: orgID}}
	svc := newService(t, store, directory, now)
	admin := domain.Owner{ID: orgID, Kind: domain.OwnerOrganization, Roles: []string{domain.RoleAdmin}}

	tariff, err := svc.CreateTariff(context.Background(), admin, "team", 1000, 100, "b2b", false, "tariff-1")
	require.NoError(t, err)
	sub, err := svc.CreateSubscription(context.Background(), admin, tariff.ID, "sub-1")
	require.NoError(t, err)

	checked, err := svc.CheckSubscription(context.Background(), domain.Owner{ID: userID, Kind: domain.OwnerUser})
	require.NoError(t, err)
	require.True(t, checked.Active)
	require.Equal(t, sub.ID, checked.Subscription.ID)

	store.expire(sub.ID)
	checked, err = svc.CheckSubscription(context.Background(), admin)
	require.NoError(t, err)
	require.False(t, checked.Active)
}

func TestCloseExpiredSkipsPaidB2CWithoutBase(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	store := newMemStore()
	svc := newService(t, store, &fakeDirectory{}, now)
	admin := domain.Owner{ID: uuid.New(), Kind: domain.OwnerOrganization, Roles: []string{domain.RoleAdmin}}
	user := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}
	tariff, err := svc.CreateTariff(context.Background(), admin, "paid", 100, 10, "b2c", false, "tariff-1")
	require.NoError(t, err)
	sub, err := svc.CreateSubscription(context.Background(), user, tariff.ID, "sub-1")
	require.NoError(t, err)
	store.finishPeriod(sub.ID, now)

	report, err := svc.CloseExpired(context.Background())
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{sub.ID}, report.Skipped)
	require.Zero(t, report.Closed)
}

func TestCloseExpiredPublishesBurnedOrganizationLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	store := newMemStore()
	periods := &fakePeriods{}
	svc := newService(t, store, &fakeDirectory{}, now)
	svc.SetPeriods(periods)
	org := domain.Owner{ID: uuid.New(), Kind: domain.OwnerOrganization, Roles: []string{domain.RoleAdmin}}
	tariff, err := svc.CreateTariff(context.Background(), org, "team", 1000, 40, "b2b", false, "tariff-1")
	require.NoError(t, err)
	sub, err := svc.CreateSubscription(context.Background(), org, tariff.ID, "sub-1")
	require.NoError(t, err)
	store.finishPeriod(sub.ID, now)

	report, err := svc.CloseExpired(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, report.Closed)
	require.Len(t, periods.events, 1)
	require.Equal(t, org.ID, periods.events[0].Owner.ID)
	require.Equal(t, int64(0), periods.events[0].Allowance)
}

type fixtures struct {
	svc      *app.Service
	balances *fakeBalances
	payments *fakePayments
}

func newFixtures(t *testing.T, store *memStore, directory app.Directory, now time.Time) fixtures {
	t.Helper()

	balances := &fakeBalances{}
	payments := &fakePayments{}
	svc, err := app.New(store, directory, balances, payments, []byte("pepper"), func() time.Time { return now })
	require.NoError(t, err)

	return fixtures{svc: svc, balances: balances, payments: payments}
}

func newService(t *testing.T, store *memStore, directory app.Directory, now time.Time) *app.Service {
	t.Helper()

	return newFixtures(t, store, directory, now).svc
}

type fakeBalances struct {
	remaining int64
	err       error
	calls     int
}

func (f *fakeBalances) Remaining(context.Context, domain.Owner) (int64, error) {
	f.calls++

	return f.remaining, f.err
}

type fakePayments struct {
	events []app.PaymentNotice
	err    error
}

type fakePeriods struct {
	events []app.PeriodNotice
}

func (f *fakePeriods) PublishPeriodEnded(_ context.Context, event app.PeriodNotice) error {
	f.events = append(f.events, event)

	return nil
}

func (f *fakePayments) PublishPaymentReceived(_ context.Context, event app.PaymentNotice) error {
	if f.err != nil {
		return f.err
	}

	f.events = append(f.events, event)

	return nil
}

type fakeDirectory struct {
	orgs    map[uuid.UUID]uuid.UUID
	missing map[uuid.UUID]struct{}
}

func (f *fakeDirectory) Lookup(_ context.Context, id uuid.UUID, _ domain.OwnerKind) (domain.Subject, error) {
	if _, ok := f.missing[id]; ok {
		return domain.Subject{}, domain.ErrNotFound
	}

	orgID, ok := f.orgs[id]
	if !ok {
		return domain.Subject{}, nil
	}

	return domain.Subject{OrganizationID: &orgID}, nil
}

type storedCommand struct {
	hash       []byte
	resourceID uuid.UUID
}

type memStore struct {
	mu      sync.Mutex
	tariffs map[uuid.UUID]domain.Tariff
	subs    map[uuid.UUID]domain.Subscription
	keys    map[string]storedCommand
}

func newMemStore() *memStore {
	return &memStore{
		tariffs: map[uuid.UUID]domain.Tariff{},
		subs:    map[uuid.UUID]domain.Subscription{},
		keys:    map[string]storedCommand{},
	}
}

func (m *memStore) finishPeriod(id uuid.UUID, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sub := m.subs[id]
	sub.PeriodEnd = now
	m.subs[id] = sub
}

func (m *memStore) expire(id uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sub := m.subs[id]
	sub.Status = domain.StatusExpired
	m.subs[id] = sub
}

func (m *memStore) ReplayTariff(_ context.Context, key string, requestHash []byte) (domain.Tariff, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.replayTariff(key, requestHash)
}

func (m *memStore) replayTariff(key string, requestHash []byte) (domain.Tariff, error) {
	existing, ok := m.keys[key]
	if !ok {
		return domain.Tariff{}, domain.ErrNotFound
	}

	if string(existing.hash) != string(requestHash) {
		return domain.Tariff{}, domain.ErrIdempotencyConflict
	}

	tariff, ok := m.tariffs[existing.resourceID]
	if !ok {
		return domain.Tariff{}, domain.ErrIdempotencyConflict
	}

	return tariff, nil
}

func (m *memStore) CreateTariff(_ context.Context, tariff domain.Tariff, key string, requestHash []byte) (domain.Tariff, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, err := m.replayTariff(key, requestHash); err == nil || !isNotFound(err) {
		return existing, err
	}

	if tariff.IsBase {
		for _, stored := range m.tariffs {
			if stored.IsBase {
				return domain.Tariff{}, domain.ErrBaseTariffExists
			}
		}
	}

	m.tariffs[tariff.ID] = tariff
	m.keys[key] = storedCommand{hash: append([]byte(nil), requestHash...), resourceID: tariff.ID}

	return tariff, nil
}

func (m *memStore) ListTariffs(context.Context) ([]domain.Tariff, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]domain.Tariff, 0, len(m.tariffs))
	for _, tariff := range m.tariffs {
		out = append(out, tariff)
	}

	return out, nil
}

func (m *memStore) Tariff(_ context.Context, id uuid.UUID) (domain.Tariff, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	tariff, ok := m.tariffs[id]
	if !ok {
		return domain.Tariff{}, domain.ErrNotFound
	}

	return tariff, nil
}

func (m *memStore) ReplaySubscription(_ context.Context, key string, requestHash []byte) (domain.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.replaySubscription(key, requestHash)
}

func (m *memStore) replaySubscription(key string, requestHash []byte) (domain.Subscription, error) {
	existing, ok := m.keys[key]
	if !ok {
		return domain.Subscription{}, domain.ErrNotFound
	}

	if string(existing.hash) != string(requestHash) {
		return domain.Subscription{}, domain.ErrIdempotencyConflict
	}

	sub, ok := m.subs[existing.resourceID]
	if !ok {
		return domain.Subscription{}, domain.ErrIdempotencyConflict
	}

	return sub, nil
}

func (m *memStore) CreateSubscription(_ context.Context, sub domain.Subscription, key string, requestHash []byte) (domain.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, err := m.replaySubscription(key, requestHash); err == nil || !isNotFound(err) {
		return existing, err
	}

	for _, stored := range m.subs {
		if stored.Status != domain.StatusActive {
			continue
		}

		if sameOwner(stored, sub) {
			return domain.Subscription{}, domain.ErrActiveSubscription
		}
	}

	m.subs[sub.ID] = sub
	m.keys[key] = storedCommand{hash: append([]byte(nil), requestHash...), resourceID: sub.ID}

	return sub, nil
}

func (m *memStore) Subscription(_ context.Context, id uuid.UUID) (domain.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sub, ok := m.subs[id]
	if !ok {
		return domain.Subscription{}, domain.ErrNotFound
	}

	return sub, nil
}

func (m *memStore) ActiveByOwner(_ context.Context, id uuid.UUID, kind domain.OwnerKind) (domain.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, sub := range m.subs {
		if sub.Status != domain.StatusActive {
			continue
		}

		if kind == domain.OwnerUser && sub.UserID != nil && *sub.UserID == id {
			return sub, nil
		}

		if kind == domain.OwnerOrganization && sub.OrganizationID != nil && *sub.OrganizationID == id {
			return sub, nil
		}
	}

	return domain.Subscription{}, domain.ErrNotFound
}

func (m *memStore) ChangeSubscription(_ context.Context, id uuid.UUID, tariff domain.Tariff, key string, requestHash []byte) (domain.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, err := m.replaySubscription(key, requestHash); err == nil || !isNotFound(err) {
		return existing, err
	}

	sub, ok := m.subs[id]
	if !ok {
		return domain.Subscription{}, domain.ErrNotFound
	}

	old, ok := m.tariffs[sub.TariffID]
	if !ok {
		return domain.Subscription{}, domain.ErrNotFound
	}

	allowance, err := domain.AllowanceOnChange(old, tariff, sub.MessageAllowance)
	if err != nil {
		return domain.Subscription{}, err
	}

	sub.TariffID = tariff.ID
	sub.MessageAllowance = allowance
	m.subs[id] = sub
	m.keys[key] = storedCommand{hash: append([]byte(nil), requestHash...), resourceID: sub.ID}

	return sub, nil
}

func (m *memStore) ReplayPayment(ctx context.Context, key string, requestHash []byte) (domain.Subscription, error) {
	return m.ReplaySubscription(ctx, key, requestHash)
}

func (m *memStore) RenewSubscription(
	_ context.Context,
	id uuid.UUID,
	remaining, amountMinor int64,
	paymentID string,
	now time.Time,
	key string,
	requestHash []byte,
) (domain.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, err := m.replaySubscription(key, requestHash); err == nil || !isNotFound(err) {
		return existing, err
	}

	sub, ok := m.subs[id]
	if !ok {
		return domain.Subscription{}, domain.ErrNotFound
	}

	tariff, ok := m.tariffs[sub.TariffID]
	if !ok {
		return domain.Subscription{}, domain.ErrNotFound
	}

	next, err := domain.PlanPayment(sub, tariff, remaining, amountMinor, now, paymentID)
	if err != nil {
		return domain.Subscription{}, err
	}

	m.subs[id] = next
	m.keys[key] = storedCommand{hash: append([]byte(nil), requestHash...), resourceID: sub.ID}

	return next, nil
}

func (m *memStore) CloseExpired(_ context.Context, now time.Time) (domain.ExpiryReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var base *domain.Tariff
	for _, tariff := range m.tariffs {
		if tariff.IsBase {
			copied := tariff
			base = &copied
		}
	}

	var report domain.ExpiryReport
	for id, sub := range m.subs {
		if sub.Status != domain.StatusActive || sub.PeriodEnd.After(now) {
			continue
		}

		tariff, ok := m.tariffs[sub.TariffID]
		if !ok {
			return domain.ExpiryReport{}, domain.ErrNotFound
		}

		next, apply, err := domain.PlanExpiry(sub, tariff, base)
		if err != nil {
			return domain.ExpiryReport{}, err
		}

		if !apply {
			report.Skipped = append(report.Skipped, id)

			continue
		}

		m.subs[id] = next
		report.Closed++
		report.Applied = append(report.Applied, next)
	}

	return report, nil
}

func sameOwner(left, right domain.Subscription) bool {
	if left.UserID != nil && right.UserID != nil {
		return *left.UserID == *right.UserID
	}

	if left.OrganizationID != nil && right.OrganizationID != nil {
		return *left.OrganizationID == *right.OrganizationID
	}

	return false
}

func isNotFound(err error) bool {
	return errors.Is(err, domain.ErrNotFound)
}
