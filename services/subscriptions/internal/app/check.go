package app

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/trb1maker/subscriptions/services/subscriptions/internal/domain"
)

// CheckSubscription сообщает, действует ли подписка вызывающего.
// Пользователь организации проверяется по подписке организации.
// Отсутствие подписки — неактивный результат. Отсутствие субъекта в Auth остаётся ошибкой.
func (s *Service) CheckSubscription(ctx context.Context, owner domain.Owner) (CheckResult, error) {
	if err := requireOwner(owner); err != nil {
		return CheckResult{}, err
	}

	sub, found, err := s.subscriptionFor(ctx, owner)
	if err != nil {
		return CheckResult{}, err
	}

	if !found || !sub.ActiveAt(s.clock()) {
		return CheckResult{}, nil
	}

	return CheckResult{Active: true, Subscription: sub}, nil
}

func (s *Service) subscriptionFor(ctx context.Context, owner domain.Owner) (domain.Subscription, bool, error) {
	ownerID := owner.ID
	kind := owner.Kind
	if owner.Kind == domain.OwnerUser {
		subject, err := s.directory.Lookup(ctx, owner.ID, owner.Kind)
		if err != nil {
			return domain.Subscription{}, false, fmt.Errorf("lookup owner: %w", err)
		}

		if subject.OrganizationID != nil {
			ownerID = *subject.OrganizationID
			kind = domain.OwnerOrganization
		}
	}

	sub, err := s.store.ActiveByOwner(ctx, ownerID, kind)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Subscription{}, false, nil
	}

	if err != nil {
		return domain.Subscription{}, false, fmt.Errorf("find subscription: %w", err)
	}

	return sub, true, nil
}

// CloseExpired переводит просроченные подписки по правилам тарифа.
func (s *Service) CloseExpired(ctx context.Context) (domain.ExpiryReport, error) {
	report, err := s.store.CloseExpired(ctx, s.clock())
	if err != nil {
		return domain.ExpiryReport{}, fmt.Errorf("close expired: %w", err)
	}

	if err := s.publishPeriods(ctx, report.Applied); err != nil {
		return report, err
	}

	return report, nil
}

func (s *Service) publishPeriods(ctx context.Context, applied []domain.Subscription) error {
	if s.periods == nil || len(applied) == 0 {
		return nil
	}

	now := s.clock().UTC()
	for _, sub := range applied {
		owner, ok := sub.Owner()
		if !ok {
			return domain.ErrInvalidArgument
		}

		allowance := sub.MessageAllowance
		if sub.Status == domain.StatusExpired {
			allowance = 0
		}

		err := s.periods.PublishPeriodEnded(ctx, PeriodNotice{
			ID:         uuid.New(),
			OccurredAt: now,
			Owner:      owner,
			Allowance:  allowance,
		})
		if err != nil {
			return fmt.Errorf("publish period ended: %w", err)
		}
	}

	return nil
}
