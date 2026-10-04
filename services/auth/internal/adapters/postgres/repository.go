package postgres

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/trb1maker/subscriptions/services/auth/internal/domain"
)

const (
	operationRegister             = "register"
	operationCreateOrganization   = "create_organization"
	constraintUsersEmail          = "users_email_unique"
	constraintIdempotencyKeysPkey = "idempotency_keys_pkey"
	pgUniqueViolation             = "23505"
)

var errIdempotencyRace = errors.New("idempotency race")

// Repository хранит пользователей и организации в Postgres.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository собирает доступ к базе.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// RegisterUser создаёт пользователя или возвращает уже созданного по ключу идемпотентности.
func (r *Repository) RegisterUser(ctx context.Context, user domain.User, key string, requestHash []byte) (domain.User, error) {
	stored, err := r.insertUser(ctx, user, key, requestHash)
	if errors.Is(err, errIdempotencyRace) {
		return r.replayUser(ctx, key, requestHash)
	}

	if err != nil {
		return domain.User{}, err
	}

	return stored, nil
}

// CreateOrganization создаёт организацию или возвращает уже созданную по ключу идемпотентности.
func (r *Repository) CreateOrganization(ctx context.Context, org domain.Organization, key string, requestHash []byte) (domain.Organization, error) {
	stored, err := r.insertOrganization(ctx, org, key, requestHash)
	if errors.Is(err, errIdempotencyRace) {
		return r.replayOrganization(ctx, key, requestHash)
	}

	if err != nil {
		return domain.Organization{}, err
	}

	return stored, nil
}

// UserByID возвращает пользователя по идентификатору.
func (r *Repository) UserByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	row, err := New(r.pool).FindUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, domain.ErrNotFound
		}

		return domain.User{}, fmt.Errorf("find user: %w", err)
	}

	return userFromRow(row.ID, row.Email, row.PasswordHash, row.OrganizationID, row.Role)
}

// OrganizationByID возвращает организацию по идентификатору.
func (r *Repository) OrganizationByID(ctx context.Context, id uuid.UUID) (domain.Organization, error) {
	row, err := New(r.pool).FindOrganization(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Organization{}, domain.ErrNotFound
		}

		return domain.Organization{}, fmt.Errorf("find organization: %w", err)
	}

	return domain.Organization{ID: row.ID, Name: row.Name}, nil
}

// UserByEmail ищет пользователя по нормализованному адресу.
func (r *Repository) UserByEmail(ctx context.Context, email string) (domain.User, error) {
	row, err := New(r.pool).FindUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, domain.ErrNotFound
		}

		return domain.User{}, fmt.Errorf("find user: %w", err)
	}

	return userFromRow(row.ID, row.Email, row.PasswordHash, row.OrganizationID, row.Role)
}

// ReplayUser возвращает пользователя, уже записанного этим ключом, без новой вставки.
func (r *Repository) ReplayUser(ctx context.Context, key string, requestHash []byte) (domain.User, error) {
	queries := New(r.pool)
	row, err := queries.FindIdempotency(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, domain.ErrNotFound
		}

		return domain.User{}, fmt.Errorf("find idempotency key: %w", err)
	}

	if row.Operation != operationRegister || subtle.ConstantTimeCompare(row.RequestHash, requestHash) != 1 {
		return domain.User{}, domain.ErrIdempotencyConflict
	}

	stored, err := queries.FindUserByID(ctx, row.ResourceID)
	if err != nil {
		return domain.User{}, fmt.Errorf("find user: %w", err)
	}

	return userFromRow(stored.ID, stored.Email, stored.PasswordHash, stored.OrganizationID, stored.Role)
}

func (r *Repository) insertUser(ctx context.Context, user domain.User, key string, requestHash []byte) (domain.User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, fmt.Errorf("begin register: %w", err)
	}

	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	queries := New(tx)
	if existing, ok, err := lockedUser(ctx, queries, key, requestHash); err != nil || ok {
		return existing, err
	}

	if user.OrganizationID != nil {
		if _, err := queries.FindOrganization(ctx, *user.OrganizationID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.User{}, domain.ErrOrganizationNotFound
			}

			return domain.User{}, fmt.Errorf("find organization: %w", err)
		}
	}

	if err := queries.InsertIdempotency(ctx, InsertIdempotencyParams{
		Key:         key,
		Operation:   operationRegister,
		RequestHash: requestHash,
		ResourceID:  user.ID,
	}); err != nil {
		if constraint(err) == constraintIdempotencyKeysPkey {
			return domain.User{}, errIdempotencyRace
		}

		return domain.User{}, fmt.Errorf("insert idempotency key: %w", err)
	}

	if err := queries.InsertUser(ctx, InsertUserParams{
		ID:             user.ID,
		Email:          user.Email,
		PasswordHash:   user.PasswordHash,
		OrganizationID: user.OrganizationID,
		Role:           string(user.Role),
	}); err != nil {
		if constraint(err) == constraintUsersEmail {
			return domain.User{}, domain.ErrEmailTaken
		}

		return domain.User{}, fmt.Errorf("insert user: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, fmt.Errorf("commit register: %w", err)
	}

	return user, nil
}

