package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"uuid"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	usagev1 "github.com/trb1maker/subscriptions/api/gen/usage/v1"
	"github.com/trb1maker/subscriptions/pkg/caller"
	"github.com/trb1maker/subscriptions/services/usage/internal/app"
	"github.com/trb1maker/subscriptions/services/usage/internal/domain"
)

// Server переводит доменные ошибки в коды gRPC.
type Server struct {
	usagev1.UnimplementedUsageServiceServer

	svc *app.Service
	log *slog.Logger
}

// NewServer собирает gRPC-адаптер.
func NewServer(svc *app.Service, log *slog.Logger) *Server {
	return &Server{svc: svc, log: log}
}

// CheckLimit сообщает, можно ли начать генерацию.
func (s *Server) CheckLimit(ctx context.Context, _ *usagev1.CheckLimitRequest) (*usagev1.CheckLimitResponse, error) {
	owner, err := ownerFromContext(ctx)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	checked, err := s.svc.CheckLimit(ctx, owner)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	resp := &usagev1.CheckLimitResponse{
		Allowed:   checked.Allowed,
		Remaining: checked.Remaining,
	}
	if checked.Owner.ID != uuid.Nil() {
		resp.OwnerKind = string(checked.Owner.Kind)
		resp.OwnerId = checked.Owner.ID.String()
	}

	return resp, nil
}

func ownerFromContext(ctx context.Context) (domain.Owner, error) {
	current, ok := caller.FromContext(ctx)
	if !ok {
		return domain.Owner{}, domain.ErrUnauthenticated
	}

	id, err := uuid.Parse(current.SubjectID)
	if err != nil {
		return domain.Owner{}, domain.ErrUnauthenticated
	}

	kind := domain.OwnerKind(current.Kind)
	if !kind.Valid() {
		return domain.Owner{}, domain.ErrUnauthenticated
	}

	return domain.Owner{ID: id, Kind: kind}, nil
}

func rpcError(ctx context.Context, log *slog.Logger, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidArgument):
		return fmt.Errorf("invalid argument: %w", status.Error(codes.InvalidArgument, "invalid argument"))
	case errors.Is(err, domain.ErrUnauthenticated):
		return fmt.Errorf("unauthenticated: %w", status.Error(codes.Unauthenticated, "unauthenticated"))
	case errors.Is(err, domain.ErrNotFound):
		return fmt.Errorf("not found: %w", status.Error(codes.NotFound, "not found"))
	case errors.Is(err, domain.ErrIdempotencyConflict):
		return fmt.Errorf("conflict: %w", status.Error(codes.FailedPrecondition, "conflict"))
	case errors.Is(err, domain.ErrUnavailable):
		return fmt.Errorf("unavailable: %w", status.Error(codes.Unavailable, "unavailable"))
	default:
		log.ErrorContext(ctx, "request failed", "error", err)

		return fmt.Errorf("internal: %w", status.Error(codes.Internal, "internal"))
	}
}
