package domain

// PrincipalType — субъект JWT: пользователь или организация.
type PrincipalType string

const (
	PrincipalUser         PrincipalType = "user"
	PrincipalOrganization PrincipalType = "organization"
)

// Valid сообщает, что тип входит в контракт токена.
func (t PrincipalType) Valid() bool {
	return t == PrincipalUser || t == PrincipalOrganization
}

// Role — роль в claim roles.
type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

// Valid сообщает, что роль известна домену.
func (r Role) Valid() bool {
	return r == RoleUser || r == RoleAdmin
}
