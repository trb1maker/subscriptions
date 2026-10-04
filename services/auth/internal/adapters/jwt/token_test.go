package jwt_test

import (
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/services/auth/internal/adapters/jwt"
	"github.com/trb1maker/subscriptions/services/auth/internal/domain"
)

func TestIssueAndParse(t *testing.T) {
	t.Parallel()

	issuer, err := jwt.NewIssuer("secret", time.Hour)
	require.NoError(t, err)

	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	subject := uuid.New()
	token, err := issuer.Issue(subject, domain.PrincipalUser, []domain.Role{domain.RoleUser}, now)
	require.NoError(t, err)

	identity, err := issuer.Parse(token, now)
	require.NoError(t, err)
	require.Equal(t, subject, identity.SubjectID)
	require.Equal(t, domain.PrincipalUser, identity.Type)
	require.Equal(t, []domain.Role{domain.RoleUser}, identity.Roles)

	_, err = issuer.Parse(token, now.Add(time.Hour))
	require.ErrorIs(t, err, domain.ErrInvalidToken)

	other, err := jwt.NewIssuer("other-secret", time.Hour)
	require.NoError(t, err)
	_, err = other.Parse(token, now)
	require.ErrorIs(t, err, domain.ErrInvalidToken)
}

func TestNewIssuerRejectsEmptySecret(t *testing.T) {
	t.Parallel()

	_, err := jwt.NewIssuer("", time.Hour)
	require.EqualError(t, err, "empty jwt secret")
}
