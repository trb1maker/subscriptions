package app

import (
	"context"
	"fmt"
	"uuid"

	"github.com/trb1maker/subscriptions/services/auth/internal/domain"
)

// CreateOrganization создаёт организацию и JWT с типом organization. Повтор ключа не создаёт вторую организацию.
func (s *Service) CreateOrganization(ctx context.Context, name, idempotencyKey string) (OrganizationCreated, error) {
	key, err := domain.ParseIdempotencyKey(idempotencyKey)
	if err != nil {
		return OrganizationCreated{}, err
	}

	name, err = domain.ParseName(name)
	if err != nil {
		return OrganizationCreated{}, err
	}

	org, err := s.store.CreateOrganization(ctx, domain.Organization{
		ID:   uuid.New(),
		Name: name,
	}, key, s.requestHash("create_organization", name))
	if err != nil {
		return OrganizationCreated{}, fmt.Errorf("create organization: %w", err)
	}

	token, err := s.tokens.Issue(org.ID, domain.PrincipalOrganization, []domain.Role{domain.RoleAdmin}, s.clock())
	if err != nil {
		return OrganizationCreated{}, fmt.Errorf("issue token: %w", err)
	}

	return OrganizationCreated{OrganizationID: org.ID, Token: token}, nil
}
