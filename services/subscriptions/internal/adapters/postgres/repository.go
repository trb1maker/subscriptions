package postgres

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/trb1maker/subscriptions/services/subscriptions/internal/domain"
)

const (
	operationCreateTariff         = "create_tariff"
	operationCreateSubscription   = "create_subscription"
	operationChangeSubscription   = "change_subscription"
	operationReceivePayment       = "receive_payment"
	constraintIdempotencyKeysPkey = "idempotency_keys_pkey"
	constraintOneBase             = "tariffs_one_base"
	constraintOneActiveUser       = "subscriptions_one_active_user"
	constraintOneActiveOrg        = "subscriptions_one_active_organization"
	pgUniqueViolation             = "23505"
)

var errIdempotencyRace = errors.New("idempotency race")

// Repository хранит тарифы и подписки в Postgres.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository собирает доступ к базе.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// ReplayTariff возвращает тариф, уже созданный этим ключом.
func (r *Repository) ReplayTariff(ctx context.Context, key string, requestHash []byte) (domain.Tariff, error) {
	row, err := New(r.pool).FindIdempotency(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Tariff{}, domain.ErrNotFound
		}

		return domain.Tariff{}, fmt.Errorf("find idempotency key: %w", err)
	}

	if row.Operation != operationCreateTariff || subtle.ConstantTimeCompare(row.RequestHash, requestHash) != 1 {
		return domain.Tariff{}, domain.ErrIdempotencyConflict
	}

	return r.Tariff(ctx, row.ResourceID)
}

// CreateTariff создаёт тариф или возвращает уже созданный по ключу идемпотентности.
func (r *Repository) CreateTariff(ctx context.Context, tariff domain.Tariff, key string, requestHash []byte) (domain.Tariff, error) {
	stored, err := r.insertTariff(ctx, tariff, key, requestHash)
	if errors.Is(err, errIdempotencyRace) {
		return r.ReplayTariff(ctx, key, requestHash)
	}

	if err != nil {
		return domain.Tariff{}, err
	}

	return stored, nil
}

// ListTariffs возвращает каталог.
func (r *Repository) ListTariffs(ctx context.Context) ([]domain.Tariff, error) {
	rows, err := New(r.pool).ListTariffs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tariffs: %w", err)
	}

	tariffs := make([]domain.Tariff, 0, len(rows))
	for _, row := range rows {
		tariff, err := tariffFrom(row.ID, row.Name, row.MonthlyPriceMinor, row.MessageLimit, row.Type, row.IsBaseTariff)
		if err != nil {
			return nil, err
		}

		tariffs = append(tariffs, tariff)
	}

	return tariffs, nil
}

// Tariff возвращает тариф по идентификатору.
func (r *Repository) Tariff(ctx context.Context, id uuid.UUID) (domain.Tariff, error) {
	row, err := New(r.pool).FindTariff(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Tariff{}, domain.ErrNotFound
		}

		return domain.Tariff{}, fmt.Errorf("find tariff: %w", err)
	}

	return tariffFrom(row.ID, row.Name, row.MonthlyPriceMinor, row.MessageLimit, row.Type, row.IsBaseTariff)
}

// ReplaySubscription возвращает подписку, уже записанную этим ключом.
func (r *Repository) ReplaySubscription(ctx context.Context, key string, requestHash []byte) (domain.Subscription, error) {
	row, err := New(r.pool).FindIdempotency(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Subscription{}, domain.ErrNotFound
		}

		return domain.Subscription{}, fmt.Errorf("find idempotency key: %w", err)
	}

	if (row.Operation != operationCreateSubscription && row.Operation != operationChangeSubscription) ||
		subtle.ConstantTimeCompare(row.RequestHash, requestHash) != 1 {
		return domain.Subscription{}, domain.ErrIdempotencyConflict
	}

	return r.Subscription(ctx, row.ResourceID)
}

