package migrate_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/pkg/migrate"
)

func TestUpRejectsUnreachableDatabase(t *testing.T) {
	t.Parallel()

	err := migrate.Up(context.Background(), "postgres://127.0.0.1:1/auth?sslmode=disable", fstest.MapFS{})
	require.Error(t, err)
}
