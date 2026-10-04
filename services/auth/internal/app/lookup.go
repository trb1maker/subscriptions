package app

import (
	"context"
	"fmt"
	"uuid"

	"github.com/trb1maker/subscriptions/services/auth/internal/domain"
)

// LookupSubject проверяет, что пользователь или организация существуют.
// Для пользователя возвращает организацию, если он к ней привязан.
func (s *Service) LookupSubject(ctx context.Context, id uuid.UUID, kind domain.PrincipalType) (*uuid.UUID, error) {
	switch kind {
	case domain.PrincipalUser:
		user, err := s.store.UserByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("lookup user: %w", err)
		}

		return user.OrganizationID, nil
	case domain.PrincipalOrganization:
		if _, err := s.store.OrganizationByID(ctx, id); err != nil {
			return nil, fmt.Errorf("lookup organization: %w", err)
		}

		return nil, nil
	default:
		return nil, domain.ErrInvalidSubject
	}
}