// CreateSubscription создаёт подписку или возвращает уже созданную по ключу идемпотентности.
func (r *Repository) CreateSubscription(ctx context.Context, sub domain.Subscription, key string, requestHash []byte) (domain.Subscription, error) {
	stored, err := r.insertSubscription(ctx, sub, key, requestHash)
	if errors.Is(err, errIdempotencyRace) {
		return r.ReplaySubscription(ctx, key, requestHash)
	}

	if err != nil {
		return domain.Subscription{}, err
	}

	return stored, nil
}

// Subscription возвращает подписку по идентификатору.
func (r *Repository) Subscription(ctx context.Context, id uuid.UUID) (domain.Subscription, error) {
	row, err := New(r.pool).FindSubscription(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Subscription{}, domain.ErrNotFound
		}

		return domain.Subscription{}, fmt.Errorf("find subscription: %w", err)
	}

	return subscriptionFrom(row.ID, row.UserID, row.OrganizationID, row.TariffID, row.Status, row.MessageAllowance, row.CurrentPeriodStart, row.CurrentPeriodEnd, paymentID(row.PaymentID))
}

// ActiveByOwner возвращает активную подписку пользователя или организации.
func (r *Repository) ActiveByOwner(ctx context.Context, id uuid.UUID, kind domain.OwnerKind) (domain.Subscription, error) {
	queries := New(r.pool)
	switch kind {
	case domain.OwnerUser:
		row, err := queries.FindActiveSubscriptionByUser(ctx, &id)
		if err != nil {
			return missingSubscription(err)
		}

		return subscriptionFrom(row.ID, row.UserID, row.OrganizationID, row.TariffID, row.Status, row.MessageAllowance, row.CurrentPeriodStart, row.CurrentPeriodEnd, paymentID(row.PaymentID))
	case domain.OwnerOrganization:
		row, err := queries.FindActiveSubscriptionByOrganization(ctx, &id)
		if err != nil {
			return missingSubscription(err)
		}

		return subscriptionFrom(row.ID, row.UserID, row.OrganizationID, row.TariffID, row.Status, row.MessageAllowance, row.CurrentPeriodStart, row.CurrentPeriodEnd, paymentID(row.PaymentID))
	default:
		return domain.Subscription{}, domain.ErrUnauthenticated
	}
}

// ChangeSubscription меняет тариф подписки под блокировкой строки.
func (r *Repository) ChangeSubscription(
	ctx context.Context,
	id uuid.UUID,
	next domain.Tariff,
	key string,
	requestHash []byte,
) (domain.Subscription, error) {
	stored, err := r.updateSubscription(ctx, id, next, key, requestHash)
	if errors.Is(err, errIdempotencyRace) {
		return r.ReplaySubscription(ctx, key, requestHash)
	}

	if err != nil {
		return domain.Subscription{}, err
	}

	return stored, nil
}

// ReplayPayment возвращает подписку, уже продлённую этим платежом.
func (r *Repository) ReplayPayment(ctx context.Context, key string, requestHash []byte) (domain.Subscription, error) {
	row, err := New(r.pool).FindIdempotency(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Subscription{}, domain.ErrNotFound
		}

		return domain.Subscription{}, fmt.Errorf("find idempotency key: %w", err)
	}

	if row.Operation != operationReceivePayment || subtle.ConstantTimeCompare(row.RequestHash, requestHash) != 1 {
		return domain.Subscription{}, domain.ErrIdempotencyConflict
	}

	return r.Subscription(ctx, row.ResourceID)
}

// RenewSubscription продлевает подписку и записывает платёж. Повтор ключа возвращает уже сохранённую строку.
func (r *Repository) RenewSubscription(
	ctx context.Context,
	id uuid.UUID,
	remaining, amountMinor int64,
	payment string,
	now time.Time,
	key string,
	requestHash []byte,
) (domain.Subscription, error) {
	stored, err := r.renewSubscription(ctx, id, remaining, amountMinor, payment, now, key, requestHash)
	if errors.Is(err, errIdempotencyRace) {
		return r.ReplayPayment(ctx, key, requestHash)
	}

	if err != nil {
		return domain.Subscription{}, err
	}

	return stored, nil
}

