package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/pkg/postgres"
)

func TestNewPoolRejectsUnreachableDatabase(t *testing.T) {
	t.Parallel()

	_, err := postgres.NewPool(context.Background(), "postgres://127.0.0.1:1/auth?sslmode=disable")
	require.Error(t, err)
}
