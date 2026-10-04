package app

import (
	"crypto/hmac"
	"crypto/sha256"

	"github.com/trb1maker/subscriptions/services/gateway/internal/domain"
)

// AcceptPaymentWebhook сверяет предъявленный ключ с ожидаемым.
// Пустой ключ не принимается. Сравнение идёт по хешам, чтобы длина не отличалась по времени.
func AcceptPaymentWebhook(presented, expected string) error {
	if presented == "" || expected == "" {
		return domain.ErrInvalidWebhookKey
	}

	got := sha256.Sum256([]byte(presented))
	want := sha256.Sum256([]byte(expected))
	if !hmac.Equal(got[:], want[:]) {
		return domain.ErrInvalidWebhookKey
	}

	return nil
}