// CloseExpired применяет правила окончания периода к заблокированным строкам.
func (r *Repository) CloseExpired(ctx context.Context, now time.Time) (domain.ExpiryReport, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ExpiryReport{}, fmt.Errorf("begin close expired: %w", err)
	}

	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	queries := New(tx)
	due, err := queries.LockDueSubscriptions(ctx, now)
	if err != nil {
		return domain.ExpiryReport{}, fmt.Errorf("lock due subscriptions: %w", err)
	}

	base, err := baseTariff(ctx, queries)
	if err != nil {
		return domain.ExpiryReport{}, err
	}

	var report domain.ExpiryReport
	for _, row := range due {
		sub, err := subscriptionFrom(row.ID, row.UserID, row.OrganizationID, row.TariffID, row.Status, row.MessageAllowance, row.CurrentPeriodStart, row.CurrentPeriodEnd, paymentID(row.PaymentID))
		if err != nil {
			return domain.ExpiryReport{}, err
		}

		tariff, err := tariffFrom(row.TariffID, row.Name, row.MonthlyPriceMinor, row.MessageLimit, row.Type, row.IsBaseTariff)
		if err != nil {
			return domain.ExpiryReport{}, err
		}

		next, apply, err := domain.PlanExpiry(sub, tariff, base)
		if err != nil {
			return domain.ExpiryReport{}, err
		}

		if !apply {
			report.Skipped = append(report.Skipped, sub.ID)

			continue
		}

		if err := queries.UpdateSubscription(ctx, UpdateSubscriptionParams{
			ID:                 next.ID,
			TariffID:           next.TariffID,
			Status:             string(next.Status),
			MessageAllowance:   next.MessageAllowance,
			CurrentPeriodStart: next.PeriodStart,
			CurrentPeriodEnd:   next.PeriodEnd,
		}); err != nil {
			return domain.ExpiryReport{}, fmt.Errorf("update subscription: %w", err)
		}

		report.Closed++
		report.Applied = append(report.Applied, next)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ExpiryReport{}, fmt.Errorf("commit close expired: %w", err)
	}

	return report, nil
}

func (r *Repository) insertTariff(ctx context.Context, tariff domain.Tariff, key string, requestHash []byte) (domain.Tariff, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Tariff{}, fmt.Errorf("begin create tariff: %w", err)
	}

	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	queries := New(tx)
	if existing, ok, err := lockedTariff(ctx, queries, key, requestHash); err != nil || ok {
		return existing, err
	}

	if err := insertKey(ctx, queries, key, operationCreateTariff, requestHash, tariff.ID); err != nil {
		return domain.Tariff{}, err
	}

	limit, err := messageLimit(tariff.MessageLimit)
	if err != nil {
		return domain.Tariff{}, err
	}

	if err := queries.InsertTariff(ctx, InsertTariffParams{
		ID:                tariff.ID,
		Name:              tariff.Name,
		MonthlyPriceMinor: tariff.MonthlyPriceMinor,
		MessageLimit:      limit,
		Type:              string(tariff.Type),
		IsBaseTariff:      tariff.IsBase,
	}); err != nil {
		if constraint(err) == constraintOneBase {
			return domain.Tariff{}, domain.ErrBaseTariffExists
		}

		return domain.Tariff{}, fmt.Errorf("insert tariff: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Tariff{}, fmt.Errorf("commit create tariff: %w", err)
	}

	return tariff, nil
}

