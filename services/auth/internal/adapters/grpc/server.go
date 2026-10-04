package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"uuid"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authv1 "github.com/trb1maker/subscriptions/api/gen/auth/v1"
	"github.com/trb1maker/subscriptions/services/auth/internal/app"
	"github.com/trb1maker/subscriptions/services/auth/internal/domain"
)

// Server переводит доменные ошибки в коды gRPC.
type Server struct {
	authv1.UnimplementedAuthServiceServer

	svc *app.Service
	log *slog.Logger
}

// NewServer собирает gRPC-адаптер.
func NewServer(svc *app.Service, log *slog.Logger) *Server {
	return &Server{svc: svc, log: log}
}

// Register создаёт пользователя и возвращает JWT.
func (s *Server) Register(ctx context.Context, req *authv1.RegisterRequest) (*authv1.RegisterResponse, error) {
	organizationID, err := organizationID(req.GetOrganizationId())
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	created, err := s.svc.Register(ctx, req.GetEmail(), req.GetPassword(), req.GetIdempotencyKey(), organizationID)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	return &authv1.RegisterResponse{UserId: created.UserID.String(), Token: created.Token}, nil
}

// Login возвращает JWT пользователя.
func (s *Server) Login(ctx context.Context, req *authv1.LoginRequest) (*authv1.LoginResponse, error) {
	token, err := s.svc.Login(ctx, req.GetEmail(), req.GetPassword())
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	return &authv1.LoginResponse{Token: token}, nil
}

// CreateOrganization создаёт организацию и возвращает её JWT.
func (s *Server) CreateOrganization(ctx context.Context, req *authv1.CreateOrganizationRequest) (*authv1.CreateOrganizationResponse, error) {
	created, err := s.svc.CreateOrganization(ctx, req.GetName(), req.GetIdempotencyKey())
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	return &authv1.CreateOrganizationResponse{
		OrganizationId: created.OrganizationID.String(),
		Token:          created.Token,
	}, nil
}

// ValidateToken проверяет JWT.
func (s *Server) ValidateToken(ctx context.Context, req *authv1.ValidateTokenRequest) (*authv1.ValidateTokenResponse, error) {
	identity, err := s.svc.ValidateToken(req.GetToken())
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	roles := make([]string, 0, len(identity.Roles))
	for _, role := range identity.Roles {
		roles = append(roles, string(role))
	}

	return &authv1.ValidateTokenResponse{
		SubjectId: identity.SubjectID.String(),
		Type:      string(identity.Type),
		Roles:     roles,
	}, nil
}

// LookupSubject проверяет, что пользователь или организация существуют.
func (s *Server) LookupSubject(ctx context.Context, req *authv1.LookupSubjectRequest) (*authv1.LookupSubjectResponse, error) {
	id, err := uuid.Parse(req.GetSubjectId())
	if err != nil {
		return nil, rpcError(ctx, s.log, domain.ErrInvalidSubject)
	}

	kind := domain.PrincipalType(req.GetType())
	if !kind.Valid() {
		return nil, rpcError(ctx, s.log, domain.ErrInvalidSubject)
	}

	organizationID, err := s.svc.LookupSubject(ctx, id, kind)
	if err != nil {
		return nil, rpcError(ctx, s.log, err)
	}

	resp := &authv1.LookupSubjectResponse{}
	if organizationID != nil {
		value := organizationID.String()
		resp.OrganizationId = &value
	}

	return resp, nil
}

func organizationID(raw string) (*uuid.UUID, error) {
	if raw == "" {
		return nil, nil
	}

	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, domain.ErrInvalidOrganizationID
	}

	return &id, nil
}

func rpcError(ctx context.Context, log *slog.Logger, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidEmail),
		errors.Is(err, domain.ErrInvalidPassword),
		errors.Is(err, domain.ErrInvalidName),
		errors.Is(err, domain.ErrInvalidIdempotencyKey),
		errors.Is(err, domain.ErrInvalidOrganizationID):
		return fmt.Errorf("invalid argument: %w", status.Error(codes.InvalidArgument, "invalid argument"))
	case errors.Is(err, domain.ErrEmailTaken):
		return fmt.Errorf("email taken: %w", status.Error(codes.AlreadyExists, "email taken"))
	case errors.Is(err, domain.ErrOrganizationNotFound):
		return fmt.Errorf("organization not found: %w", status.Error(codes.NotFound, "organization not found"))
	case errors.Is(err, domain.ErrIdempotencyConflict):
		return fmt.Errorf("idempotency conflict: %w", status.Error(codes.FailedPrecondition, "idempotency conflict"))
	case errors.Is(err, domain.ErrInvalidCredentials),
		errors.Is(err, domain.ErrInvalidToken):
		return fmt.Errorf("unauthenticated: %w", status.Error(codes.Unauthenticated, "unauthenticated"))
	case errors.Is(err, domain.ErrNotFound):
		return fmt.Errorf("not found: %w", status.Error(codes.NotFound, "not found"))
	case errors.Is(err, domain.ErrInvalidSubject):
		return fmt.Errorf("invalid argument: %w", status.Error(codes.InvalidArgument, "invalid argument"))
	default:
		log.ErrorContext(ctx, "request failed", "error", err)

		return fmt.Errorf("internal: %w", status.Error(codes.Internal, "internal"))
	}
}
