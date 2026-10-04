package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/services/auth/internal/domain"
)

func TestParseEmail(t *testing.T) {
	t.Parallel()

	email, err := domain.ParseEmail("  Ada@Example.com ")
	require.NoError(t, err)
	require.Equal(t, "ada@example.com", email)

	_, err = domain.ParseEmail("not-an-email")
	require.ErrorIs(t, err, domain.ErrInvalidEmail)
}

func TestValidatePassword(t *testing.T) {
	t.Parallel()

	require.NoError(t, domain.ValidatePassword("long-enough"))
	require.ErrorIs(t, domain.ValidatePassword("short"), domain.ErrInvalidPassword)
	require.ErrorIs(t, domain.ValidatePassword(strings.Repeat("a", 73)), domain.ErrInvalidPassword)
}

func TestParseName(t *testing.T) {
	t.Parallel()

	name, err := domain.ParseName("  Acme ")
	require.NoError(t, err)
	require.Equal(t, "Acme", name)
	_, err = domain.ParseName("   ")
	require.ErrorIs(t, err, domain.ErrInvalidName)
}

func TestParseIdempotencyKey(t *testing.T) {
	t.Parallel()

	key, err := domain.ParseIdempotencyKey(" key ")
	require.NoError(t, err)
	require.Equal(t, "key", key)
	_, err = domain.ParseIdempotencyKey("  ")
	require.ErrorIs(t, err, domain.ErrInvalidIdempotencyKey)
}
