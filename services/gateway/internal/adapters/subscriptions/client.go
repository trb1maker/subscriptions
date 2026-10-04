package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	subscriptionsv1 "github.com/trb1maker/subscriptions/api/gen/subscriptions/v1"
	"github.com/trb1maker/subscriptions/services/gateway/internal/app"
	"github.com/trb1maker/subscriptions/services/gateway/internal/domain"
)

// Client вызывает Subscriptions по gRPC. Вызывающий должен уже лежать в context.
type Client struct {
	api subscriptionsv1.SubscriptionServiceClient
	log *slog.Logger
}

// NewClient собирает адаптер поверх сгенерированного клиента.
func NewClient(api subscriptionsv1.SubscriptionServiceClient, log *slog.Logger) *Client {
	return &Client{api: api, log: log}
}

// CreateTariff создаёт тариф.
func (c *Client) CreateTariff(ctx context.Context, idempotencyKey, name string, price int64, limit int32, kind string, base bool) (app.Tariff, error) {
	resp, err := c.api.CreateTariff(ctx, &subscriptionsv1.CreateTariffRequest{
		IdempotencyKey:    idempotencyKey,
		Name:              name,
		MonthlyPriceMinor: price,
		MessageLimit:      limit,
		Type:              kind,
		IsBaseTariff:      base,
	})
	if err != nil {
		return app.Tariff{}, c.failure(ctx, err)
	}

	return tariffFrom(resp), nil
}

// ListTariffs возвращает каталог.
func (c *Client) ListTariffs(ctx context.Context) ([]app.Tariff, error) {
	resp, err := c.api.ListTariffs(ctx, &subscriptionsv1.ListTariffsRequest{})
	if err != nil {
		return nil, c.failure(ctx, err)
	}

	tariffs := make([]app.Tariff, 0, len(resp.GetTariffs()))
	for _, tariff := range resp.GetTariffs() {
		tariffs = append(tariffs, tariffFrom(tariff))
	}

	return tariffs, nil
}

// CreateSubscription создаёт подписку вызывающего.
func (c *Client) CreateSubscription(ctx context.Context, idempotencyKey, tariffID string) (app.Subscription, error) {
	resp, err := c.api.CreateSubscription(ctx, &subscriptionsv1.CreateSubscriptionRequest{
		IdempotencyKey: idempotencyKey,
		TariffId:       tariffID,
	})
	if err != nil {
		return app.Subscription{}, c.failure(ctx, err)
	}

	return c.subscription(ctx, resp)
}

// ChangeSubscription меняет тариф подписки.
func (c *Client) ChangeSubscription(ctx context.Context, idempotencyKey, subscriptionID, tariffID string) (app.Subscription, error) {
	resp, err := c.api.ChangeSubscription(ctx, &subscriptionsv1.ChangeSubscriptionRequest{
		IdempotencyKey: idempotencyKey,
		SubscriptionId: subscriptionID,
		TariffId:       tariffID,
	})
	if err != nil {
		return app.Subscription{}, c.failure(ctx, err)
	}

	return c.subscription(ctx, resp)
}

// GetSubscription возвращает подписку.
func (c *Client) GetSubscription(ctx context.Context, subscriptionID string) (app.Subscription, error) {
	resp, err := c.api.GetSubscription(ctx, &subscriptionsv1.GetSubscriptionRequest{SubscriptionId: subscriptionID})
	if err != nil {
		return app.Subscription{}, c.failure(ctx, err)
	}

	return c.subscription(ctx, resp)
}

func tariffFrom(tariff *subscriptionsv1.Tariff) app.Tariff {
	if tariff == nil {
		return app.Tariff{}
	}

	return app.Tariff{
		ID:                tariff.GetId(),
		Name:              tariff.GetName(),
		MonthlyPriceMinor: tariff.GetMonthlyPriceMinor(),
		MessageLimit:      tariff.GetMessageLimit(),
		Type:              tariff.GetType(),
		IsBase:            tariff.GetIsBaseTariff(),
	}
}

func subscriptionFrom(sub *subscriptionsv1.Subscription) (app.Subscription, error) {
	if sub == nil {
		return app.Subscription{}, errors.New("empty subscription")
	}

	start, err := time.Parse(time.RFC3339, sub.GetCurrentPeriodStart())
	if err != nil {
		return app.Subscription{}, fmt.Errorf("parse period start: %w", err)
	}

	end, err := time.Parse(time.RFC3339, sub.GetCurrentPeriodEnd())
	if err != nil {
		return app.Subscription{}, fmt.Errorf("parse period end: %w", err)
	}

	return app.Subscription{
		ID:               sub.GetId(),
		TariffID:         sub.GetTariffId(),
		Status:           sub.GetStatus(),
		MessageAllowance: sub.GetMessageAllowance(),
		PeriodStart:      start,
		PeriodEnd:        end,
	}, nil
}

func (c *Client) subscription(ctx context.Context, sub *subscriptionsv1.Subscription) (app.Subscription, error) {
	parsed, err := subscriptionFrom(sub)
	if err != nil {
		c.log.ErrorContext(ctx, "subscriptions response failed", "error", err)

		return app.Subscription{}, domain.ErrInternal
	}

	return parsed, nil
}

func (c *Client) failure(ctx context.Context, err error) error {
	mapped := mapErr(err)
	if errors.Is(mapped, domain.ErrInternal) || errors.Is(mapped, domain.ErrUnavailable) {
		c.log.ErrorContext(ctx, "subscriptions request failed", "error", err)
	}

	return mapped
}

func mapErr(err error) error {
	switch status.Code(err) {
	case codes.InvalidArgument:
		return domain.ErrInvalidArgument
	case codes.Unauthenticated:
		return domain.ErrUnauthenticated
	case codes.PermissionDenied:
		return domain.ErrForbidden
	case codes.NotFound:
		return domain.ErrNotFound
	case codes.AlreadyExists, codes.FailedPrecondition:
		return domain.ErrConflict
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return domain.ErrUnavailable
	default:
		return domain.ErrInternal
	}
}
