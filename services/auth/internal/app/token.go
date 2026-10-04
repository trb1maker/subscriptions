package app

import (
	"fmt"

	"github.com/trb1maker/subscriptions/services/auth/internal/domain"
)

// ValidateToken проверяет подпись и срок JWT.
func (s *Service) ValidateToken(token string) (Identity, error) {
	if token == "" {
		return Identity{}, domain.ErrInvalidToken
	}

	identity, err := s.tokens.Parse(token, s.clock())
	if err != nil {
		return Identity{}, fmt.Errorf("parse token: %w", err)
	}

	return identity, nil
}
