package password

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/trb1maker/subscriptions/services/auth/internal/domain"
)

// blindHash — bcrypt стоимости DefaultCost. С ним отсутствующий пользователь отвечает не быстрее существующего.
const blindHash = "$2a$10$O3ox6PpN6SwsSDzbWLZBPOLW9.B7sKQ3Osc8VwN6p0jgkDA4DtCDC"

// Hasher считает bcrypt-хеш пароля.
type Hasher struct{}

// Hash возвращает хеш пароля.
func (Hasher) Hash(plain string) (string, error) {
	sum, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}

	return string(sum), nil
}

// Match сверяет пароль с хешем.
func (Hasher) Match(hash, plain string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return domain.ErrInvalidCredentials
		}

		return fmt.Errorf("compare password: %w", err)
	}

	return nil
}

// Burn сравнивает пароль с фиксированным хешем и всегда сообщает о неверных учётных данных.
func (Hasher) Burn(plain string) error {
	_ = bcrypt.CompareHashAndPassword([]byte(blindHash), []byte(plain))

	return domain.ErrInvalidCredentials
}
