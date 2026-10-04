package redis

import (
	"context"
	"fmt"

	goredis "github.com/redis/go-redis/v9"

	"github.com/trb1maker/subscriptions/pkg/health"
)

// New открывает клиент и проверяет, что Redis принимает запросы.
func New(ctx context.Context, url string) (*goredis.Client, error) {
	options, err := goredis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	client := goredis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()

		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return client, nil
}

// Check — проверка Redis для GET /health.
func Check(client *goredis.Client) health.Check {
	return health.Check{
		Name: "redis",
		Fn: func(ctx context.Context) error {
			return client.Ping(ctx).Err()
		},
	}
}
