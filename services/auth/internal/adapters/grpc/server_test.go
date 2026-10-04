package grpcapi_test

import (
	"context"
	"io"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	authv1 "github.com/trb1maker/subscriptions/api/gen/auth/v1"
	"github.com/trb1maker/subscriptions/pkg/logger"
	grpcapi "github.com/trb1maker/subscriptions/services/auth/internal/adapters/grpc"
	"github.com/trb1maker/subscriptions/services/auth/internal/adapters/jwt"
	"github.com/trb1maker/subscriptions/services/auth/internal/adapters/password"
	"github.com/trb1maker/subscriptions/services/auth/internal/app"
	"github.com/trb1maker/subscriptions/services/auth/internal/domain"
)

func TestRegisterLoginAndValidate(t *testing.T) {
	t.Parallel()

	srv := newServer(t)
	created, err := srv.Register(context.Background(), &authv1.RegisterRequest{
		IdempotencyKey: "key-1",
		Email:          "ada@example.com",
		Password:       "long-enough",
	})
	require.NoError(t, err)

	checked, err := srv.ValidateToken(context.Background(), &authv1.ValidateTokenRequest{Token: created.GetToken()})
	require.NoError(t, err)
	require.Equal(t, created.GetUserId(), checked.GetSubjectId())
	require.Equal(t, string(domain.PrincipalUser), checked.GetType())
	require.Equal(t, []string{string(domain.RoleUser)}, checked.GetRoles())

	logged, err := srv.Login(context.Background(), &authv1.LoginRequest{
		Email:    "ada@example.com",
		Password: "long-enough",
	})
	require.NoError(t, err)
	require.NotEmpty(t, logged.GetToken())
}

func TestRegisterMapsErrors(t *testing.T) {
	t.Parallel()

	srv := newServer(t)
	_, err := srv.Register(context.Background(), &authv1.RegisterRequest{
		IdempotencyKey: "key-1",
		Email:          "ada@example.com",
		Password:       "short",
	})
	requireCode(t, err, codes.InvalidArgument)

	badOrg := "not-a-uuid"
	_, err = srv.Register(context.Background(), &authv1.RegisterRequest{
		IdempotencyKey: "key-1",
		Email:          "ada@example.com",
		Password:       "long-enough",
		OrganizationId: &badOrg,
	})
	requireCode(t, err, codes.InvalidArgument)

	orgID := uuid.New().String()
	_, err = srv.Register(context.Background(), &authv1.RegisterRequest{
		IdempotencyKey: "key-1",
		Email:          "ada@example.com",
		Password:       "long-enough",
		OrganizationId: &orgID,
	})
	requireCode(t, err, codes.NotFound)

	_, err = srv.Register(context.Background(), &authv1.RegisterRequest{
		IdempotencyKey: "key-1",
		Email:          "ada@example.com",
		Password:       "long-enough",
	})
	require.NoError(t, err)

	_, err = srv.Register(context.Background(), &authv1.RegisterRequest{
		IdempotencyKey: "key-1",
		Email:          "ada@example.com",
		Password:       "other-password",
	})
	requireCode(t, err, codes.FailedPrecondition)

	_, err = srv.Register(context.Background(), &authv1.RegisterRequest{
		IdempotencyKey: "key-2",
		Email:          "ada@example.com",
		Password:       "long-enough",
	})
	requireCode(t, err, codes.AlreadyExists)

	_, err = srv.Login(context.Background(), &authv1.LoginRequest{
		Email:    "ada@example.com",
		Password: "wrong-password",
	})
	requireCode(t, err, codes.Unauthenticated)

	_, err = srv.ValidateToken(context.Background(), &authv1.ValidateTokenRequest{Token: "bad"})
	requireCode(t, err, codes.Unauthenticated)
}

func TestCreateOrganizationReplay(t *testing.T) {
	t.Parallel()

	srv := newServer(t)
	first, err := srv.CreateOrganization(context.Background(), &authv1.CreateOrganizationRequest{
		IdempotencyKey: "org-1",
		Name:           "Acme",
	})
	require.NoError(t, err)

	second, err := srv.CreateOrganization(context.Background(), &authv1.CreateOrganizationRequest{
		IdempotencyKey: "org-1",
		Name:           "Acme",
	})
	require.NoError(t, err)
	require.Equal(t, first.GetOrganizationId(), second.GetOrganizationId())

	_, err = srv.CreateOrganization(context.Background(), &authv1.CreateOrganizationRequest{
		IdempotencyKey: "org-1",
		Name:           "Other",
	})
	requireCode(t, err, codes.FailedPrecondition)
}

