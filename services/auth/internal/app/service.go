package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"
	"uuid"

	"github.com/trb1maker/subscriptions/services/auth/internal/domain"
)

// Store хранит пользователей и организации и обеспечивает идемпотентность команд.
type Store interface {
	// ReplayUser возвращает пользователя, уже созданного этим ключом.
	// ErrNotFound — ключа нет. ErrIdempotencyConflict — ключ занят другим запросом.
	ReplayUser(ctx context.Context, key string, requestHash []byte) (domain.User, error)
	RegisterUser(ctx context.Context, user domain.User, key string, requestHash []byte) (domain.User, error)
	CreateOrganization(ctx context.Context, org domain.Organization, key string, requestHash []byte) (domain.Organization, error)
	UserByEmail(ctx context.Context, email string) (domain.User, error)
	UserByID(ctx context.Context, id uuid.UUID) (domain.User, error)
	OrganizationByID(ctx context.Context, id uuid.UUID) (domain.Organization, error)
}

// Passwords хеширует пароль и сверяет его с хешем.
type Passwords interface {
	Hash(password string) (string, error)
	Match(hash, password string) error
	// Burn тратит столько же времени, сколько Match, и всегда возвращает ErrInvalidCredentials.
	Burn(password string) error
}

// Identity — проверенные claims JWT.
type Identity struct {
	SubjectID uuid.UUID
	Type      domain.PrincipalType
	Roles     []domain.Role
}

// Tokens выпускает и проверяет JWT.
type Tokens interface {
	Issue(subject uuid.UUID, kind domain.PrincipalType, roles []domain.Role, now time.Time) (string, error)
	Parse(token string, now time.Time) (Identity, error)
}

// Registered — результат регистрации.
type Registered struct {
	UserID uuid.UUID
	Token  string
}

// OrganizationCreated — результат создания организации.
type OrganizationCreated struct {
	OrganizationID uuid.UUID
	Token          string
}

// Service — сценарии аутентификации.
type Service struct {
	store     Store
	passwords Passwords
	tokens    Tokens
	pepper    []byte
	now       func() time.Time
}

// New собирает сценарии. pepper — секрет HMAC для отпечатка запроса, now может быть nil.
func New(store Store, passwords Passwords, tokens Tokens, pepper []byte, now func() time.Time) (*Service, error) {
	if len(pepper) == 0 {
		return nil, errors.New("empty request pepper")
	}

	if now == nil {
		now = time.Now
	}

	return &Service{
		store:     store,
		passwords: passwords,
		tokens:    tokens,
		pepper:    append([]byte(nil), pepper...),
		now:       now,
	}, nil
}

func (s *Service) clock() time.Time {
	return s.now()
}

func (s *Service) requestHash(parts ...string) []byte {
	mac := hmac.New(sha256.New, s.pepper)
	for _, part := range parts {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(part)))
		_, _ = mac.Write(n[:])
		_, _ = mac.Write([]byte(part))
	}

	return mac.Sum(nil)
}
