package usage

import (
	"context"
	"errors"
	"log/slog"
	"uuid"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	usagev1 "github.com/trb1maker/subscriptions/api/gen/usage/v1"
	"github.com/trb1maker/subscriptions/services/gateway/internal/app"
	"github.com/trb1maker/subscriptions/services/gateway/internal/domain"
)

// Client вызывает Usage по gRPC. Вызывающий должен уже лежать в context.
type Client struct {
	api usagev1.UsageServiceClient
	log *slog.Logger
}

// NewClient собирает адаптер поверх сгенерированного клиента.
func NewClient(api usagev1.UsageServiceClient, log *slog.Logger) *Client {
	return &Client{api: api, log: log}
}

// CheckLimit сообщает, можно ли начать генерацию, и кто владеет остатком.
func (c *Client) CheckLimit(ctx context.Context) (app.LimitStatus, error) {
	resp, err := c.api.CheckLimit(ctx, &usagev1.CheckLimitRequest{})
	if err != nil {
		return app.LimitStatus{}, c.failure(ctx, err)
	}

	checked := app.LimitStatus{Allowed: resp.GetAllowed(), Remaining: resp.GetRemaining()}
	if resp.GetOwnerId() == "" && resp.GetOwnerKind() == "" {
		return checked, nil
	}

	ownerID, err := uuid.Parse(resp.GetOwnerId())
	if err != nil || ownerID == uuid.Nil() || resp.GetOwnerKind() == "" {
		c.log.ErrorContext(ctx, "usage response failed", "error", errors.New("invalid owner"))

		return app.LimitStatus{}, domain.ErrInternal
	}

	checked.OwnerID = ownerID
	checked.OwnerKind = resp.GetOwnerKind()

	return checked, nil
}

func (c *Client) failure(ctx context.Context, err error) error {
	mapped := mapErr(err)
	if errors.Is(mapped, domain.ErrInternal) || errors.Is(mapped, domain.ErrUnavailable) {
		c.log.ErrorContext(ctx, "usage request failed", "error", err)
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
