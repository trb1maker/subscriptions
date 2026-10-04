package app

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/trb1maker/subscriptions/services/usage/internal/domain"
)

// Ledger хранит журнал событий.
type Ledger interface {
	Find(ctx context.Context, id uuid.UUID) (domain.Event, bool, error)
	Append(ctx context.Context, event domain.Event) error
	List(ctx context.Context) ([]domain.Event, error)
	ListOwner(ctx context.Context, owner domain.Owner) ([]domain.Event, error)
}

// Balances хранит проекцию остатка.
type Balances interface {
	Restore(ctx context.Context, owner domain.Owner, balance int64, eventIDs []uuid.UUID) error
	Get(ctx context.Context, owner domain.Owner) (int64, bool, error)
}

// Directory проверяет вызывающего в Auth.
type Directory interface {
	Lookup(ctx context.Context, id uuid.UUID, kind domain.OwnerKind) (domain.Subject, error)
}

// CheckResult — ответ проверки лимита.
// Owner — владелец остатка. Нулевой, если проекции нет.
type CheckResult struct {
	Allowed   bool
	Remaining int64
	Owner     domain.Owner
}

// Metrics считает списания, токены и овердрафт. Повтор события сюда не попадает.
type Metrics interface {
	MessageConsumed(kind string)
	TokensUsed(kind string, tokens int64)
	Overdraft(kind string)
}

type nopMetrics struct{}

func (nopMetrics) MessageConsumed(string) {}

func (nopMetrics) TokensUsed(string, int64) {}

func (nopMetrics) Overdraft(string) {}

// Service применяет события и отвечает, можно ли начать генерацию.
type Service struct {
	ledger    Ledger
	balances  Balances
	directory Directory
	metrics   Metrics
}

// New собирает сценарии.
func New(ledger Ledger, balances Balances, directory Directory) (*Service, error) {
	if ledger == nil || balances == nil || directory == nil {
		return nil, errors.New("missing dependency")
	}

	return &Service{ledger: ledger, balances: balances, directory: directory, metrics: nopMetrics{}}, nil
}

// SetMetrics подключает счётчики. nil оставляет пустую реализацию.
func (s *Service) SetMetrics(m Metrics) {
	if m != nil {
		s.metrics = m
	}
}

// Apply записывает событие и обновляет остаток. Повтор с тем же телом ничего не меняет.
func (s *Service) Apply(ctx context.Context, event domain.Event) error {
	event = event.Normalized()
	if err := event.Validate(); err != nil {
		return err
	}

	stored, found, err := s.ledger.Find(ctx, event.ID)
	if err != nil {
		return fmt.Errorf("find event: %w", err)
	}

	if found && !stored.Same(event) {
		return domain.ErrIdempotencyConflict
	}

	if err := s.ledger.Append(ctx, event); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	events, err := s.ledger.ListOwner(ctx, event.Owner)
	if err != nil {
		return fmt.Errorf("list owner events: %w", err)
	}

	balance, err := s.writeProjection(ctx, event.Owner, events)
	if err != nil {
		return err
	}

	if !found {
		s.observe(event, balance)
	}

	return nil
}

// Restore пересобирает проекцию всех владельцев из журнала.
func (s *Service) Restore(ctx context.Context) error {
	events, err := s.ledger.List(ctx)
	if err != nil {
		return fmt.Errorf("list events: %w", err)
	}

	grouped := make(map[domain.Owner][]domain.Event)
	for _, event := range events {
		grouped[event.Owner] = append(grouped[event.Owner], event)
	}

	for owner, list := range grouped {
		if _, err := s.writeProjection(ctx, owner, list); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) writeProjection(ctx context.Context, owner domain.Owner, events []domain.Event) (int64, error) {
	balance, err := domain.Project(events)
	if err != nil {
		return 0, err
	}

	if err := s.balances.Restore(ctx, owner, balance, eventIDs(events)); err != nil {
		return 0, fmt.Errorf("restore balance: %w", err)
	}

	return balance, nil
}

func (s *Service) observe(event domain.Event, balance int64) {
	kind := string(event.Owner.Kind)
	if event.Type == domain.EventMessageProcessed {
		s.metrics.MessageConsumed(kind)
		if balance < 0 {
			s.metrics.Overdraft(kind)
		}
	}

	if _, ok := event.Type.Outcome(); ok {
		s.metrics.TokensUsed(kind, event.Tokens)
	}
}

func eventIDs(events []domain.Event) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(events))
	ids := make([]uuid.UUID, 0, len(events))
	for _, event := range events {
		if _, ok := seen[event.ID]; ok {
			continue
		}

		seen[event.ID] = struct{}{}
		ids = append(ids, event.ID)
	}

	return ids
}
