package app

import "context"

// Identity — проверенный через Auth субъект JWT.
type Identity struct {
	SubjectID string
	Kind      string
	Roles     []string
}

// Registered — результат регистрации.
type Registered struct {
	UserID string
	Token  string
}

// OrganizationCreated — результат создания организации.
type OrganizationCreated struct {
	OrganizationID string
	Token          string
}

// Auth — внутренние вызовы Auth.
type Auth interface {
	Register(ctx context.Context, email, password, idempotencyKey, organizationID string) (Registered, error)
	Login(ctx context.Context, email, password string) (string, error)
	CreateOrganization(ctx context.Context, name, idempotencyKey string) (OrganizationCreated, error)
	ValidateToken(ctx context.Context, token string) (Identity, error)
}
