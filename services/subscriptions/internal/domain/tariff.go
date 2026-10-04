package domain

import "uuid"

// TariffType — аудитория тарифа.
type TariffType string

const (
	TariffB2C TariffType = "b2c"
	TariffB2B TariffType = "b2b"
)

// Valid сообщает, что тип тарифа известен домену.
func (t TariffType) Valid() bool {
	return t == TariffB2C || t == TariffB2B
}

// Tariff — условия подписки. Цена хранится в минимальных единицах валюты.
type Tariff struct {
	ID                uuid.UUID
	Name              string
	MonthlyPriceMinor int64
	MessageLimit      int
	Type              TariffType
	IsBase            bool
}

// Matches сообщает, что тариф подходит владельцу.
func (t Tariff) Matches(kind OwnerKind) bool {
	switch kind {
	case OwnerUser:
		return t.Type == TariffB2C
	case OwnerOrganization:
		return t.Type == TariffB2B
	default:
		return false
	}
}
