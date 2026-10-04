package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/trb1maker/subscriptions/services/auth/internal/domain"
)

// Login возвращает JWT пользователя. Неверный адрес и неверный пароль выглядят одинаково.
func (s *Service) Login(ctx context.Context, email, password string) (string, error) {
	email, err := domain.ParseEmail(email)
	if err != nil {
		return "", domain.ErrInvalidCredentials
	}

	if err := domain.ValidatePassword(password); err != nil {
		return "", domain.ErrInvalidCredentials
	}

	user, err := s.store.UserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", fmt.Errorf("burn password: %w", s.passwords.Burn(password))
		}

		return "", fmt.Errorf("find user: %w", err)
	}

	if err := s.passwords.Match(user.PasswordHash, password); err != nil {
		return "", fmt.Errorf("match password: %w", err)
	}

	token, err := s.tokens.Issue(user.ID, domain.PrincipalUser, []domain.Role{user.Role}, s.clock())
	if err != nil {
		return "", fmt.Errorf("issue token: %w", err)
	}

	return token, nil
}
