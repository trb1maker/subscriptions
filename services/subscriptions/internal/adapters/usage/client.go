package usage

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	usagev1 "github.com/trb1maker/subscriptions/api/gen/usage/v1"
	"github.com/trb1maker/subscriptions/pkg/caller"
	"github.com/trb1maker/subscriptions/services/subscriptions/internal/domain"
)

// API — вызов Usage, которым пользуется Subscriptions.
type API interface {
	CheckLimit(ctx context.Context, in *usagev1.CheckLimitRequest, opts ...grpc.CallOption) (*usagev1.CheckLimitResponse, error)
}

// Client читает живой остаток владельца.
type Client struct {
	api API
	log *slog.Logger
}

// NewClient собирает адаптер поверх сгенерированного клиента.
func NewClient(api API, log *slog.Logger) *Client {
	return &Client{api: api, log: log}
}

// Remaining возвращает текущий остаток. Нет проекции — ноль.
func (c *Client) Remaining(ctx context.Context, owner domain.Owner) (int64, error) {
	ctx = caller.NewContext(ctx, caller.Caller{
		SubjectID: owner.ID.String(),
		Kind:      string(owner.Kind),
	})

	resp, err := c.api.CheckLimit(ctx, &usagev1.CheckLimitRequest{})
	if err != nil {
		return 0, c.failure(ctx, err)
	}

	return resp.GetRemaining(), nil
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
	case codes.NotFound:
		return domain.ErrNotFound
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return domain.ErrUnavailable
	default:
		return domain.ErrInternal
	}
}
