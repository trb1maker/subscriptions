package domain

import "time"

// AllowanceMode — как из текущего остатка получить следующий.
type AllowanceMode int

const (
	// AllowanceRenewal продлевает тот же тариф: неизрасходованное сгорает, овердрафт остаётся.
	AllowanceRenewal AllowanceMode = iota + 1
	// AllowanceFromPaid переносит неизрасходованное платного тарифа и овердрафт.
	AllowanceFromPaid
	// AllowanceFromBase сжигает неизрасходованное базового тарифа и сохраняет овердрафт.
	AllowanceFromBase
)

// NextAllowance считает остаток на новый период.
func NextAllowance(mode AllowanceMode, quota, current int64) (int64, error) {
	switch mode {
	case AllowanceRenewal, AllowanceFromBase:
		return quota + overdraft(current), nil
	case AllowanceFromPaid:
		return quota + current, nil
	default:
		return 0, ErrInvalidArgument
	}
}

// AllowanceOnChange считает остаток при смене тарифа. Тот же тариф остаток не меняет.
func AllowanceOnChange(old, next Tariff, current int64) (int64, error) {
	if old.ID == next.ID {
		return current, nil
	}

	mode := AllowanceFromPaid
	if old.IsBase {
		mode = AllowanceFromBase
	}

	return NextAllowance(mode, int64(next.MessageLimit), current)
}

// PlanExpiry решает, что сделать с подпиской, у которой закончился период.
// false означает, что строку менять рано: для платного B2C нет базового тарифа.
func PlanExpiry(sub Subscription, tariff Tariff, base *Tariff) (Subscription, bool, error) {
	switch tariff.Type {
	case TariffB2B:
		sub.Status = StatusExpired

		return sub, true, nil
	case TariffB2C:
		if tariff.IsBase {
			allowance, err := NextAllowance(AllowanceRenewal, int64(tariff.MessageLimit), sub.MessageAllowance)
			if err != nil {
				return Subscription{}, false, err
			}

			sub.MessageAllowance = allowance
			sub.PeriodStart = sub.PeriodEnd
			sub.PeriodEnd = NextPeriodEnd(sub.PeriodStart)

			return sub, true, nil
		}

		if base == nil {
			return sub, false, nil
		}

		allowance, err := NextAllowance(AllowanceFromPaid, int64(base.MessageLimit), sub.MessageAllowance)
		if err != nil {
			return Subscription{}, false, err
		}

		sub.TariffID = base.ID
		sub.MessageAllowance = allowance
		sub.PeriodStart = sub.PeriodEnd
		sub.PeriodEnd = NextPeriodEnd(sub.PeriodStart)

		return sub, true, nil
	default:
		return sub, false, ErrInvalidTariffType
	}
}

// PlanPayment продлевает активную подписку текущим тарифом.
// remaining — живой остаток. Неизрасходованное сгорает, овердрафт остаётся.
// Пока период не кончился, сдвигается только его конец. Иначе новый период начинается с now.
func PlanPayment(sub Subscription, tariff Tariff, remaining, amountMinor int64, now time.Time, paymentID string) (Subscription, error) {
	if sub.Status != StatusActive {
		return Subscription{}, ErrSubscriptionInactive
	}

	if paymentID == "" || now.IsZero() || amountMinor != tariff.MonthlyPriceMinor {
		return Subscription{}, ErrInvalidArgument
	}

	allowance, err := NextAllowance(AllowanceRenewal, int64(tariff.MessageLimit), remaining)
	if err != nil {
		return Subscription{}, err
	}

	sub.MessageAllowance = allowance
	sub.PaymentID = paymentID
	if now.Before(sub.PeriodEnd) {
		sub.PeriodEnd = NextPeriodEnd(sub.PeriodEnd)

		return sub, nil
	}

	sub.PeriodStart = now
	sub.PeriodEnd = NextPeriodEnd(now)

	return sub, nil
}

func overdraft(current int64) int64 {
	if current < 0 {
		return current
	}

	return 0
}
