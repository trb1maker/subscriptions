package auth

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authv1 "github.com/trb1maker/subscriptions/api/gen/auth/v1"
	"github.com/trb1maker/subscriptions/services/gateway/internal/app"
	"github.com/trb1maker/subscriptions/services/gateway/internal/domain"
)

// Client вызывает Auth по gRPC. Вызывающий должен уже лежать в context.
type Client struct {
	api authv1.AuthServiceClient
	log *slog.Logger
}

// NewClient собирает адаптер поверх сгенерированного клиента.
func NewClient(api authv1.AuthServiceClient, log *slog.Logger) *Client {
	return &Client{api: api, log: log}
}

// Register создаёт пользователя.
func (c *Client) Register(ctx context.Context, email, password, idempotencyKey, organizationID string) (app.Registered, error) {
	req := &authv1.RegisterRequest{
		IdempotencyKey: idempotencyKey,
		Email:          email,
		Password:       password,
	}
	if organizationID != "" {
		req.OrganizationId = &organizationID
	}

	resp, err := c.api.Register(ctx, req)
	if err != nil {
		return app.Registered{}, c.failure(ctx, err)
	}

	return app.Registered{UserID: resp.GetUserId(), Token: resp.GetToken()}, nil
}

// Login возвращает JWT.
func (c *Client) Login(ctx context.Context, email, password string) (string, error) {
	resp, err := c.api.Login(ctx, &authv1.LoginRequest{Email: email, Password: password})
	if err != nil {
		return "", c.failure(ctx, err)
	}

	return resp.GetToken(), nil
}

// CreateOrganization создаёт организацию. Metadata вызывающего добавляет interceptor соединения.
func (c *Client) CreateOrganization(ctx context.Context, name, idempotencyKey string) (app.OrganizationCreated, error) {
	resp, err := c.api.CreateOrganization(ctx, &authv1.CreateOrganizationRequest{
		IdempotencyKey: idempotencyKey,
		Name:           name,
	})
	if err != nil {
		return app.OrganizationCreated{}, c.failure(ctx, err)
	}

	return app.OrganizationCreated{OrganizationID: resp.GetOrganizationId(), Token: resp.GetToken()}, nil
}

// ValidateToken проверяет JWT и не требует уже известного вызывающего.
func (c *Client) ValidateToken(ctx context.Context, token string) (app.Identity, error) {
	if token == "" {
		return app.Identity{}, domain.ErrUnauthenticated
	}

	resp, err := c.api.ValidateToken(ctx, &authv1.ValidateTokenRequest{Token: token})
	if err != nil {
		return app.Identity{}, c.failure(ctx, err)
	}

	if resp.GetSubjectId() == "" {
		return app.Identity{}, domain.ErrUnauthenticated
	}

	return app.Identity{
		SubjectID: resp.GetSubjectId(),
		Kind:      resp.GetType(),
		Roles:     append([]string(nil), resp.GetRoles()...),
	}, nil
}

func (c *Client) failure(ctx context.Context, err error) error {
	mapped := mapErr(err)
	if errors.Is(mapped, domain.ErrInternal) || errors.Is(mapped, domain.ErrUnavailable) {
		c.log.ErrorContext(ctx, "auth request failed", "error", err)
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
	case codes.AlreadyExists, codes.FailedPrecondition:
		return domain.ErrConflict
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return domain.ErrUnavailable
	default:
		return domain.ErrInternal
	}
}