func (r *Repository) insertSubscription(ctx context.Context, sub domain.Subscription, key string, requestHash []byte) (domain.Subscription, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("begin create subscription: %w", err)
	}

	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	queries := New(tx)
	if existing, ok, err := lockedSubscription(ctx, queries, key, requestHash); err != nil || ok {
		return existing, err
	}

	if err := insertKey(ctx, queries, key, operationCreateSubscription, requestHash, sub.ID); err != nil {
		return domain.Subscription{}, err
	}

	if err := queries.InsertSubscription(ctx, InsertSubscriptionParams{
		ID:                 sub.ID,
		UserID:             sub.UserID,
		OrganizationID:     sub.OrganizationID,
		TariffID:           sub.TariffID,
		Status:             string(sub.Status),
		MessageAllowance:   sub.MessageAllowance,
		CurrentPeriodStart: sub.PeriodStart,
		CurrentPeriodEnd:   sub.PeriodEnd,
	}); err != nil {
		if constraint(err) == constraintOneActiveUser || constraint(err) == constraintOneActiveOrg {
			return domain.Subscription{}, domain.ErrActiveSubscription
		}

		return domain.Subscription{}, fmt.Errorf("insert subscription: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Subscription{}, fmt.Errorf("commit create subscription: %w", err)
	}

	return sub, nil
}

func (r *Repository) updateSubscription(
	ctx context.Context,
	id uuid.UUID,
	next domain.Tariff,
	key string,
	requestHash []byte,
) (domain.Subscription, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("begin change subscription: %w", err)
	}

	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	queries := New(tx)
	if existing, ok, err := lockedSubscription(ctx, queries, key, requestHash); err != nil || ok {
		return existing, err
	}

	current, err := queries.LockSubscription(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Subscription{}, domain.ErrNotFound
		}

		return domain.Subscription{}, fmt.Errorf("lock subscription: %w", err)
	}

	oldRow, err := queries.FindTariff(ctx, current.TariffID)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("find tariff: %w", err)
	}

	old, err := tariffFrom(oldRow.ID, oldRow.Name, oldRow.MonthlyPriceMinor, oldRow.MessageLimit, oldRow.Type, oldRow.IsBaseTariff)
	if err != nil {
		return domain.Subscription{}, err
	}

	sub, err := subscriptionFrom(
		current.ID,
		current.UserID,
		current.OrganizationID,
		current.TariffID,
		current.Status,
		current.MessageAllowance,
		current.CurrentPeriodStart,
		current.CurrentPeriodEnd,
		paymentID(current.PaymentID),
	)
	if err != nil {
		return domain.Subscription{}, err
	}

	allowance, err := domain.AllowanceOnChange(old, next, sub.MessageAllowance)
	if err != nil {
		return domain.Subscription{}, err
	}

	sub.TariffID = next.ID
	sub.MessageAllowance = allowance
	if err := insertKey(ctx, queries, key, operationChangeSubscription, requestHash, sub.ID); err != nil {
		return domain.Subscription{}, err
	}

	if err := queries.UpdateSubscription(ctx, UpdateSubscriptionParams{
		ID:                 sub.ID,
		TariffID:           sub.TariffID,
		Status:             string(sub.Status),
		MessageAllowance:   sub.MessageAllowance,
		CurrentPeriodStart: sub.PeriodStart,
		CurrentPeriodEnd:   sub.PeriodEnd,
	}); err != nil {
		return domain.Subscription{}, fmt.Errorf("update subscription: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Subscription{}, fmt.Errorf("commit change subscription: %w", err)
	}

	return sub, nil
}

