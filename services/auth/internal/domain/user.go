package domain

import "uuid"

// User — учётная запись. Пароль хранится только как хеш.
type User struct {
	ID             uuid.UUID
	Email          string
	PasswordHash   string
	OrganizationID *uuid.UUID
	Role           Role
}

// Organization — организация, от имени которой можно выпустить JWT.
type Organization struct {
	ID   uuid.UUID
	Name string
}
