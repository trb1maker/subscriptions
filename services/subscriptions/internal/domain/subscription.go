package domain

import (
	"time"
	"uuid"
)

// Status — состояние подписки.
type Status string

const (
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
	StatusExpired   Status = "expired"
)

// Valid сообщает, что статус известен домену.
func (s Status) Valid() bool {
	return s == StatusActive || s == StatusSuspended || s == StatusExpired
}

// Subscription — доступ владельца к тарифу на текущий период.
type Subscription struct {
	ID               uuid.UUID
	UserID           *uuid.UUID
	OrganizationID   *uuid.UUID
	TariffID         uuid.UUID
	Status           Status
	MessageAllowance int64
	PeriodStart      time.Time
	PeriodEnd        time.Time
	PaymentID        string
}

// OwnedBy сообщает, что подписка принадлежит вызывающему.
func (s Subscription) OwnedBy(owner Owner) bool {
	switch owner.Kind {
	case OwnerUser:
		return s.UserID != nil && *s.UserID == owner.ID
	case OwnerOrganization:
		return s.OrganizationID != nil && *s.OrganizationID == owner.ID
	default:
		return false
	}
}

// Owner возвращает владельца подписки.
func (s Subscription) Owner() (Owner, bool) {
	switch {
	case s.UserID != nil && s.OrganizationID == nil:
		return Owner{ID: *s.UserID, Kind: OwnerUser}, true
	case s.OrganizationID != nil && s.UserID == nil:
		return Owner{ID: *s.OrganizationID, Kind: OwnerOrganization}, true
	default:
		return Owner{}, false
	}
}

// ActiveAt сообщает, что подписка активна и момент попадает в период.
func (s Subscription) ActiveAt(now time.Time) bool {
	return s.Status == StatusActive && !now.Before(s.PeriodStart) && now.Before(s.PeriodEnd)
}

// NextPeriodEnd сдвигает начало периода на календарный месяц.
func NextPeriodEnd(start time.Time) time.Time {
	return start.AddDate(0, 1, 0)
}

// ExpiryReport — сколько подписок cron закрыл и какие оставил без базового тарифа.
// Applied — строки после применения правила. По ним публикуется окончание периода.
type ExpiryReport struct {
	Closed  int
	Skipped []uuid.UUID
	Applied []Subscription
}
