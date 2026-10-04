package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
	"uuid"

	"github.com/trb1maker/subscriptions/services/subscriptions/internal/domain"
)

// ProcessPayment продлевает активную подписку и публикует уже посчитанный остаток.
// Повтор того же payment_id не сдвигает период второй раз, но снова публикует то же событие.
func (s *Service) ProcessPayment(
	ctx context.Context,
	subscriptionID uuid.UUID,
	paymentID string,
	amountMinor int64,
	occurredAt time.Time,
) (domain.Subscription, error) {
	if subscriptionID == uuid.Nil() {
		return domain.Subscription{}, domain.ErrInvalidSubscriptionID
	}

	paymentUUID, err := uuid.Parse(paymentID)
	if err != nil || paymentUUID == uuid.Nil() {
		return domain.Subscription{}, domain.ErrInvalidIdempotencyKey
	}

	key, err := domain.ParseIdempotencyKey(paymentID)
	if err != nil {
		return domain.Subscription{}, err
	}

	if occurredAt.IsZero() {
		return domain.Subscription{}, domain.ErrInvalidArgument
	}

	if err := domain.ParsePrice(amountMinor); err != nil {
		return domain.Subscription{}, err
	}

	occurredAt = occurredAt.UTC()
	digest := s.requestHash(
		"receive_payment",
		paymentID,
		subscriptionID.String(),
		strconv.FormatInt(amountMinor, 10),
		occurredAt.Format(time.RFC3339Nano),
	)

	stored, err := s.store.ReplayPayment(ctx, key, digest)
	if err == nil {
		return stored, s.publishPayment(ctx, stored, paymentUUID, occurredAt, amountMinor)
	}

	if !errors.Is(err, domain.ErrNotFound) {
		return domain.Subscription{}, fmt.Errorf("replay payment: %w", err)
	}

	current, err := s.store.Subscription(ctx, subscriptionID)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("find subscription: %w", err)
	}

	tariff, err := s.store.Tariff(ctx, current.TariffID)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("find tariff: %w", err)
	}

	if _, err := domain.PlanPayment(current, tariff, 0, amountMinor, s.clock(), paymentID); err != nil {
		return domain.Subscription{}, err
	}

	owner, ok := current.Owner()
	if !ok {
		return domain.Subscription{}, domain.ErrInvalidArgument
	}

	remaining, err := s.balances.Remaining(ctx, owner)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("read balance: %w", err)
	}

	renewed, err := s.store.RenewSubscription(ctx, subscriptionID, remaining, amountMinor, paymentID, s.clock(), key, digest)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("renew subscription: %w", err)
	}

	if err := s.publishPayment(ctx, renewed, paymentUUID, occurredAt, amountMinor); err != nil {
		return domain.Subscription{}, err
	}

	return renewed, nil
}

func (s *Service) publishPayment(ctx context.Context, sub domain.Subscription, eventID uuid.UUID, occurredAt time.Time, amountMinor int64) error {
	owner, ok := sub.Owner()
	if !ok {
		return domain.ErrInvalidArgument
	}

	err := s.payments.PublishPaymentReceived(ctx, PaymentNotice{
		ID:          eventID,
		OccurredAt:  occurredAt,
		Owner:       owner,
		Allowance:   sub.MessageAllowance,
		PaymentID:   eventID.String(),
		AmountMinor: amountMinor,
	})
	if err != nil {
		return fmt.Errorf("publish payment: %w", err)
	}

	return nil
}
