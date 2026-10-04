package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	subscriptionsv1 "github.com/trb1maker/subscriptions/api/gen/subscriptions/v1"
	"github.com/trb1maker/subscriptions/pkg/caller"
	"github.com/trb1maker/subscriptions/services/subscriptions/internal/app"
	"github.com/trb1maker/subscriptions/services/subscriptions/internal/domain"
)

// Server переводит доменные ошибки в коды gRPC.
type Server struct {
	subscriptionsv1.UnimplementedSubscriptionServiceServer

	svc *app.Service
	log *slog.Logger
}

// NewServer собирает gRPC-адаптер.
func NewServer(svc *app.Service, log *slog.Logger) *Server {
	return &Server{svc: svc, log: log}
}

// CreateTariff создаёт тариф.
func (s *Server) CreateTariff(ctx context.Context, req *subscriptionsv1.CreateTariffRequest) (*subscriptionsv1.Tariff, error) {
	owner, err := ownerFromContext(ctx)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	tariff, err := s.svc.CreateTariff(
		ctx,
		owner,
		req.GetName(),
		req.GetMonthlyPriceMinor(),
		int(req.GetMessageLimit()),
		req.GetType(),
		req.GetIsBaseTariff(),
		req.GetIdempotencyKey(),
	)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	return tariffMessage(tariff), nil
}

// ListTariffs возвращает каталог.
func (s *Server) ListTariffs(ctx context.Context, _ *subscriptionsv1.ListTariffsRequest) (*subscriptionsv1.ListTariffsResponse, error) {
	owner, err := ownerFromContext(ctx)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	tariffs, err := s.svc.ListTariffs(ctx, owner)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	out := make([]*subscriptionsv1.Tariff, 0, len(tariffs))
	for _, tariff := range tariffs {
		out = append(out, tariffMessage(tariff))
	}

	return &subscriptionsv1.ListTariffsResponse{Tariffs: out}, nil
}

// CreateSubscription создаёт подписку вызывающего.
func (s *Server) CreateSubscription(ctx context.Context, req *subscriptionsv1.CreateSubscriptionRequest) (*subscriptionsv1.Subscription, error) {
	owner, err := ownerFromContext(ctx)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	tariffID, err := parseID(req.GetTariffId(), domain.ErrInvalidTariffID)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	sub, err := s.svc.CreateSubscription(ctx, owner, tariffID, req.GetIdempotencyKey())
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	return subscriptionMessage(sub), nil
}

// ChangeSubscription меняет тариф подписки.
func (s *Server) ChangeSubscription(ctx context.Context, req *subscriptionsv1.ChangeSubscriptionRequest) (*subscriptionsv1.Subscription, error) {
	owner, err := ownerFromContext(ctx)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	subscriptionID, err := parseID(req.GetSubscriptionId(), domain.ErrInvalidSubscriptionID)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	tariffID, err := parseID(req.GetTariffId(), domain.ErrInvalidTariffID)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	sub, err := s.svc.ChangeSubscription(ctx, owner, subscriptionID, tariffID, req.GetIdempotencyKey())
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	return subscriptionMessage(sub), nil
}

// GetSubscription возвращает подписку владельца.
func (s *Server) GetSubscription(ctx context.Context, req *subscriptionsv1.GetSubscriptionRequest) (*subscriptionsv1.Subscription, error) {
	owner, err := ownerFromContext(ctx)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	subscriptionID, err := parseID(req.GetSubscriptionId(), domain.ErrInvalidSubscriptionID)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	sub, err := s.svc.GetSubscription(ctx, owner, subscriptionID)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	return subscriptionMessage(sub), nil
}

// CheckSubscription сообщает, действует ли подписка вызывающего.
func (s *Server) CheckSubscription(ctx context.Context, _ *subscriptionsv1.CheckSubscriptionRequest) (*subscriptionsv1.CheckSubscriptionResponse, error) {
	owner, err := ownerFromContext(ctx)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	checked, err := s.svc.CheckSubscription(ctx, owner)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	resp := &subscriptionsv1.CheckSubscriptionResponse{Active: checked.Active}
	if !checked.Active {
		return resp, nil
	}

	resp.SubscriptionId = checked.Subscription.ID.String()
	resp.TariffId = checked.Subscription.TariffID.String()
	resp.MessageLimit = checked.Subscription.MessageAllowance
	resp.CurrentPeriodEnd = formatTime(checked.Subscription.PeriodEnd)

	return resp, nil
}

func tariffMessage(tariff domain.Tariff) *subscriptionsv1.Tariff {
	return &subscriptionsv1.Tariff{
		Id:                tariff.ID.String(),
		Name:              tariff.Name,
		MonthlyPriceMinor: tariff.MonthlyPriceMinor,
		MessageLimit:      int32(tariff.MessageLimit),
		Type:              string(tariff.Type),
		IsBaseTariff:      tariff.IsBase,
	}
}

func subscriptionMessage(sub domain.Subscription) *subscriptionsv1.Subscription {
	return &subscriptionsv1.Subscription{
		Id:                 sub.ID.String(),
		TariffId:           sub.TariffID.String(),
		Status:             string(sub.Status),
		MessageAllowance:   sub.MessageAllowance,
		CurrentPeriodStart: formatTime(sub.PeriodStart),
		CurrentPeriodEnd:   formatTime(sub.PeriodEnd),
	}
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339)
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

	return domain.Owner{ID: id, Kind: kind, Roles: append([]string(nil), current.Roles...)}, nil
}

func parseID(raw string, invalid error) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil(), invalid
	}

	return id, nil
}

func rpcError(ctx context.Context, log *slog.Logger, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidName),
		errors.Is(err, domain.ErrInvalidPrice),
		errors.Is(err, domain.ErrInvalidMessageLimit),
		errors.Is(err, domain.ErrInvalidTariffType),
		errors.Is(err, domain.ErrInvalidTariffID),
		errors.Is(err, domain.ErrInvalidSubscriptionID),
		errors.Is(err, domain.ErrInvalidIdempotencyKey),
		errors.Is(err, domain.ErrInvalidArgument),
		errors.Is(err, domain.ErrTariffType):
		return fmt.Errorf("invalid argument: %w", status.Error(codes.InvalidArgument, "invalid argument"))
	case errors.Is(err, domain.ErrUnauthenticated):
		return fmt.Errorf("unauthenticated: %w", status.Error(codes.Unauthenticated, "unauthenticated"))
	case errors.Is(err, domain.ErrForbidden):
		return fmt.Errorf("forbidden: %w", status.Error(codes.PermissionDenied, "forbidden"))
	case errors.Is(err, domain.ErrNotFound):
		return fmt.Errorf("not found: %w", status.Error(codes.NotFound, "not found"))
	case errors.Is(err, domain.ErrOrganizationMember),
		errors.Is(err, domain.ErrActiveSubscription),
		errors.Is(err, domain.ErrBaseTariffExists),
		errors.Is(err, domain.ErrIdempotencyConflict):
		return fmt.Errorf("conflict: %w", status.Error(codes.FailedPrecondition, "conflict"))
	case errors.Is(err, domain.ErrUnavailable):
		return fmt.Errorf("unavailable: %w", status.Error(codes.Unavailable, "unavailable"))
	default:
		log.ErrorContext(ctx, "request failed", "error", err)

		return fmt.Errorf("internal: %w", status.Error(codes.Internal, "internal"))
	}
}
