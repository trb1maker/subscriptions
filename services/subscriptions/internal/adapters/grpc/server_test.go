package grpcapi_test

import (
	"context"
	"io"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	subscriptionsv1 "github.com/trb1maker/subscriptions/api/gen/subscriptions/v1"
	"github.com/trb1maker/subscriptions/pkg/caller"
	"github.com/trb1maker/subscriptions/pkg/logger"
	grpcapi "github.com/trb1maker/subscriptions/services/subscriptions/internal/adapters/grpc"
	"github.com/trb1maker/subscriptions/services/subscriptions/internal/app"
	"github.com/trb1maker/subscriptions/services/subscriptions/internal/domain"
)

func TestCheckSubscription(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	owner := uuid.New()
	other := uuid.New()
	tariffID := uuid.New()
	activeID := uuid.New()
	store := &memStore{subs: map[uuid.UUID]domain.Subscription{
		activeID: {
			ID:               activeID,
			UserID:           &owner,
			TariffID:         tariffID,
			Status:           domain.StatusActive,
			MessageAllowance: 15,
			PeriodStart:      now.Add(-time.Hour),
			PeriodEnd:        now.Add(time.Hour),
		},
		uuid.New(): {
			ID:               uuid.New(),
			UserID:           &other,
			TariffID:         tariffID,
			Status:           domain.StatusActive,
			MessageAllowance: 15,
			PeriodStart:      now.Add(-time.Hour),
			PeriodEnd:        now.Add(time.Hour),
		},
	}}
	expiredID := uuid.New()
	store.subs[expiredID] = domain.Subscription{
		ID:               expiredID,
		UserID:           &owner,
		TariffID:         tariffID,
		Status:           domain.StatusExpired,
		MessageAllowance: 15,
		PeriodStart:      now.Add(-2 * time.Hour),
		PeriodEnd:        now.Add(-time.Hour),
	}
	srv := newServer(t, store, now)

	active, err := srv.CheckSubscription(ownerContext(owner), &subscriptionsv1.CheckSubscriptionRequest{})
	require.NoError(t, err)
	require.True(t, active.GetActive())
	require.Equal(t, activeID.String(), active.GetSubscriptionId())
	require.Equal(t, tariffID.String(), active.GetTariffId())
	require.Equal(t, int64(15), active.GetMessageLimit())
	require.NotEmpty(t, active.GetCurrentPeriodEnd())

	foreign, err := srv.CheckSubscription(ownerContext(uuid.New()), &subscriptionsv1.CheckSubscriptionRequest{})
	require.NoError(t, err)
	require.False(t, foreign.GetActive())
	require.Empty(t, foreign.GetSubscriptionId())

	delete(store.subs, activeID)
	inactive, err := srv.CheckSubscription(ownerContext(owner), &subscriptionsv1.CheckSubscriptionRequest{})
	require.NoError(t, err)
	require.False(t, inactive.GetActive())
}

func TestProcessPaymentDoesNotRequireCaller(t *testing.T) {
	t.Parallel()

	srv := newServer(t, &memStore{}, time.Now())
	_, err := srv.ProcessPayment(context.Background(), &subscriptionsv1.ProcessPaymentRequest{})
	requireCode(t, err, codes.InvalidArgument)
}

func TestCreateTariffForbiddenAndMissingCaller(t *testing.T) {
	t.Parallel()

	srv := newServer(t, &memStore{subs: map[uuid.UUID]domain.Subscription{}}, time.Now())
	_, err := srv.CreateTariff(ownerContext(uuid.New()), &subscriptionsv1.CreateTariffRequest{
		IdempotencyKey:    "tariff-1",
		Name:              "base",
		MonthlyPriceMinor: 0,
		MessageLimit:      10,
		Type:              "b2c",
		IsBaseTariff:      true,
	})
	requireCode(t, err, codes.PermissionDenied)

	_, err = srv.CheckSubscription(context.Background(), &subscriptionsv1.CheckSubscriptionRequest{})
	requireCode(t, err, codes.Unauthenticated)
}

func newServer(t *testing.T, store *memStore, now time.Time) *grpcapi.Server {
	t.Helper()

	log, err := logger.New(io.Discard, "error")
	require.NoError(t, err)
	svc, err := app.New(store, directory{}, fakeBalances{}, fakePayments{}, []byte("pepper"), func() time.Time { return now })
	require.NoError(t, err)

	return grpcapi.NewServer(svc, log)
}

func ownerContext(id uuid.UUID) context.Context {
	return caller.NewContext(context.Background(), caller.Caller{SubjectID: id.String(), Kind: string(domain.OwnerUser)})
}

func requireCode(t *testing.T, err error, code codes.Code) {
	t.Helper()

	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, code, st.Code())
}

type directory struct{}

func (directory) Lookup(context.Context, uuid.UUID, domain.OwnerKind) (domain.Subject, error) {
	return domain.Subject{}, nil
}

type memStore struct {
	subs map[uuid.UUID]domain.Subscription
}

func (m *memStore) ReplayTariff(context.Context, string, []byte) (domain.Tariff, error) {
	return domain.Tariff{}, domain.ErrNotFound
}

func (m *memStore) CreateTariff(context.Context, domain.Tariff, string, []byte) (domain.Tariff, error) {
	return domain.Tariff{}, domain.ErrNotFound
}

func (m *memStore) ListTariffs(context.Context) ([]domain.Tariff, error) {
	return nil, nil
}

func (m *memStore) Tariff(context.Context, uuid.UUID) (domain.Tariff, error) {
	return domain.Tariff{}, domain.ErrNotFound
}

func (m *memStore) ReplaySubscription(context.Context, string, []byte) (domain.Subscription, error) {
	return domain.Subscription{}, domain.ErrNotFound
}

func (m *memStore) CreateSubscription(context.Context, domain.Subscription, string, []byte) (domain.Subscription, error) {
	return domain.Subscription{}, domain.ErrNotFound
}

func (m *memStore) Subscription(context.Context, uuid.UUID) (domain.Subscription, error) {
	return domain.Subscription{}, domain.ErrNotFound
}

func (m *memStore) ActiveByOwner(_ context.Context, id uuid.UUID, kind domain.OwnerKind) (domain.Subscription, error) {
	for _, sub := range m.subs {
		if sub.Status != domain.StatusActive {
			continue
		}

		if kind == domain.OwnerUser && sub.UserID != nil && *sub.UserID == id {
			return sub, nil
		}
	}

	return domain.Subscription{}, domain.ErrNotFound
}

func (m *memStore) ChangeSubscription(context.Context, uuid.UUID, domain.Tariff, string, []byte) (domain.Subscription, error) {
	return domain.Subscription{}, domain.ErrNotFound
}

func (m *memStore) ReplayPayment(context.Context, string, []byte) (domain.Subscription, error) {
	return domain.Subscription{}, domain.ErrNotFound
}

func (m *memStore) RenewSubscription(context.Context, uuid.UUID, int64, int64, string, time.Time, string, []byte) (domain.Subscription, error) {
	return domain.Subscription{}, domain.ErrNotFound
}

type fakeBalances struct{}

func (fakeBalances) Remaining(context.Context, domain.Owner) (int64, error) {
	return 0, nil
}

type fakePayments struct{}

func (fakePayments) PublishPaymentReceived(context.Context, app.PaymentNotice) error {
	return nil
}

func (m *memStore) CloseExpired(context.Context, time.Time) (domain.ExpiryReport, error) {
	return domain.ExpiryReport{}, nil
}
