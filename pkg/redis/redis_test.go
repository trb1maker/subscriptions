package redis_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/pkg/redis"
)

func TestNewRejectsUnreachableRedis(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	t.Cleanup(cancel)

	_, err := redis.New(ctx, "redis://127.0.0.1:1/0")
	require.Error(t, err)
}

func TestNewRejectsSentinelWithoutMaster(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	t.Cleanup(cancel)

	_, err := redis.New(ctx, "sentinel://:secret@127.0.0.1:1/0")
	require.Error(t, err)
}
