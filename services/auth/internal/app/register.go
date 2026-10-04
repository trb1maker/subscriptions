package app

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/trb1maker/subscriptions/services/auth/internal/domain"
)

// Register создаёт пользователя и JWT. Повтор с тем же ключом и тем же телом возвращает того же пользователя и новый токен.
func (s *Service) Register(ctx context.Context, email, password, idempotencyKey string, organizationID *uuid.UUID) (Registered, error) {
	key, err := domain.ParseIdempotencyKey(idempotencyKey)
	if err != nil {
		return Registered{}, err
	}

	email, err = domain.ParseEmail(email)
	if err != nil {
		return Registered{}, err
	}

	if err := domain.ValidatePassword(password); err != nil {
		return Registered{}, err
	}

	org := ""
	if organizationID != nil {
		org = organizationID.String()
	}

	digest := s.requestHash("register", email, password, org)
	user, err := s.store.ReplayUser(ctx, key, digest)
	if errors.Is(err, domain.ErrNotFound) {
		user, err = s.registerNewUser(ctx, email, password, key, digest, organizationID)
	}

	if err != nil {
		return Registered{}, fmt.Errorf("register user: %w", err)
	}

	token, err := s.tokens.Issue(user.ID, domain.PrincipalUser, []domain.Role{user.Role}, s.clock())
	if err != nil {
		return Registered{}, fmt.Errorf("issue token: %w", err)
	}

	return Registered{UserID: user.ID, Token: token}, nil
}

func (s *Service) registerNewUser(ctx context.Context, email, password, key string, digest []byte, organizationID *uuid.UUID) (domain.User, error) {
	hash, err := s.passwords.Hash(password)
	if err != nil {
		return domain.User{}, fmt.Errorf("hash password: %w", err)
	}

	user, err := s.store.RegisterUser(ctx, domain.User{
		ID:             uuid.New(),
		Email:          email,
		PasswordHash:   hash,
		OrganizationID: organizationID,
		Role:           domain.RoleUser,
	}, key, digest)
	if err != nil {
		return domain.User{}, fmt.Errorf("store user: %w", err)
	}

	return user, nil
}
