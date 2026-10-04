package app_test

import (
	"context"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/services/subscriptions/internal/app"
	"github.com/trb1maker/subscriptions/services/subscriptions/internal/domain"
)

func TestProcessPaymentRenewsOnceAndRepublishes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	store := newMemStore()
	fx := newFixtures(t, store, &fakeDirectory{}, now)
	admin := domain.Owner{ID: uuid.New(), Kind: domain.OwnerOrganization, Roles: []string{domain.RoleAdmin}}
	user := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}
	tariff, err := fx.svc.CreateTariff(context.Background(), admin, "personal", 100, 20, "b2c", false, "tariff-1")
	require.NoError(t, err)
	sub, err := fx.svc.CreateSubscription(context.Background(), user, tariff.ID, "sub-1")
	require.NoError(t, err)

	fx.balances.remaining = 7
	paymentID := uuid.New()
	occurredAt := now.Add(time.Hour)
	paid, err := fx.svc.ProcessPayment(context.Background(), sub.ID, paymentID.String(), 100, occurredAt)
	require.NoError(t, err)
	require.Equal(t, int64(20), paid.MessageAllowance)
	require.Equal(t, sub.PeriodStart, paid.PeriodStart)
	require.Equal(t, sub.PeriodEnd.AddDate(0, 1, 0), paid.PeriodEnd)
	require.Equal(t, paymentID.String(), paid.PaymentID)
	require.Equal(t, 1, fx.balances.calls)
	require.Len(t, fx.payments.events, 1)
	require.Equal(t, paymentID, fx.payments.events[0].ID)
	require.Equal(t, int64(20), fx.payments.events[0].Allowance)
	require.Equal(t, user.ID, fx.payments.events[0].Owner.ID)

	again, err := fx.svc.ProcessPayment(context.Background(), sub.ID, paymentID.String(), 100, occurredAt)
	require.NoError(t, err)
	require.Equal(t, paid.PeriodEnd, again.PeriodEnd)
	require.Equal(t, 1, fx.balances.calls)
	require.Len(t, fx.payments.events, 2)
	require.Equal(t, fx.payments.events[0], fx.payments.events[1])

	_, err = fx.svc.ProcessPayment(context.Background(), sub.ID, paymentID.String(), 90, occurredAt)
	require.ErrorIs(t, err, domain.ErrIdempotencyConflict)
	require.Len(t, fx.payments.events, 2)
}

func TestProcessPaymentKeepsOverdraftAndRejectsInactive(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	store := newMemStore()
	fx := newFixtures(t, store, &fakeDirectory{}, now)
	admin := domain.Owner{ID: uuid.New(), Kind: domain.OwnerOrganization, Roles: []string{domain.RoleAdmin}}
	user := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}
	tariff, err := fx.svc.CreateTariff(context.Background(), admin, "personal", 100, 20, "b2c", false, "tariff-1")
	require.NoError(t, err)
	sub, err := fx.svc.CreateSubscription(context.Background(), user, tariff.ID, "sub-1")
	require.NoError(t, err)

	_, err = fx.svc.ProcessPayment(context.Background(), sub.ID, uuid.New().String(), 50, now)
	require.ErrorIs(t, err, domain.ErrInvalidArgument)
	require.Zero(t, fx.balances.calls)

	fx.balances.remaining = -3
	store.finishPeriod(sub.ID, now)
	paid, err := fx.svc.ProcessPayment(context.Background(), sub.ID, uuid.New().String(), 100, now)
	require.NoError(t, err)
	require.Equal(t, int64(17), paid.MessageAllowance)
	require.Equal(t, now, paid.PeriodStart)
	require.Equal(t, now.AddDate(0, 1, 0), paid.PeriodEnd)

	store.expire(sub.ID)
	_, err = fx.svc.ProcessPayment(context.Background(), sub.ID, uuid.New().String(), 100, now)
	require.ErrorIs(t, err, domain.ErrSubscriptionInactive)
	require.Equal(t, 1, fx.balances.calls)
}

func TestProcessPaymentRetriesPublishAfterCommit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	store := newMemStore()
	fx := newFixtures(t, store, &fakeDirectory{}, now)
	admin := domain.Owner{ID: uuid.New(), Kind: domain.OwnerOrganization, Roles: []string{domain.RoleAdmin}}
	user := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}
	tariff, err := fx.svc.CreateTariff(context.Background(), admin, "personal", 100, 20, "b2c", false, "tariff-1")
	require.NoError(t, err)
	sub, err := fx.svc.CreateSubscription(context.Background(), user, tariff.ID, "sub-1")
	require.NoError(t, err)

	fx.payments.err = domain.ErrUnavailable
	paymentID := uuid.New().String()
	_, err = fx.svc.ProcessPayment(context.Background(), sub.ID, paymentID, 100, now)
	require.ErrorIs(t, err, domain.ErrUnavailable)

	fx.payments.err = nil
	paid, err := fx.svc.ProcessPayment(context.Background(), sub.ID, paymentID, 100, now)
	require.NoError(t, err)
	require.Equal(t, sub.PeriodEnd.AddDate(0, 1, 0), paid.PeriodEnd)
	require.Len(t, fx.payments.events, 1)
	require.Equal(t, paymentID, fx.payments.events[0].ID.String())
}

func TestProcessPaymentCountsOnce(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	store := newMemStore()
	fx := newFixtures(t, store, &fakeDirectory{}, now)
	rec := &paymentRecorder{}
	fx.svc.SetMetrics(rec)
	admin := domain.Owner{ID: uuid.New(), Kind: domain.OwnerOrganization, Roles: []string{domain.RoleAdmin}}
	user := domain.Owner{ID: uuid.New(), Kind: domain.OwnerUser}
	tariff, err := fx.svc.CreateTariff(context.Background(), admin, "personal", 100, 20, "b2c", false, "tariff-1")
	require.NoError(t, err)
	sub, err := fx.svc.CreateSubscription(context.Background(), user, tariff.ID, "sub-1")
	require.NoError(t, err)

	_, err = fx.svc.ProcessPayment(context.Background(), sub.ID, "not-a-uuid", 100, now)
	require.Error(t, err)
	require.Empty(t, rec.got)

	paymentID := uuid.New().String()
	fx.payments.err = domain.ErrUnavailable
	_, err = fx.svc.ProcessPayment(context.Background(), sub.ID, paymentID, 100, now)
	require.ErrorIs(t, err, domain.ErrUnavailable)
	require.Equal(t, []string{app.PaymentStatusReceived}, rec.got)

	fx.payments.err = nil
	_, err = fx.svc.ProcessPayment(context.Background(), sub.ID, paymentID, 100, now)
	require.NoError(t, err)
	require.Equal(t, []string{app.PaymentStatusReceived}, rec.got)

	_, err = fx.svc.ProcessPayment(context.Background(), sub.ID, uuid.New().String(), 50, now)
	require.ErrorIs(t, err, domain.ErrInvalidArgument)
	require.Equal(t, []string{app.PaymentStatusReceived, app.PaymentStatusFailed}, rec.got)

	store.expire(sub.ID)
	_, err = fx.svc.ProcessPayment(context.Background(), sub.ID, uuid.New().String(), 100, now)
	require.ErrorIs(t, err, domain.ErrSubscriptionInactive)
	require.Equal(t, []string{app.PaymentStatusReceived, app.PaymentStatusFailed, app.PaymentStatusFailed}, rec.got)
}

type paymentRecorder struct {
	got []string
}

func (r *paymentRecorder) Payment(status string) {
	r.got = append(r.got, status)
}
