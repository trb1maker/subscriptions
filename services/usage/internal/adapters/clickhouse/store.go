package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	"uuid"

	"github.com/trb1maker/subscriptions/services/usage/internal/domain"
)

const (
	insertEvent = `
INSERT INTO usage.events (
    event_id, event_type, occurred_at, owner_kind, owner_id,
    allowance, tokens, payment_id, amount_minor
) VALUES (toUUID(?), ?, ?, ?, toUUID(?), ?, ?, ?, ?)`

	insertPayment = `
INSERT INTO usage.payments (
    event_id, occurred_at, owner_kind, owner_id, payment_id, amount_minor
) VALUES (toUUID(?), ?, ?, toUUID(?), ?, ?)`

	insertTokens = `
INSERT INTO usage.token_usage (
    event_id, occurred_at, owner_kind, owner_id, tokens, outcome
) VALUES (toUUID(?), ?, ?, toUUID(?), ?, ?)`

	eventColumns = `
    toString(event_id), event_type, occurred_at, owner_kind, toString(owner_id),
    allowance, tokens, payment_id, amount_minor`

	selectByID = `
SELECT` + eventColumns + `
FROM usage.events FINAL
WHERE event_id = toUUID(?)`

	selectAll = `
SELECT` + eventColumns + `
FROM usage.events FINAL`

	selectOwner = `
SELECT` + eventColumns + `
FROM usage.events FINAL
WHERE owner_kind = ? AND owner_id = toUUID(?)`
)

// Store — журнал событий в ClickHouse.
type Store struct {
	db *sql.DB
}

// NewStore собирает адаптер поверх соединения ClickHouse.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Find возвращает событие по идентификатору.
func (s *Store) Find(ctx context.Context, id uuid.UUID) (domain.Event, bool, error) {
	rows, err := s.db.QueryContext(ctx, selectByID, id.String())
	if err != nil {
		return domain.Event{}, false, fmt.Errorf("query event: %w", err)
	}

	return single(rows)
}

// Append дописывает событие и связанные строки аналитики.
// Повтор того же идентификатора схлопывается движком таблицы.
func (s *Store) Append(ctx context.Context, event domain.Event) error {
	event = event.Normalized()
	var allowance any
	if event.HasAllowance {
		allowance = event.Allowance
	}

	_, err := s.db.ExecContext(ctx, insertEvent,
		event.ID.String(),
		string(event.Type),
		event.OccurredAt,
		string(event.Owner.Kind),
		event.Owner.ID.String(),
		allowance,
		event.Tokens,
		event.PaymentID,
		event.AmountMinor,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	if event.Type == domain.EventPaymentReceived {
		_, err = s.db.ExecContext(ctx, insertPayment,
			event.ID.String(),
			event.OccurredAt,
			string(event.Owner.Kind),
			event.Owner.ID.String(),
			event.PaymentID,
			event.AmountMinor,
		)
		if err != nil {
			return fmt.Errorf("insert payment: %w", err)
		}
	}

	outcome, ok := event.Type.Outcome()
	if !ok {
		return nil
	}

	_, err = s.db.ExecContext(ctx, insertTokens,
		event.ID.String(),
		event.OccurredAt,
		string(event.Owner.Kind),
		event.Owner.ID.String(),
		event.Tokens,
		outcome,
	)
	if err != nil {
		return fmt.Errorf("insert token usage: %w", err)
	}

	return nil
}

// List возвращает журнал для восстановления проекции.
func (s *Store) List(ctx context.Context) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx, selectAll)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}

	return collect(rows)
}

// ListOwner возвращает журнал одного владельца для живой проекции.
func (s *Store) ListOwner(ctx context.Context, owner domain.Owner) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx, selectOwner, string(owner.Kind), owner.ID.String())
	if err != nil {
		return nil, fmt.Errorf("query owner events: %w", err)
	}

	return collect(rows)
}

func collect(rows *sql.Rows) ([]domain.Event, error) {
	defer func() {
		_ = rows.Close()
	}()

	var events []domain.Event
	for rows.Next() {
		event, scanErr := scanEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}

	return events, nil
}

func single(rows *sql.Rows) (domain.Event, bool, error) {
	defer func() {
		_ = rows.Close()
	}()

	var found []domain.Event
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return domain.Event{}, false, err
		}

		found = append(found, event)
	}

	if err := rows.Err(); err != nil {
		return domain.Event{}, false, fmt.Errorf("read event: %w", err)
	}

	if len(found) == 0 {
		return domain.Event{}, false, nil
	}

	for _, event := range found[1:] {
		if !found[0].Same(event) {
			return domain.Event{}, false, domain.ErrIdempotencyConflict
		}
	}

	return found[0], true, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanEvent(row scanner) (domain.Event, error) {
	var (
		eventID     string
		eventType   string
		occurredAt  time.Time
		ownerKind   string
		ownerID     string
		allowance   sql.NullInt64
		tokens      int64
		paymentID   string
		amountMinor int64
	)
	if err := row.Scan(&eventID, &eventType, &occurredAt, &ownerKind, &ownerID, &allowance, &tokens, &paymentID, &amountMinor); err != nil {
		return domain.Event{}, fmt.Errorf("scan event: %w", err)
	}

	id, err := uuid.Parse(eventID)
	if err != nil {
		return domain.Event{}, fmt.Errorf("parse event id: %w", err)
	}

	owner, err := uuid.Parse(ownerID)
	if err != nil {
		return domain.Event{}, fmt.Errorf("parse owner id: %w", err)
	}

	event := domain.Event{
		ID:          id,
		Type:        domain.EventType(eventType),
		OccurredAt:  occurredAt.UTC().Truncate(time.Millisecond),
		Owner:       domain.Owner{ID: owner, Kind: domain.OwnerKind(ownerKind)},
		Tokens:      tokens,
		PaymentID:   paymentID,
		AmountMinor: amountMinor,
	}
	if allowance.Valid {
		event.HasAllowance = true
		event.Allowance = allowance.Int64
	}

	return event, nil
}
