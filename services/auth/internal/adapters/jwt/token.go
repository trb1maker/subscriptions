package jwt

import (
	"errors"
	"fmt"
	"time"
	"uuid"

	jwtv5 "github.com/golang-jwt/jwt/v5"

	"github.com/trb1maker/subscriptions/services/auth/internal/app"
	"github.com/trb1maker/subscriptions/services/auth/internal/domain"
)

// Issuer подписывает JWT алгоритмом HS256.
type Issuer struct {
	secret []byte
	ttl    time.Duration
}

type claims struct {
	jwtv5.RegisteredClaims

	Type  string   `json:"type"`
	Roles []string `json:"roles"`
}

// NewIssuer проверяет секрет и срок жизни токена.
func NewIssuer(secret string, ttl time.Duration) (*Issuer, error) {
	if secret == "" {
		return nil, errors.New("empty jwt secret")
	}

	if ttl <= 0 {
		return nil, errors.New("non-positive jwt ttl")
	}

	return &Issuer{secret: []byte(secret), ttl: ttl}, nil
}

// Issue собирает claims и подписывает их.
func (i *Issuer) Issue(subject uuid.UUID, kind domain.PrincipalType, roles []domain.Role, now time.Time) (string, error) {
	if !kind.Valid() || len(roles) == 0 {
		return "", domain.ErrInvalidToken
	}

	names := make([]string, 0, len(roles))
	for _, role := range roles {
		if !role.Valid() {
			return "", domain.ErrInvalidToken
		}

		names = append(names, string(role))
	}

	token := jwtv5.NewWithClaims(jwtv5.SigningMethodHS256, claims{
		RegisteredClaims: jwtv5.RegisteredClaims{
			Subject:   subject.String(),
			IssuedAt:  jwtv5.NewNumericDate(now),
			ExpiresAt: jwtv5.NewNumericDate(now.Add(i.ttl)),
		},
		Type:  string(kind),
		Roles: names,
	})

	signed, err := token.SignedString(i.secret)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}

	return signed, nil
}

// Parse проверяет подпись, срок и состав claims.
func (i *Issuer) Parse(token string, now time.Time) (app.Identity, error) {
	parsed, err := jwtv5.ParseWithClaims(token, &claims{}, func(token *jwtv5.Token) (any, error) {
		if token.Method != jwtv5.SigningMethodHS256 {
			return nil, domain.ErrInvalidToken
		}

		return i.secret, nil
	}, jwtv5.WithValidMethods([]string{jwtv5.SigningMethodHS256.Alg()}),
		jwtv5.WithExpirationRequired(),
		jwtv5.WithLeeway(0),
		jwtv5.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil || !parsed.Valid {
		return app.Identity{}, domain.ErrInvalidToken
	}

	body, ok := parsed.Claims.(*claims)
	if !ok {
		return app.Identity{}, domain.ErrInvalidToken
	}

	subject, err := uuid.Parse(body.Subject)
	if err != nil {
		return app.Identity{}, domain.ErrInvalidToken
	}

	kind := domain.PrincipalType(body.Type)
	if !kind.Valid() || len(body.Roles) == 0 {
		return app.Identity{}, domain.ErrInvalidToken
	}

	roles := make([]domain.Role, 0, len(body.Roles))
	for _, role := range body.Roles {
		item := domain.Role(role)
		if !item.Valid() {
			return app.Identity{}, domain.ErrInvalidToken
		}

		roles = append(roles, item)
	}

	return app.Identity{SubjectID: subject, Type: kind, Roles: roles}, nil
}
