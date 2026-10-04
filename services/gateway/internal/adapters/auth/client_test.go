package auth_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	authv1 "github.com/trb1maker/subscriptions/api/gen/auth/v1"
	"github.com/trb1maker/subscriptions/pkg/caller"
	"github.com/trb1maker/subscriptions/pkg/grpcclient"
	"github.com/trb1maker/subscriptions/pkg/grpcserver"
	"github.com/trb1maker/subscriptions/pkg/logger"
	authadapter "github.com/trb1maker/subscriptions/services/gateway/internal/adapters/auth"
	"github.com/trb1maker/subscriptions/services/gateway/internal/domain"
)

func TestMapsGRPCStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code   codes.Code
		target error
	}{
		{code: codes.InvalidArgument, target: domain.ErrInvalidArgument},
		{code: codes.Unauthenticated, target: domain.ErrUnauthenticated},
		{code: codes.NotFound, target: domain.ErrNotFound},
		{code: codes.AlreadyExists, target: domain.ErrConflict},
		{code: codes.FailedPrecondition, target: domain.ErrConflict},
		{code: codes.Unavailable, target: domain.ErrUnavailable},
		{code: codes.DeadlineExceeded, target: domain.ErrUnavailable},
		{code: codes.Canceled, target: domain.ErrUnavailable},
		{code: codes.Internal, target: domain.ErrInternal},
		{code: codes.PermissionDenied, target: domain.ErrInternal},
	}

	for _, tt := range tests {
		t.Run(tt.code.String(), func(t *testing.T) {
			t.Parallel()

			client := authadapter.NewClient(&stubAPI{err: status.Error(tt.code, "boom")}, discardLog(t))
			_, err := client.Login(context.Background(), "a@b.c", "secret")
			require.ErrorIs(t, err, tt.target)
		})
	}
}

func TestValidateTokenRejectsEmptySubject(t *testing.T) {
	t.Parallel()

	client := authadapter.NewClient(&stubAPI{validate: &authv1.ValidateTokenResponse{}}, discardLog(t))
	_, err := client.ValidateToken(context.Background(), "token")
	require.ErrorIs(t, err, domain.ErrUnauthenticated)
}

func TestValidateTokenRequiresToken(t *testing.T) {
	t.Parallel()

	client := authadapter.NewClient(&stubAPI{}, discardLog(t))
	_, err := client.ValidateToken(context.Background(), "")
	require.ErrorIs(t, err, domain.ErrUnauthenticated)
}

func TestForwardsCallerOnlyWhenPresent(t *testing.T) {
	t.Parallel()

	const bufferSize = 1024 * 1024

	log := discardLog(t)
	lis := bufconn.Listen(bufferSize)
	server := grpcserver.New(log)
	capture := &captureAPI{}
	authv1.RegisterAuthServiceServer(server, capture)
	go func() {
		_ = server.Serve(lis)
	}()
	t.Cleanup(func() {
		server.Stop()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(grpcclient.UnaryClientInterceptor()),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, conn.Close())
	})

	client := authadapter.NewClient(authv1.NewAuthServiceClient(conn), log)
	ctx := logger.WithRequestID(context.Background(), "req-1")

	_, err = client.Register(ctx, "a@b.c", "secret", "key-1", "org-1")
	require.NoError(t, err)
	_, err = client.ValidateToken(ctx, "token")
	require.NoError(t, err)

	orgCtx := caller.NewContext(ctx, caller.Caller{SubjectID: "user-1", Kind: "user", Roles: []string{"admin"}})
	_, err = client.CreateOrganization(orgCtx, "acme", "org-1")
	require.NoError(t, err)

	registerMD := capture.md(authv1.AuthService_Register_FullMethodName)
	require.Equal(t, []string{"req-1"}, registerMD.Get(caller.MetadataRequestID))
	require.Empty(t, registerMD.Get(caller.MetadataSubject))
	require.Equal(t, "org-1", capture.registerOrg)

	validateMD := capture.md(authv1.AuthService_ValidateToken_FullMethodName)
	require.Equal(t, []string{"req-1"}, validateMD.Get(caller.MetadataRequestID))
	require.Empty(t, validateMD.Get(caller.MetadataSubject))

	orgMD := capture.md(authv1.AuthService_CreateOrganization_FullMethodName)
	require.Equal(t, []string{"req-1"}, orgMD.Get(caller.MetadataRequestID))
	require.Equal(t, []string{"user-1"}, orgMD.Get(caller.MetadataSubject))
	require.Equal(t, []string{"user"}, orgMD.Get(caller.MetadataKind))
	require.Equal(t, []string{"admin"}, orgMD.Get(caller.MetadataRole))
}

