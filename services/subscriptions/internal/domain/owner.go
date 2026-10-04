package domain

import "uuid"

// OwnerKind — владелец подписки: пользователь или организация.
type OwnerKind string

const (
	OwnerUser         OwnerKind = "user"
	OwnerOrganization OwnerKind = "organization"
)

// Valid сообщает, что вид владельца известен домену.
func (k OwnerKind) Valid() bool {
	return k == OwnerUser || k == OwnerOrganization
}

// RoleAdmin — роль, которой разрешено создавать тарифы.
const RoleAdmin = "admin"

// Owner — вызывающий, уже извлечённый из metadata.
type Owner struct {
	ID    uuid.UUID
	Kind  OwnerKind
	Roles []string
}

// Admin сообщает, что среди ролей есть администратор.
func (o Owner) Admin() bool {
	for _, role := range o.Roles {
		if role == RoleAdmin {
			return true
		}
	}

	return false
}

// Subject — сведения Auth о пользователе или организации.
type Subject struct {
	OrganizationID *uuid.UUID
}
