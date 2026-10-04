package app_test

import (
	"context"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/services/auth/internal/app"
	"github.com/trb1maker/subscriptions/services/auth/internal/domain"
)

func TestRegisterLoginAndValidate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	svc := newService(t, newMemStore(), &fakePasswords{}, newFakeTokens(), func() time.Time { return now })

	created, err := svc.Register(context.Background(), "ada@example.com", "long-enough", "key-1", nil)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil(), created.UserID)

	identity, err := svc.ValidateToken(created.Token)
	require.NoError(t, err)
	require.Equal(t, created.UserID, identity.SubjectID)
	require.Equal(t, domain.PrincipalUser, identity.Type)
	require.Equal(t, []domain.Role{domain.RoleUser}, identity.Roles)

	token, err := svc.Login(context.Background(), "Ada@Example.com", "long-enough")
	require.NoError(t, err)
	identity, err = svc.ValidateToken(token)
	require.NoError(t, err)
	require.Equal(t, created.UserID, identity.SubjectID)
}

func TestRegisterReplayKeepsUserAndIssuesNewToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	clock := now
	passwords := &fakePasswords{}
	svc := newService(t, newMemStore(), passwords, newFakeTokens(), func() time.Time { return clock })

	first, err := svc.Register(context.Background(), "ada@example.com", "long-enough", "key-1", nil)
	require.NoError(t, err)

	clock = now.Add(time.Minute)
	second, err := svc.Register(context.Background(), "ada@example.com", "long-enough", "key-1", nil)
	require.NoError(t, err)
	require.Equal(t, first.UserID, second.UserID)
	require.NotEqual(t, first.Token, second.Token)
	require.Equal(t, 1, passwords.hashes)

	_, err = svc.Register(context.Background(), "ada@example.com", "other-password", "key-1", nil)
	require.ErrorIs(t, err, domain.ErrIdempotencyConflict)
	require.Equal(t, 1, passwords.hashes)

	_, err = svc.Register(context.Background(), "ada@example.com", "long-enough", "key-2", nil)
	require.ErrorIs(t, err, domain.ErrEmailTaken)
}

func TestRegisterUnknownOrganization(t *testing.T) {
	t.Parallel()

	svc := newService(t, newMemStore(), &fakePasswords{}, newFakeTokens(), nil)
	orgID := uuid.New()
	_, err := svc.Register(context.Background(), "ada@example.com", "long-enough", "key-1", &orgID)
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound)
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	t.Parallel()

	passwords := &fakePasswords{}
	svc := newService(t, newMemStore(), passwords, newFakeTokens(), nil)
	_, err := svc.Register(context.Background(), "ada@example.com", "long-enough", "key-1", nil)
	require.NoError(t, err)

	_, err = svc.Login(context.Background(), "ada@example.com", "wrong-password")
	require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	require.Zero(t, passwords.burns)

	_, err = svc.Login(context.Background(), "missing@example.com", "long-enough")
	require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	require.Equal(t, 1, passwords.burns)
}

func TestCreateOrganizationAndExpiredToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	clock := now
	tokens := newFakeTokens()
	svc := newService(t, newMemStore(), &fakePasswords{}, tokens, func() time.Time { return clock })

	created, err := svc.CreateOrganization(context.Background(), "Acme", "org-1")
	require.NoError(t, err)

	identity, err := svc.ValidateToken(created.Token)
	require.NoError(t, err)
	require.Equal(t, created.OrganizationID, identity.SubjectID)
	require.Equal(t, domain.PrincipalOrganization, identity.Type)
	require.Equal(t, []domain.Role{domain.RoleAdmin}, identity.Roles)

	again, err := svc.CreateOrganization(context.Background(), "Acme", "org-1")
	require.NoError(t, err)
	require.Equal(t, created.OrganizationID, again.OrganizationID)

	clock = now.Add(tokens.ttl + time.Second)
	_, err = svc.ValidateToken(created.Token)
	require.ErrorIs(t, err, domain.ErrInvalidToken)
}

func TestNewRejectsEmptyPepper(t *testing.T) {
	t.Parallel()

	_, err := app.New(newMemStore(), &fakePasswords{}, newFakeTokens(), nil, nil)
	require.EqualError(t, err, "empty request pepper")
}

func TestRegisterValidation(t *testing.T) {
	t.Parallel()

	svc := newService(t, newMemStore(), &fakePasswords{}, newFakeTokens(), nil)
	_, err := svc.Register(context.Background(), "ada@example.com", "long-enough", " ", nil)
	require.ErrorIs(t, err, domain.ErrInvalidIdempotencyKey)
	_, err = svc.Register(context.Background(), "bad", "long-enough", "key", nil)
	require.ErrorIs(t, err, domain.ErrInvalidEmail)
	_, err = svc.Register(context.Background(), "ada@example.com", "short", "key", nil)
	require.ErrorIs(t, err, domain.ErrInvalidPassword)
}

func newService(t *testing.T, store *memStore, passwords app.Passwords, tokens app.Tokens, now func() time.Time) *app.Service {
	t.Helper()

	svc, err := app.New(store, passwords, tokens, []byte("pepper"), now)
	require.NoError(t, err)

	return svc
}

type fakePasswords struct {
	hashes int
	burns  int
}

func (f *fakePasswords) Hash(password string) (string, error) {
	f.hashes++

	return "hash:" + password, nil
}

func (f *fakePasswords) Match(hash, password string) error {
	if hash != "hash:"+password {
		return domain.ErrInvalidCredentials
	}

	return nil
}

func (f *fakePasswords) Burn(string) error {
	f.burns++

	return domain.ErrInvalidCredentials
}

type issuedToken struct {
	identity app.Identity
	expires  time.Time
}

type fakeTokens struct {
	mu     sync.Mutex
	ttl    time.Duration
	issued map[string]issuedToken
	n      int
}

func newFakeTokens() *fakeTokens {
	return &fakeTokens{ttl: time.Hour, issued: map[string]issuedToken{}}
}

func (f *fakeTokens) Issue(subject uuid.UUID, kind domain.PrincipalType, roles []domain.Role, now time.Time) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.n++
	token := subject.String() + ":" + now.Format(time.RFC3339Nano)
	f.issued[token] = issuedToken{
		identity: app.Identity{SubjectID: subject, Type: kind, Roles: roles},
		expires:  now.Add(f.ttl),
	}

	return token, nil
}

func (f *fakeTokens) Parse(token string, now time.Time) (app.Identity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	issued, ok := f.issued[token]
	if !ok || !now.Before(issued.expires) {
		return app.Identity{}, domain.ErrInvalidToken
	}

	return issued.identity, nil
}

type storedCommand struct {
	hash       []byte
	resourceID uuid.UUID
}

type memStore struct {
	mu            sync.Mutex
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
	m.mu.Lock()
	defer m.mu.Unlock()

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
	m.mu.Lock()
	defer m.mu.Unlock()

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
	m.mu.Lock()
	defer m.mu.Unlock()

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
	m.mu.Lock()
	defer m.mu.Unlock()

	id, ok := m.usersByEmail[email]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}

	return m.users[id], nil
}
