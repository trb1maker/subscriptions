package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/trb1maker/subscriptions/services/gateway/internal/domain"
)

const (
	statusProcessed   = "processed"
	statusFailed      = "failed"
	ownerUser         = "user"
	ownerOrganization = "organization"
)

// Generated — ответ генерации.
type Generated struct {
	Status string
	Text   string
	Tokens int64
}

// MessageEvent — событие успешной или неуспешной генерации.
type MessageEvent struct {
	ID         uuid.UUID
	OccurredAt time.Time
	OwnerID    uuid.UUID
	OwnerKind  string
	Failed     bool
	Tokens     int64
}

// Publisher публикует событие учёта.
type Publisher interface {
	Publish(ctx context.Context, event MessageEvent) error
}

// Generator выполняет один запрос генерации.
type Generator interface {
	Generate(ctx context.Context, prompt, key string) (Generated, error)
}

type remembered struct {
	mu     sync.Mutex
	hash   [sha256.Size]byte
	result Generated
	event  MessageEvent
	fixed  bool
	ready  bool
}

// Service проверяет допуск, эмулирует ответ и публикует событие.
type Service struct {
	subscriptions Subscriptions
	usage         Usage
	publisher     Publisher
	emulator      Emulator
	now           func() time.Time

	mu      sync.Mutex
	results map[uuid.UUID]*remembered
}

// New собирает сценарий генерации. now == nil берёт time.Now.
func New(
	subscriptions Subscriptions,
	usage Usage,
	publisher Publisher,
	emulator Emulator,
	now func() time.Time,
) (*Service, error) {
	if subscriptions == nil || usage == nil || publisher == nil || emulator == nil {
		return nil, errors.New("missing dependency")
	}

	if now == nil {
		now = time.Now
	}

	return &Service{
		subscriptions: subscriptions,
		usage:         usage,
		publisher:     publisher,
		emulator:      emulator,
		now:           now,
		results:       map[uuid.UUID]*remembered{},
	}, nil
}

// Generate проводит запрос. Ключ — UUID и идентификатор события.
// Повтор того же ключа и prompt возвращает первый ответ.
// Если публикация ещё не подтверждена, повтор отправляет то же событие.
func (s *Service) Generate(ctx context.Context, prompt, key string) (Generated, error) {
	id, err := parseKey(key)
	if err != nil {
		return Generated{}, err
	}

	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Generated{}, domain.ErrInvalidArgument
	}

	hash := sha256.Sum256([]byte(prompt))
	slot := s.slot(id)
	slot.mu.Lock()
	defer slot.mu.Unlock()

	if slot.fixed {
		if slot.hash != hash {
			return Generated{}, domain.ErrConflict
		}

		if slot.ready {
			return slot.result, nil
		}
	} else {
		if err := ctx.Err(); err != nil {
			return Generated{}, domain.ErrUnavailable
		}

		result, event, err := s.generate(ctx, id)
		if err != nil {
			return Generated{}, err
		}

		slot.hash = hash
		slot.result = result
		slot.event = event
		slot.fixed = true
	}

	if err := ctx.Err(); err != nil {
		return Generated{}, domain.ErrUnavailable
	}

	if err := s.publisher.Publish(ctx, slot.event); err != nil {
		return Generated{}, fmt.Errorf("publish event: %w", err)
	}

	slot.ready = true

	return slot.result, nil
}

func (s *Service) generate(ctx context.Context, id uuid.UUID) (Generated, MessageEvent, error) {
	subscription, err := s.subscriptions.CheckSubscription(ctx)
	if err != nil {
		return Generated{}, MessageEvent{}, fmt.Errorf("check subscription: %w", err)
	}

	if !subscription.Active {
		return Generated{}, MessageEvent{}, domain.ErrSubscriptionInactive
	}

	limit, err := s.usage.CheckLimit(ctx)
	if err != nil {
		return Generated{}, MessageEvent{}, fmt.Errorf("check limit: %w", err)
	}

	if !limit.Allowed {
		return Generated{}, MessageEvent{}, domain.ErrLimitExceeded
	}

	if !validOwner(limit.OwnerKind, limit.OwnerID) {
		return Generated{}, MessageEvent{}, domain.ErrInternal
	}

	outcome, err := s.emulator.Emulate(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Generated{}, MessageEvent{}, domain.ErrUnavailable
		}

		return Generated{}, MessageEvent{}, fmt.Errorf("emulate: %w", err)
	}

	event := MessageEvent{
		ID:         id,
		OccurredAt: s.now().UTC().Truncate(time.Millisecond),
		OwnerID:    limit.OwnerID,
		OwnerKind:  limit.OwnerKind,
		Failed:     outcome.Failed,
		Tokens:     outcome.Tokens,
	}
	result := Generated{Status: statusProcessed, Text: outcome.Text, Tokens: outcome.Tokens}
	if outcome.Failed {
		result = Generated{Status: statusFailed, Tokens: outcome.Tokens}
	}

	return result, event, nil
}

func (s *Service) slot(id uuid.UUID) *remembered {
	s.mu.Lock()
	defer s.mu.Unlock()

	slot, ok := s.results[id]
	if !ok {
		slot = &remembered{}
		s.results[id] = slot
	}

	return slot
}

func parseKey(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || id == uuid.Nil() {
		return uuid.Nil(), domain.ErrInvalidArgument
	}

	return id, nil
}

func validOwner(kind string, id uuid.UUID) bool {
	if id == uuid.Nil() {
		return false
	}

	return kind == ownerUser || kind == ownerOrganization
}
