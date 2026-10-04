package domain

import (
	"bytes"
	"time"
	"uuid"
)

// EventType — вид события, которое меняет проекцию или только журнал.
type EventType string

const (
	EventMessageProcessed        EventType = "message_processed"
	EventMessageFailed           EventType = "message_failed"
	EventPaymentReceived         EventType = "payment_received"
	EventSubscriptionChanged     EventType = "subscription_changed"
	EventSubscriptionPeriodEnded EventType = "subscription_period_ended"
)

// Valid сообщает, что тип события известен домену.
func (t EventType) Valid() bool {
	switch t {
	case EventMessageProcessed, EventMessageFailed, EventPaymentReceived, EventSubscriptionChanged, EventSubscriptionPeriodEnded:
		return true
	default:
		return false
	}
}

// Outcome — значение аналитики токенов. Второе значение ложно, если событие токены не пишет.
func (t EventType) Outcome() (string, bool) {
	switch t {
	case EventMessageProcessed:
		return "processed", true
	case EventMessageFailed:
		return "failed", true
	default:
		return "", false
	}
}

// Event — одно событие учёта. Allowance заполнен только у событий, которые ставят остаток.
type Event struct {
	ID           uuid.UUID
	Type         EventType
	OccurredAt   time.Time
	Owner        Owner
	Allowance    int64
	HasAllowance bool
	Tokens       int64
	PaymentID    string
	AmountMinor  int64
}

// Normalized приводит момент к UTC с точностью ClickHouse.
func (e Event) Normalized() Event {
	e.OccurredAt = e.OccurredAt.UTC().Truncate(time.Millisecond)

	return e
}

// Validate проверяет инварианты события.
func (e Event) Validate() error {
	if e.ID == uuid.Nil() || e.OccurredAt.IsZero() || e.Owner.ID == uuid.Nil() || !e.Owner.Kind.Valid() || !e.Type.Valid() {
		return ErrInvalidArgument
	}

	switch e.Type {
	case EventMessageProcessed, EventMessageFailed:
		if e.Tokens < 0 || e.HasAllowance || e.PaymentID != "" || e.AmountMinor != 0 {
			return ErrInvalidArgument
		}
	case EventPaymentReceived:
		if !e.HasAllowance || e.PaymentID == "" || e.AmountMinor < 0 || e.Tokens != 0 {
			return ErrInvalidArgument
		}
	case EventSubscriptionChanged, EventSubscriptionPeriodEnded:
		if !e.HasAllowance || e.PaymentID != "" || e.AmountMinor != 0 || e.Tokens != 0 {
			return ErrInvalidArgument
		}
	default:
		return ErrInvalidArgument
	}

	return nil
}

// SetsAllowance сообщает, что событие заменяет остаток целиком.
func (e Event) SetsAllowance() bool {
	switch e.Type {
	case EventPaymentReceived, EventSubscriptionChanged, EventSubscriptionPeriodEnded:
		return true
	default:
		return false
	}
}

// Decrements сообщает, что событие списывает одно сообщение.
func (e Event) Decrements() bool {
	return e.Type == EventMessageProcessed
}

// Same сообщает, что повтор несёт то же тело.
func (e Event) Same(other Event) bool {
	left := e.Normalized()
	right := other.Normalized()

	return left.ID == right.ID &&
		left.Type == right.Type &&
		left.OccurredAt.Equal(right.OccurredAt) &&
		left.Owner == right.Owner &&
		left.HasAllowance == right.HasAllowance &&
		left.Allowance == right.Allowance &&
		left.Tokens == right.Tokens &&
		left.PaymentID == right.PaymentID &&
		left.AmountMinor == right.AmountMinor
}

// Later сообщает, что событие случилось после other.
// При равном времени больше тот идентификатор, который старше по байтам.
func (e Event) Later(other Event) bool {
	left := e.Normalized()
	right := other.Normalized()
	if left.OccurredAt.After(right.OccurredAt) {
		return true
	}

	if !left.OccurredAt.Equal(right.OccurredAt) {
		return false
	}

	return bytes.Compare(left.ID[:], right.ID[:]) > 0
}
