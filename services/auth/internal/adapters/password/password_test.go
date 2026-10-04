package password_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/services/auth/internal/adapters/password"
	"github.com/trb1maker/subscriptions/services/auth/internal/domain"
)

func TestHashAndMatch(t *testing.T) {
	t.Parallel()

	hasher := password.Hasher{}
	hash, err := hasher.Hash("long-enough")
	require.NoError(t, err)
	require.NoError(t, hasher.Match(hash, "long-enough"))
	require.ErrorIs(t, hasher.Match(hash, "other-password"), domain.ErrInvalidCredentials)
	require.ErrorIs(t, hasher.Burn("long-enough"), domain.ErrInvalidCredentials)
}
