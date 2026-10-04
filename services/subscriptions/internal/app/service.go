package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"
	"uuid"

	"github.com/trb1maker/subscriptions/services/subscriptions/internal/domain"
)

// Store хранит тарифы и подписки и обеспечивает идемпотентность команд.
type Store interface {
	ReplayTariff(ctx context.Context, key string, requestHash []byte) (domain.Tariff, error)
	CreateTariff(ctx context.Context, tariff domain.Tariff, key string, requestHash []byte) (domain.Tariff, error)
	ListTariffs(ctx context.Context) ([]domain.Tariff, error)
	Tariff(ctx context.Context, id uuid.UUID) (domain.Tariff, error)

	ReplaySubscription(ctx context.Context, key string, requestHash []byte) (domain.Subscription, error)
	CreateSubscription(ctx context.Context, sub domain.Subscription, key string, requestHash []byte) (domain.Subscription, error)
	Subscription(ctx context.Context, id uuid.UUID) (domain.Subscription, error)
	ActiveByOwner(ctx context.Context, id uuid.UUID, kind domain.OwnerKind) (domain.Subscription, error)
	ChangeSubscription(ctx context.Context, id uuid.UUID, tariff domain.Tariff, key string, requestHash []byte) (domain.Subscription, error)
	ReplayPayment(ctx context.Context, key string, requestHash []byte) (domain.Subscription, error)
	RenewSubscription(ctx context.Context, id uuid.UUID, remaining, amountMinor int64, paymentID string, now time.Time, key string, requestHash []byte) (domain.Subscription, error)
	CloseExpired(ctx context.Context, now time.Time) (domain.ExpiryReport, error)
}

// Balances читает живой остаток владельца в Usage.
type Balances interface {
	Remaining(ctx context.Context, owner domain.Owner) (int64, error)
}

// PaymentNotice — уже посчитанный платёж для Usage.
type PaymentNotice struct {
	ID          uuid.UUID
	OccurredAt  time.Time
	Owner       domain.Owner
	Allowance   int64
	PaymentID   string
	AmountMinor int64
}

// Payments публикует PaymentReceived. Повтор того же идентификатора не меняет остаток второй раз.
type Payments interface {
	PublishPaymentReceived(ctx context.Context, event PaymentNotice) error
}

// Directory проверяет в Auth, что владелец существует.
type Directory interface {
	Lookup(ctx context.Context, id uuid.UUID, kind domain.OwnerKind) (domain.Subject, error)
}

// CheckResult — ответ проверки подписки. Пустая подписка при Active == false.
type CheckResult struct {
	Active       bool
	Subscription domain.Subscription
}

// Service — сценарии тарифов и подписок.
type Service struct {
	store     Store
	directory Directory
	balances  Balances
	payments  Payments
	pepper    []byte
	now       func() time.Time
}

// New собирает сценарии. pepper — секрет HMAC для отпечатка запроса, now может быть nil.
func New(store Store, directory Directory, balances Balances, payments Payments, pepper []byte, now func() time.Time) (*Service, error) {
	if len(pepper) == 0 {
		return nil, errors.New("empty request pepper")
	}

	if directory == nil {
		return nil, errors.New("missing directory")
	}

	if balances == nil {
		return nil, errors.New("missing balances")
	}

	if payments == nil {
		return nil, errors.New("missing payments")
	}

	if now == nil {
		now = time.Now
	}

	return &Service{
		store:     store,
		directory: directory,
		balances:  balances,
		payments:  payments,
		pepper:    append([]byte(nil), pepper...),
		now:       now,
	}, nil
}

func (s *Service) clock() time.Time {
	return s.now()
}

func (s *Service) requestHash(parts ...string) []byte {
	mac := hmac.New(sha256.New, s.pepper)
	for _, part := range parts {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(part)))
		_, _ = mac.Write(n[:])
		_, _ = mac.Write([]byte(part))
	}

	return mac.Sum(nil)
}

func requireOwner(owner domain.Owner) error {
	if owner.ID == uuid.Nil() || !owner.Kind.Valid() {
		return domain.ErrUnauthenticated
	}

	return nil
}
