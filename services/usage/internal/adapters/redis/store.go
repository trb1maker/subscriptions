package redis

import (
	"context"
	"fmt"
	"strconv"
	"uuid"

	goredis "github.com/redis/go-redis/v9"

	"github.com/trb1maker/subscriptions/services/usage/internal/domain"
)

const decimalBase = 10

// restoreLua заменяет проекцию и множество применённых событий владельца.
const restoreLua = `
redis.call('SET', KEYS[1], ARGV[1])
redis.call('DEL', KEYS[2])
for i = 2, #ARGV do
  redis.call('SADD', KEYS[2], ARGV[i])
end
return 1
`

// Store — проекция остатка в Redis.
type Store struct {
	client  *goredis.Client
	restore *goredis.Script
}

// NewStore собирает адаптер поверх клиента Redis.
func NewStore(client *goredis.Client) *Store {
	return &Store{
		client:  client,
		restore: goredis.NewScript(restoreLua),
	}
}

// Restore записывает остаток и отмечает события уже применёнными.
func (s *Store) Restore(ctx context.Context, owner domain.Owner, balance int64, eventIDs []uuid.UUID) error {
	args := make([]any, 0, len(eventIDs)+1)
	args = append(args, strconv.FormatInt(balance, decimalBase))
	for _, id := range eventIDs {
		args = append(args, id.String())
	}

	keys := []string{owner.LimitKey(), owner.SeenKey()}
	if err := s.restore.Run(ctx, s.client, keys, args...).Err(); err != nil {
		return fmt.Errorf("restore limit: %w", err)
	}

	return nil
}

// Get читает остаток. Отсутствие ключа — found == false.
func (s *Store) Get(ctx context.Context, owner domain.Owner) (int64, bool, error) {
	value, err := s.client.Get(ctx, owner.LimitKey()).Result()
	if err == goredis.Nil {
		return 0, false, nil
	}

	if err != nil {
		return 0, false, fmt.Errorf("read limit: %w", err)
	}

	parsed, err := strconv.ParseInt(value, decimalBase, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse limit: %w", err)
	}

	return parsed, true, nil
}
