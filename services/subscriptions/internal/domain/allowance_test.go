package domain_test

import (
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/services/subscriptions/internal/domain"
)

func TestNextAllowance(t *testing.T) {
	t.Parallel()

	quota := int64(100)
	renewal, err := domain.NextAllowance(domain.AllowanceRenewal, quota, 40)
	require.NoError(t, err)
	require.Equal(t, quota, renewal)

	overdraft, err := domain.NextAllowance(domain.AllowanceRenewal, quota, -3)
	require.NoError(t, err)
	require.Equal(t, int64(97), overdraft)

	carried, err := domain.NextAllowance(domain.AllowanceFromPaid, quota, 40)
	require.NoError(t, err)
	require.Equal(t, int64(140), carried)

	carriedDebt, err := domain.NextAllowance(domain.AllowanceFromPaid, quota, -3)
	require.NoError(t, err)
	require.Equal(t, int64(97), carriedDebt)

	burned, err := domain.NextAllowance(domain.AllowanceFromBase, quota, 40)
	require.NoError(t, err)
	require.Equal(t, quota, burned)

	burnedDebt, err := domain.NextAllowance(domain.AllowanceFromBase, quota, -3)
	require.NoError(t, err)
	require.Equal(t, int64(97), burnedDebt)
}

func TestPlanExpiry(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	paid := domain.Tariff{ID: uuid.New(), Type: domain.TariffB2C, MessageLimit: 100}
	base := domain.Tariff{ID: uuid.New(), Type: domain.TariffB2C, MessageLimit: 10, IsBase: true}
	b2b := domain.Tariff{ID: uuid.New(), Type: domain.TariffB2B, MessageLimit: 1000}
	sub := domain.Subscription{
		ID:               uuid.New(),
		TariffID:         paid.ID,
		Status:           domain.StatusActive,
		MessageAllowance: 40,
		PeriodStart:      start,
		PeriodEnd:        end,
	}

	moved, apply, err := domain.PlanExpiry(sub, paid, &base)
	require.NoError(t, err)
	require.True(t, apply)
	require.Equal(t, base.ID, moved.TariffID)
	require.Equal(t, domain.StatusActive, moved.Status)
	require.Equal(t, end, moved.PeriodStart)
	require.Equal(t, end.AddDate(0, 1, 0), moved.PeriodEnd)
	require.Equal(t, int64(50), moved.MessageAllowance)

	unchanged, apply, err := domain.PlanExpiry(sub, paid, nil)
	require.NoError(t, err)
	require.False(t, apply)
	require.Equal(t, sub, unchanged)

	onBase := sub
	onBase.TariffID = base.ID
	onBase.MessageAllowance = 4
	renewed, apply, err := domain.PlanExpiry(onBase, base, &base)
	require.NoError(t, err)
	require.True(t, apply)
	require.Equal(t, base.ID, renewed.TariffID)
	require.Equal(t, int64(10), renewed.MessageAllowance)
	require.Equal(t, end, renewed.PeriodStart)

	org := sub
	org.TariffID = b2b.ID
	org.MessageAllowance = 40
	expired, apply, err := domain.PlanExpiry(org, b2b, &base)
	require.NoError(t, err)
	require.True(t, apply)
	require.Equal(t, domain.StatusExpired, expired.Status)
	require.Equal(t, int64(40), expired.MessageAllowance)
	require.Equal(t, end, expired.PeriodEnd)
}
