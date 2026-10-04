package app

import (
	"context"
	"errors"
	"fmt"

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

	return report, nil
}
