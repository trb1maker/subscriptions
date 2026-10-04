package clickhouse_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/pkg/clickhouse"
)

func TestOpenRejectsUnreachableClickHouse(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	t.Cleanup(cancel)

	_, err := clickhouse.Open(ctx, "clickhouse://127.0.0.1:1/default")
	require.Error(t, err)
}
