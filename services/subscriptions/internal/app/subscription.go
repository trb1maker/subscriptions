package app

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/trb1maker/subscriptions/services/subscriptions/internal/domain"
)

// CreateSubscription создаёт активную подписку владельца на календарный месяц.
func (s *Service) CreateSubscription(ctx context.Context, owner domain.Owner, tariffID uuid.UUID, idempotencyKey string) (domain.Subscription, error) {
	if err := requireOwner(owner); err != nil {
		return domain.Subscription{}, err
	}

	key, err := domain.ParseIdempotencyKey(idempotencyKey)
	if err != nil {
		return domain.Subscription{}, err
	}

	if tariffID == uuid.Nil() {
		return domain.Subscription{}, domain.ErrInvalidTariffID
	}

	subject, err := s.directory.Lookup(ctx, owner.ID, owner.Kind)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("lookup owner: %w", err)
	}

	if owner.Kind == domain.OwnerUser && subject.OrganizationID != nil {
		return domain.Subscription{}, domain.ErrOrganizationMember
	}

	tariff, err := s.store.Tariff(ctx, tariffID)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("find tariff: %w", err)
	}

	if !tariff.Matches(owner.Kind) {
		return domain.Subscription{}, domain.ErrTariffType
	}

	digest := s.requestHash("create_subscription", owner.ID.String(), string(owner.Kind), tariffID.String())
	sub, err := s.store.ReplaySubscription(ctx, key, digest)
	if errors.Is(err, domain.ErrNotFound) {
		sub, err = s.store.CreateSubscription(ctx, newSubscription(owner, tariff, s.clock()), key, digest)
	}

	if err != nil {
		return domain.Subscription{}, fmt.Errorf("create subscription: %w", err)
	}

	return sub, nil
}

// ChangeSubscription меняет тариф и пересчитывает остаток. Даты периода сохраняются.
func (s *Service) ChangeSubscription(
	ctx context.Context,
	owner domain.Owner,
	subscriptionID, tariffID uuid.UUID,
	idempotencyKey string,
) (domain.Subscription, error) {
	if err := requireOwner(owner); err != nil {
		return domain.Subscription{}, err
	}

	key, err := domain.ParseIdempotencyKey(idempotencyKey)
	if err != nil {
		return domain.Subscription{}, err
	}

	if subscriptionID == uuid.Nil() {
		return domain.Subscription{}, domain.ErrInvalidSubscriptionID
	}

	if tariffID == uuid.Nil() {
		return domain.Subscription{}, domain.ErrInvalidTariffID
	}

	digest := s.requestHash(
		"change_subscription",
		owner.ID.String(),
		string(owner.Kind),
		subscriptionID.String(),
		tariffID.String(),
	)
	sub, err := s.store.ReplaySubscription(ctx, key, digest)
	if err == nil {
		if !sub.OwnedBy(owner) {
			return domain.Subscription{}, domain.ErrNotFound
		}

		return sub, nil
	}

	if !errors.Is(err, domain.ErrNotFound) {
		return domain.Subscription{}, fmt.Errorf("replay subscription: %w", err)
	}

	current, err := s.store.Subscription(ctx, subscriptionID)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("find subscription: %w", err)
	}

	if !current.OwnedBy(owner) {
		return domain.Subscription{}, domain.ErrNotFound
	}

	tariff, err := s.store.Tariff(ctx, tariffID)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("find tariff: %w", err)
	}

	if !tariff.Matches(owner.Kind) {
		return domain.Subscription{}, domain.ErrTariffType
	}

	changed, err := s.store.ChangeSubscription(ctx, subscriptionID, tariff, key, digest)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("change subscription: %w", err)
	}

	return changed, nil
}

// GetSubscription возвращает подписку владельца.
func (s *Service) GetSubscription(ctx context.Context, owner domain.Owner, subscriptionID uuid.UUID) (domain.Subscription, error) {
	if err := requireOwner(owner); err != nil {
		return domain.Subscription{}, err
	}

	if subscriptionID == uuid.Nil() {
		return domain.Subscription{}, domain.ErrInvalidSubscriptionID
	}

	sub, err := s.store.Subscription(ctx, subscriptionID)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("find subscription: %w", err)
	}

	if !sub.OwnedBy(owner) {
		return domain.Subscription{}, domain.ErrNotFound
	}

	return sub, nil
}

func newSubscription(owner domain.Owner, tariff domain.Tariff, start time.Time) domain.Subscription {
	sub := domain.Subscription{
		ID:               uuid.New(),
		TariffID:         tariff.ID,
		Status:           domain.StatusActive,
		MessageAllowance: int64(tariff.MessageLimit),
		PeriodStart:      start,
		PeriodEnd:        domain.NextPeriodEnd(start),
	}
	id := owner.ID
	if owner.Kind == domain.OwnerOrganization {
		sub.OrganizationID = &id
	} else {
		sub.UserID = &id
	}

	return sub
}