func (r *Repository) replayUser(ctx context.Context, key string, requestHash []byte) (domain.User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, fmt.Errorf("begin register replay: %w", err)
	}

	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	user, ok, err := lockedUser(ctx, New(tx), key, requestHash)
	if err != nil {
		return domain.User{}, err
	}

	if !ok {
		return domain.User{}, domain.ErrIdempotencyConflict
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, fmt.Errorf("commit register replay: %w", err)
	}

	return user, nil
}

func lockedUser(ctx context.Context, queries *Queries, key string, requestHash []byte) (domain.User, bool, error) {
	row, err := queries.FindIdempotencyForUpdate(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, false, nil
		}

		return domain.User{}, false, fmt.Errorf("lock idempotency key: %w", err)
	}

	if row.Operation != operationRegister || subtle.ConstantTimeCompare(row.RequestHash, requestHash) != 1 {
		return domain.User{}, false, domain.ErrIdempotencyConflict
	}

	stored, err := queries.FindUserByID(ctx, row.ResourceID)
	if err != nil {
		return domain.User{}, false, fmt.Errorf("find user: %w", err)
	}

	user, err := userFromRow(stored.ID, stored.Email, stored.PasswordHash, stored.OrganizationID, stored.Role)
	if err != nil {
		return domain.User{}, false, err
	}

	return user, true, nil
}

func (r *Repository) insertOrganization(ctx context.Context, org domain.Organization, key string, requestHash []byte) (domain.Organization, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Organization{}, fmt.Errorf("begin create organization: %w", err)
	}

	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	queries := New(tx)
	if existing, ok, err := lockedOrganization(ctx, queries, key, requestHash); err != nil || ok {
		return existing, err
	}

	if err := queries.InsertIdempotency(ctx, InsertIdempotencyParams{
		Key:         key,
		Operation:   operationCreateOrganization,
		RequestHash: requestHash,
		ResourceID:  org.ID,
	}); err != nil {
		if constraint(err) == constraintIdempotencyKeysPkey {
			return domain.Organization{}, errIdempotencyRace
		}

		return domain.Organization{}, fmt.Errorf("insert idempotency key: %w", err)
	}

	if err := queries.InsertOrganization(ctx, InsertOrganizationParams{ID: org.ID, Name: org.Name}); err != nil {
		return domain.Organization{}, fmt.Errorf("insert organization: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Organization{}, fmt.Errorf("commit create organization: %w", err)
	}

	return org, nil
}

func (r *Repository) replayOrganization(ctx context.Context, key string, requestHash []byte) (domain.Organization, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Organization{}, fmt.Errorf("begin organization replay: %w", err)
	}

	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	org, ok, err := lockedOrganization(ctx, New(tx), key, requestHash)
	if err != nil {
		return domain.Organization{}, err
	}

	if !ok {
		return domain.Organization{}, domain.ErrIdempotencyConflict
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Organization{}, fmt.Errorf("commit organization replay: %w", err)
	}

	return org, nil
}

func lockedOrganization(ctx context.Context, queries *Queries, key string, requestHash []byte) (domain.Organization, bool, error) {
	row, err := queries.FindIdempotencyForUpdate(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Organization{}, false, nil
		}

		return domain.Organization{}, false, fmt.Errorf("lock idempotency key: %w", err)
	}

	if row.Operation != operationCreateOrganization || subtle.ConstantTimeCompare(row.RequestHash, requestHash) != 1 {
		return domain.Organization{}, false, domain.ErrIdempotencyConflict
	}

	stored, err := queries.FindOrganization(ctx, row.ResourceID)
	if err != nil {
		return domain.Organization{}, false, fmt.Errorf("find organization: %w", err)
	}

	return domain.Organization{ID: stored.ID, Name: stored.Name}, true, nil
}

func userFromRow(id uuid.UUID, email, passwordHash string, organizationID *uuid.UUID, role string) (domain.User, error) {
	parsed := domain.Role(role)
	if !parsed.Valid() {
		return domain.User{}, fmt.Errorf("unknown user role %q", role)
	}

	return domain.User{
		ID:             id,
		Email:          email,
		PasswordHash:   passwordHash,
		OrganizationID: organizationID,
		Role:           parsed,
	}, nil
}

func constraint(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return pgErr.ConstraintName
	}

	return ""
}
