package app

import (
	"context"
	"fmt"
	"uuid"

	"github.com/trb1maker/subscriptions/services/usage/internal/domain"
)

// CheckLimit разрешает генерацию, пока остаток владельца больше нуля.
// Пользователь организации проверяется по лимиту организации.
func (s *Service) CheckLimit(ctx context.Context, owner domain.Owner) (CheckResult, error) {
	if err := requireOwner(owner); err != nil {
		return CheckResult{}, err
	}

	billing, err := s.billingOwner(ctx, owner)
	if err != nil {
		return CheckResult{}, err
	}

	remaining, found, err := s.balances.Get(ctx, billing)
	if err != nil {
		return CheckResult{}, fmt.Errorf("read balance: %w", err)
	}

	if !found {
		return CheckResult{}, nil
	}

	return CheckResult{Allowed: remaining > 0, Remaining: remaining}, nil
}

func (s *Service) billingOwner(ctx context.Context, owner domain.Owner) (domain.Owner, error) {
	subject, err := s.directory.Lookup(ctx, owner.ID, owner.Kind)
	if err != nil {
		return domain.Owner{}, fmt.Errorf("lookup owner: %w", err)
	}

	if owner.Kind == domain.OwnerUser && subject.OrganizationID != nil {
		return domain.Owner{ID: *subject.OrganizationID, Kind: domain.OwnerOrganization}, nil
	}

	return owner, nil
}

func requireOwner(owner domain.Owner) error {
	if owner.ID == uuid.Nil() || !owner.Kind.Valid() {
		return domain.ErrUnauthenticated
	}

	return nil
}