func TestLookupSubject(t *testing.T) {
	t.Parallel()

	srv := newServer(t)
	created, err := srv.Register(context.Background(), &authv1.RegisterRequest{
		IdempotencyKey: "key-1",
		Email:          "ada@example.com",
		Password:       "long-enough",
	})
	require.NoError(t, err)

	found, err := srv.LookupSubject(context.Background(), &authv1.LookupSubjectRequest{
		SubjectId: created.GetUserId(),
		Type:      string(domain.PrincipalUser),
	})
	require.NoError(t, err)
	require.Empty(t, found.GetOrganizationId())

	_, err = srv.LookupSubject(context.Background(), &authv1.LookupSubjectRequest{
		SubjectId: uuid.New().String(),
		Type:      string(domain.PrincipalUser),
	})
	requireCode(t, err, codes.NotFound)

	_, err = srv.LookupSubject(context.Background(), &authv1.LookupSubjectRequest{
		SubjectId: "not-a-uuid",
		Type:      string(domain.PrincipalUser),
	})
	requireCode(t, err, codes.InvalidArgument)
}

func newServer(t *testing.T) *grpcapi.Server {
	t.Helper()

	log, err := logger.New(io.Discard, "error")
	require.NoError(t, err)
	issuer, err := jwt.NewIssuer("secret", time.Hour)
	require.NoError(t, err)

	svc, err := app.New(newMemStore(), password.Hasher{}, issuer, []byte("pepper"), time.Now)
	require.NoError(t, err)

	return grpcapi.NewServer(svc, log)
}

func requireCode(t *testing.T, err error, code codes.Code) {
	t.Helper()

	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, code, st.Code())
}

type storedCommand struct {
	hash       []byte
	resourceID uuid.UUID
}

type memStore struct {
	users         map[uuid.UUID]domain.User
	usersByEmail  map[string]uuid.UUID
	organizations map[uuid.UUID]domain.Organization
	keys          map[string]storedCommand
}

func newMemStore() *memStore {
	return &memStore{
		users:         map[uuid.UUID]domain.User{},
		usersByEmail:  map[string]uuid.UUID{},
		organizations: map[uuid.UUID]domain.Organization{},
		keys:          map[string]storedCommand{},
	}
}

func (m *memStore) ReplayUser(_ context.Context, key string, requestHash []byte) (domain.User, error) {
	existing, ok := m.keys[key]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}

	if string(existing.hash) != string(requestHash) {
		return domain.User{}, domain.ErrIdempotencyConflict
	}

	user, ok := m.users[existing.resourceID]
	if !ok {
		return domain.User{}, domain.ErrIdempotencyConflict
	}

	return user, nil
}

func (m *memStore) RegisterUser(_ context.Context, user domain.User, key string, requestHash []byte) (domain.User, error) {
	if existing, ok := m.keys[key]; ok {
		if string(existing.hash) != string(requestHash) {
			return domain.User{}, domain.ErrIdempotencyConflict
		}

		return m.users[existing.resourceID], nil
	}

	if user.OrganizationID != nil {
		if _, ok := m.organizations[*user.OrganizationID]; !ok {
			return domain.User{}, domain.ErrOrganizationNotFound
		}
	}

	if _, ok := m.usersByEmail[user.Email]; ok {
		return domain.User{}, domain.ErrEmailTaken
	}

	m.users[user.ID] = user
	m.usersByEmail[user.Email] = user.ID
	m.keys[key] = storedCommand{hash: append([]byte(nil), requestHash...), resourceID: user.ID}

	return user, nil
}

func (m *memStore) CreateOrganization(_ context.Context, org domain.Organization, key string, requestHash []byte) (domain.Organization, error) {
	if existing, ok := m.keys[key]; ok {
		if string(existing.hash) != string(requestHash) {
			return domain.Organization{}, domain.ErrIdempotencyConflict
		}

		return m.organizations[existing.resourceID], nil
	}

	m.organizations[org.ID] = org
	m.keys[key] = storedCommand{hash: append([]byte(nil), requestHash...), resourceID: org.ID}

	return org, nil
}

func (m *memStore) UserByEmail(_ context.Context, email string) (domain.User, error) {
	id, ok := m.usersByEmail[email]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}

	return m.users[id], nil
}

func (m *memStore) UserByID(_ context.Context, id uuid.UUID) (domain.User, error) {
	user, ok := m.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}

	return user, nil
}

func (m *memStore) OrganizationByID(_ context.Context, id uuid.UUID) (domain.Organization, error) {
	org, ok := m.organizations[id]
	if !ok {
		return domain.Organization{}, domain.ErrNotFound
	}

	return org, nil
}
