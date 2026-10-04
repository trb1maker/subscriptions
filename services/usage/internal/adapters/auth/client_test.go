package auth_test

import (
	"context"
	"log/slog"
	"testing"
	"uuid"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authv1 "github.com/trb1maker/subscriptions/api/gen/auth/v1"
	"github.com/trb1maker/subscriptions/services/usage/internal/adapters/auth"
	"github.com/trb1maker/subscriptions/services/usage/internal/domain"
)

func TestLookupMapsNotFound(t *testing.T) {
	t.Parallel()

	client := auth.NewClient(stubAPI{err: status.Error(codes.NotFound, "not found")}, discardLog())
	_, err := client.Lookup(context.Background(), uuid.New(), domain.OwnerUser)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestLookupReturnsOrganization(t *testing.T) {
	t.Parallel()

	orgID := uuid.New().String()
	client := auth.NewClient(stubAPI{orgID: orgID}, discardLog())
	subject, err := client.Lookup(context.Background(), uuid.New(), domain.OwnerUser)
	require.NoError(t, err)
	require.NotNil(t, subject.OrganizationID)
	require.Equal(t, orgID, subject.OrganizationID.String())
}

type stubAPI struct {
	orgID string
	err   error
}

func (s stubAPI) LookupSubject(context.Context, *authv1.LookupSubjectRequest, ...grpc.CallOption) (*authv1.LookupSubjectResponse, error) {
	if s.err != nil {
		return nil, s.err
	}

	resp := &authv1.LookupSubjectResponse{}
	if s.orgID != "" {
		resp.OrganizationId = &s.orgID
	}

	return resp, nil
}

func discardLog() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