func (r *Repository) renewSubscription(
	ctx context.Context,
	id uuid.UUID,
	remaining, amountMinor int64,
	payment string,
	now time.Time,
	key string,
	requestHash []byte,
) (domain.Subscription, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("begin renew subscription: %w", err)
	}

	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	queries := New(tx)
	if existing, ok, err := lockedPayment(ctx, queries, key, requestHash); err != nil || ok {
		return existing, err
	}

	current, err := queries.LockSubscription(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Subscription{}, domain.ErrNotFound
		}

		return domain.Subscription{}, fmt.Errorf("lock subscription: %w", err)
	}

	tariffRow, err := queries.FindTariff(ctx, current.TariffID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Subscription{}, domain.ErrNotFound
		}

		return domain.Subscription{}, fmt.Errorf("find tariff: %w", err)
	}

	tariff, err := tariffFrom(tariffRow.ID, tariffRow.Name, tariffRow.MonthlyPriceMinor, tariffRow.MessageLimit, tariffRow.Type, tariffRow.IsBaseTariff)
	if err != nil {
		return domain.Subscription{}, err
	}

	sub, err := subscriptionFrom(
		current.ID,
		current.UserID,
		current.OrganizationID,
		current.TariffID,
		current.Status,
		current.MessageAllowance,
		current.CurrentPeriodStart,
		current.CurrentPeriodEnd,
		paymentID(current.PaymentID),
	)
	if err != nil {
		return domain.Subscription{}, err
	}

	next, err := domain.PlanPayment(sub, tariff, remaining, amountMinor, now, payment)
	if err != nil {
		return domain.Subscription{}, err
	}

	if err := insertKey(ctx, queries, key, operationReceivePayment, requestHash, next.ID); err != nil {
		return domain.Subscription{}, err
	}

	if err := queries.RenewSubscription(ctx, RenewSubscriptionParams{
		ID:                 next.ID,
		MessageAllowance:   next.MessageAllowance,
		CurrentPeriodStart: next.PeriodStart,
		CurrentPeriodEnd:   next.PeriodEnd,
		PaymentID:          paymentText(next.PaymentID),
	}); err != nil {
		return domain.Subscription{}, fmt.Errorf("renew subscription: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Subscription{}, fmt.Errorf("commit renew subscription: %w", err)
	}

	return next, nil
}

func lockedPayment(ctx context.Context, queries *Queries, key string, requestHash []byte) (domain.Subscription, bool, error) {
	row, err := queries.FindIdempotencyForUpdate(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Subscription{}, false, nil
		}

		return domain.Subscription{}, false, fmt.Errorf("lock idempotency key: %w", err)
	}

	if row.Operation != operationReceivePayment || subtle.ConstantTimeCompare(row.RequestHash, requestHash) != 1 {
		return domain.Subscription{}, false, domain.ErrIdempotencyConflict
	}

	stored, err := queries.FindSubscription(ctx, row.ResourceID)
	if err != nil {
		return domain.Subscription{}, false, fmt.Errorf("find subscription: %w", err)
	}

	sub, err := subscriptionFrom(
		stored.ID,
		stored.UserID,
		stored.OrganizationID,
		stored.TariffID,
		stored.Status,
		stored.MessageAllowance,
		stored.CurrentPeriodStart,
		stored.CurrentPeriodEnd,
		paymentID(stored.PaymentID),
	)
	if err != nil {
		return domain.Subscription{}, false, err
	}

	return sub, true, nil
}

func lockedTariff(ctx context.Context, queries *Queries, key string, requestHash []byte) (domain.Tariff, bool, error) {
	resourceID, ok, err := lockedKey(ctx, queries, key, operationCreateTariff, requestHash)
	if err != nil || !ok {
		return domain.Tariff{}, false, err
	}

	row, err := queries.FindTariff(ctx, resourceID)
	if err != nil {
		return domain.Tariff{}, false, fmt.Errorf("find tariff: %w", err)
	}

	tariff, err := tariffFrom(row.ID, row.Name, row.MonthlyPriceMinor, row.MessageLimit, row.Type, row.IsBaseTariff)
	if err != nil {
		return domain.Tariff{}, false, err
	}

	return tariff, true, nil
}

func lockedSubscription(ctx context.Context, queries *Queries, key string, requestHash []byte) (domain.Subscription, bool, error) {
	row, err := queries.FindIdempotencyForUpdate(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Subscription{}, false, nil
		}

		return domain.Subscription{}, false, fmt.Errorf("lock idempotency key: %w", err)
	}

	if (row.Operation != operationCreateSubscription && row.Operation != operationChangeSubscription) ||
		subtle.ConstantTimeCompare(row.RequestHash, requestHash) != 1 {
		return domain.Subscription{}, false, domain.ErrIdempotencyConflict
	}

	stored, err := queries.FindSubscription(ctx, row.ResourceID)
	if err != nil {
		return domain.Subscription{}, false, fmt.Errorf("find subscription: %w", err)
	}

	sub, err := subscriptionFrom(
		stored.ID,
		stored.UserID,
		stored.OrganizationID,
		stored.TariffID,
		stored.Status,
		stored.MessageAllowance,
		stored.CurrentPeriodStart,
		stored.CurrentPeriodEnd,
		paymentID(stored.PaymentID),
	)
	if err != nil {
		return domain.Subscription{}, false, err
	}

	return sub, true, nil
}

