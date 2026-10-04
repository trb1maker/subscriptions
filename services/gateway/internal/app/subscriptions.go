package app

import (
	"context"
	"time"
)

// Tariff — тариф из Subscriptions.
type Tariff struct {
	ID                string
	Name              string
	MonthlyPriceMinor int64
	MessageLimit      int32
	Type              string
	IsBase            bool
}

// Subscription — подписка из Subscriptions.
type Subscription struct {
	ID               string
	TariffID         string
	Status           string
	MessageAllowance int64
	PeriodStart      time.Time
	PeriodEnd        time.Time
}

// SubscriptionStatus — действует ли подписка вызывающего.
type SubscriptionStatus struct {
	Active bool
}

// Subscriptions — внутренние вызовы Subscriptions.
type Subscriptions interface {
	CreateTariff(ctx context.Context, idempotencyKey, name string, price int64, limit int32, kind string, base bool) (Tariff, error)
	ListTariffs(ctx context.Context) ([]Tariff, error)
	CreateSubscription(ctx context.Context, idempotencyKey, tariffID string) (Subscription, error)
	ChangeSubscription(ctx context.Context, idempotencyKey, subscriptionID, tariffID string) (Subscription, error)
	GetSubscription(ctx context.Context, subscriptionID string) (Subscription, error)
	CheckSubscription(ctx context.Context) (SubscriptionStatus, error)
}
