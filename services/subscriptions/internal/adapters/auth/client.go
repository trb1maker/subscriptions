package auth

import (
	"context"
	"fmt"
	"log/slog"
	"uuid"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authv1 "github.com/trb1maker/subscriptions/api/gen/auth/v1"
	"github.com/trb1maker/subscriptions/services/subscriptions/internal/domain"
)

// LookupAPI — вызов Auth, которым пользуется Subscriptions.
type LookupAPI interface {
	LookupSubject(ctx context.Context, in *authv1.LookupSubjectRequest, opts ...grpc.CallOption) (*authv1.LookupSubjectResponse, error)
}

// Client проверяет владельца подписки в Auth.
type Client struct {
	api LookupAPI
	log *slog.Logger
}

// NewClient собирает адаптер поверх сгенерированного клиента.
func NewClient(api LookupAPI, log *slog.Logger) *Client {
	return &Client{api: api, log: log}
}

// Lookup сообщает, что субъект существует, и для пользователя возвращает организацию.
func (c *Client) Lookup(ctx context.Context, id uuid.UUID, kind domain.OwnerKind) (domain.Subject, error) {
	resp, err := c.api.LookupSubject(ctx, &authv1.LookupSubjectRequest{
		SubjectId: id.String(),
		Type:      string(kind),
	})
	if err != nil {
		return domain.Subject{}, c.failure(ctx, err)
	}

	if resp.GetOrganizationId() == "" {
		return domain.Subject{}, nil
	}

	orgID, err := uuid.Parse(resp.GetOrganizationId())
	if err != nil {
		return domain.Subject{}, fmt.Errorf("parse organization id: %w", err)
	}

	return domain.Subject{OrganizationID: &orgID}, nil
}

func (c *Client) failure(ctx context.Context, err error) error {
	mapped := mapErr(err)
	if mapped == nil {
		c.log.ErrorContext(ctx, "auth lookup failed", "error", err)

		return fmt.Errorf("lookup subject: %w", err)
	}

	return mapped
}

func mapErr(err error) error {
	switch status.Code(err) {
	case codes.InvalidArgument:
		return domain.ErrInvalidArgument
	case codes.NotFound:
		return domain.ErrNotFound
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return domain.ErrUnavailable
	default:
		return nil
	}
}