func lockedKey(ctx context.Context, queries *Queries, key, operation string, requestHash []byte) (uuid.UUID, bool, error) {
	row, err := queries.FindIdempotencyForUpdate(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil(), false, nil
		}

		return uuid.Nil(), false, fmt.Errorf("lock idempotency key: %w", err)
	}

	if row.Operation != operation || subtle.ConstantTimeCompare(row.RequestHash, requestHash) != 1 {
		return uuid.Nil(), false, domain.ErrIdempotencyConflict
	}

	return row.ResourceID, true, nil
}

func insertKey(ctx context.Context, queries *Queries, key, operation string, requestHash []byte, resourceID uuid.UUID) error {
	err := queries.InsertIdempotency(ctx, InsertIdempotencyParams{
		Key:         key,
		Operation:   operation,
		RequestHash: requestHash,
		ResourceID:  resourceID,
	})
	if constraint(err) == constraintIdempotencyKeysPkey {
		return errIdempotencyRace
	}

	if err != nil {
		return fmt.Errorf("insert idempotency key: %w", err)
	}

	return nil
}

func baseTariff(ctx context.Context, queries *Queries) (*domain.Tariff, error) {
	row, err := queries.FindBaseTariff(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, fmt.Errorf("find base tariff: %w", err)
	}

	tariff, err := tariffFrom(row.ID, row.Name, row.MonthlyPriceMinor, row.MessageLimit, row.Type, row.IsBaseTariff)
	if err != nil {
		return nil, err
	}

	return &tariff, nil
}

func missingSubscription(err error) (domain.Subscription, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Subscription{}, domain.ErrNotFound
	}

	return domain.Subscription{}, fmt.Errorf("find subscription: %w", err)
}

func tariffFrom(id uuid.UUID, name string, price int64, limit int32, kind string, base bool) (domain.Tariff, error) {
	parsed := domain.TariffType(kind)
	if !parsed.Valid() {
		return domain.Tariff{}, fmt.Errorf("unknown tariff type %q", kind)
	}

	return domain.Tariff{
		ID:                id,
		Name:              name,
		MonthlyPriceMinor: price,
		MessageLimit:      int(limit),
		Type:              parsed,
		IsBase:            base,
	}, nil
}

func subscriptionFrom(
	id uuid.UUID,
	userID, organizationID *uuid.UUID,
	tariffID uuid.UUID,
	status string,
	allowance int64,
	periodStart, periodEnd time.Time,
	payment string,
) (domain.Subscription, error) {
	parsed := domain.Status(status)
	if !parsed.Valid() {
		return domain.Subscription{}, fmt.Errorf("unknown subscription status %q", status)
	}

	return domain.Subscription{
		ID:               id,
		UserID:           userID,
		OrganizationID:   organizationID,
		TariffID:         tariffID,
		Status:           parsed,
		MessageAllowance: allowance,
		PeriodStart:      periodStart,
		PeriodEnd:        periodEnd,
		PaymentID:        payment,
	}, nil
}

func paymentID(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}

	return value.String
}

func paymentText(id string) pgtype.Text {
	if id == "" {
		return pgtype.Text{}
	}

	return pgtype.Text{String: id, Valid: true}
}

func messageLimit(limit int) (int32, error) {
	if err := domain.ParseMessageLimit(limit); err != nil {
		return 0, err
	}

	return int32(limit), nil
}

func constraint(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return pgErr.ConstraintName
	}

	return ""
}
