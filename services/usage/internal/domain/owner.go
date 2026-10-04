package domain

import "uuid"

// OwnerKind — владелец лимита: пользователь или организация.
type OwnerKind string

const (
	OwnerUser         OwnerKind = "user"
	OwnerOrganization OwnerKind = "organization"
)

// Valid сообщает, что вид владельца известен домену.
func (k OwnerKind) Valid() bool {
	return k == OwnerUser || k == OwnerOrganization
}

// Owner — тот, на чьём ключе лежит остаток сообщений.
type Owner struct {
	ID   uuid.UUID
	Kind OwnerKind
}

// LimitKey — ключ проекции остатка в Redis.
func (o Owner) LimitKey() string {
	return "limit:" + o.scope() + ":" + o.ID.String()
}

// SeenKey — множество уже применённых событий владельца.
func (o Owner) SeenKey() string {
	return "seen:" + o.scope() + ":" + o.ID.String()
}

func (o Owner) scope() string {
	if o.Kind == OwnerOrganization {
		return "org"
	}

	return "user"
}

// Subject — сведения Auth о пользователе или организации.
type Subject struct {
	OrganizationID *uuid.UUID
}
