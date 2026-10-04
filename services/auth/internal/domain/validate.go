package domain

import "strings"

const (
	minPasswordLen       = 8
	maxPasswordLen       = 72
	maxEmailLen          = 254
	maxNameLen           = 200
	maxIdempotencyKeyLen = 255
)

// ParseEmail приводит адрес к нижнему регистру и проверяет, что в нём есть локальная часть и домен.
func ParseEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	local, host, ok := strings.Cut(email, "@")
	if !ok || local == "" || host == "" || len(email) > maxEmailLen || strings.ContainsAny(email, " \t") {
		return "", ErrInvalidEmail
	}

	return email, nil
}

// ValidatePassword проверяет длину пароля в байтах: bcrypt не использует хвост длиннее 72 байт.
func ValidatePassword(password string) error {
	n := len(password)
	if n < minPasswordLen || n > maxPasswordLen {
		return ErrInvalidPassword
	}

	return nil
}

// ParseName убирает крайние пробелы и отвергает пустое имя.
func ParseName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || len(name) > maxNameLen {
		return "", ErrInvalidName
	}

	return name, nil
}

// ParseIdempotencyKey отвергает пустой и слишком длинный ключ.
func ParseIdempotencyKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" || len(key) > maxIdempotencyKeyLen {
		return "", ErrInvalidIdempotencyKey
	}

	return key, nil
}