type stubAPI struct {
	err      error
	validate *authv1.ValidateTokenResponse
}

func (s *stubAPI) Register(context.Context, *authv1.RegisterRequest, ...grpc.CallOption) (*authv1.RegisterResponse, error) {
	if s.err != nil {
		return nil, s.err
	}

	return &authv1.RegisterResponse{UserId: "user-1", Token: "tok"}, nil
}

func (s *stubAPI) Login(context.Context, *authv1.LoginRequest, ...grpc.CallOption) (*authv1.LoginResponse, error) {
	if s.err != nil {
		return nil, s.err
	}

	return &authv1.LoginResponse{Token: "tok"}, nil
}

func (s *stubAPI) CreateOrganization(context.Context, *authv1.CreateOrganizationRequest, ...grpc.CallOption) (*authv1.CreateOrganizationResponse, error) {
	if s.err != nil {
		return nil, s.err
	}

	return &authv1.CreateOrganizationResponse{OrganizationId: "org-1", Token: "tok"}, nil
}

func (s *stubAPI) ValidateToken(context.Context, *authv1.ValidateTokenRequest, ...grpc.CallOption) (*authv1.ValidateTokenResponse, error) {
	if s.err != nil {
		return nil, s.err
	}

	if s.validate != nil {
		return s.validate, nil
	}

	return &authv1.ValidateTokenResponse{SubjectId: "user-1", Type: "user", Roles: []string{"user"}}, nil
}

func (s *stubAPI) LookupSubject(context.Context, *authv1.LookupSubjectRequest, ...grpc.CallOption) (*authv1.LookupSubjectResponse, error) {
	if s.err != nil {
		return nil, s.err
	}

	return &authv1.LookupSubjectResponse{}, nil
}

type captureAPI struct {
	authv1.UnimplementedAuthServiceServer

	calls       map[string]metadata.MD
	registerOrg string
}

func (c *captureAPI) Register(ctx context.Context, req *authv1.RegisterRequest) (*authv1.RegisterResponse, error) {
	c.save(ctx, authv1.AuthService_Register_FullMethodName)
	c.registerOrg = req.GetOrganizationId()

	return &authv1.RegisterResponse{UserId: "user-1", Token: "tok"}, nil
}

func (c *captureAPI) ValidateToken(ctx context.Context, _ *authv1.ValidateTokenRequest) (*authv1.ValidateTokenResponse, error) {
	c.save(ctx, authv1.AuthService_ValidateToken_FullMethodName)

	return &authv1.ValidateTokenResponse{SubjectId: "user-1", Type: "user", Roles: []string{"user"}}, nil
}

func (c *captureAPI) CreateOrganization(ctx context.Context, _ *authv1.CreateOrganizationRequest) (*authv1.CreateOrganizationResponse, error) {
	c.save(ctx, authv1.AuthService_CreateOrganization_FullMethodName)

	return &authv1.CreateOrganizationResponse{OrganizationId: "org-1", Token: "tok"}, nil
}

func (c *captureAPI) save(ctx context.Context, method string) {
	md, _ := metadata.FromIncomingContext(ctx)
	if c.calls == nil {
		c.calls = map[string]metadata.MD{}
	}

	c.calls[method] = md.Copy()
}

func (c *captureAPI) md(method string) metadata.MD {
	return c.calls[method]
}

func discardLog(t *testing.T) *slog.Logger {
	t.Helper()

	log, err := logger.New(io.Discard, "error")
	require.NoError(t, err)

	return log
}
