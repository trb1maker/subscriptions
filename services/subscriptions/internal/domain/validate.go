package domain

import "strings"

const (
	maxNameLen           = 200
	maxIdempotencyKeyLen = 255
	maxMessageLimit      = 1_000_000_000
)

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

// ParsePrice отвергает отрицательную цену в минимальных единицах.
func ParsePrice(minor int64) error {
	if minor < 0 {
		return ErrInvalidPrice
	}

	return nil
}

// ParseMessageLimit отвергает нулевой и слишком большой лимит сообщений.
func ParseMessageLimit(limit int) error {
	if limit < 1 || limit > maxMessageLimit {
		return ErrInvalidMessageLimit
	}

	return nil
}

// ParseTariffType принимает только b2c и b2b.
func ParseTariffType(raw string) (TariffType, error) {
	kind := TariffType(strings.TrimSpace(raw))
	if !kind.Valid() {
		return "", ErrInvalidTariffType
	}

	return kind, nil
}
