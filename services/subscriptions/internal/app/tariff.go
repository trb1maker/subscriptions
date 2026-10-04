package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"uuid"

	"github.com/trb1maker/subscriptions/services/subscriptions/internal/domain"
)

// CreateTariff создаёт тариф. Повтор ключа с тем же телом возвращает прежний тариф.
func (s *Service) CreateTariff(
	ctx context.Context,
	owner domain.Owner,
	name string,
	price int64,
	limit int,
	kind string,
	base bool,
	idempotencyKey string,
) (domain.Tariff, error) {
	if err := requireOwner(owner); err != nil {
		return domain.Tariff{}, err
	}

	if !owner.Admin() {
		return domain.Tariff{}, domain.ErrForbidden
	}

	key, err := domain.ParseIdempotencyKey(idempotencyKey)
	if err != nil {
		return domain.Tariff{}, err
	}

	name, err = domain.ParseName(name)
	if err != nil {
		return domain.Tariff{}, err
	}

	if err := domain.ParsePrice(price); err != nil {
		return domain.Tariff{}, err
	}

	if err := domain.ParseMessageLimit(limit); err != nil {
		return domain.Tariff{}, err
	}

	tariffType, err := domain.ParseTariffType(kind)
	if err != nil {
		return domain.Tariff{}, err
	}

	if base && tariffType != domain.TariffB2C {
		return domain.Tariff{}, domain.ErrTariffType
	}

	digest := s.requestHash(
		"create_tariff",
		name,
		strconv.FormatInt(price, 10),
		strconv.Itoa(limit),
		string(tariffType),
		strconv.FormatBool(base),
	)
	tariff, err := s.store.ReplayTariff(ctx, key, digest)
	if errors.Is(err, domain.ErrNotFound) {
		tariff, err = s.store.CreateTariff(ctx, domain.Tariff{
			ID:                uuid.New(),
			Name:              name,
			MonthlyPriceMinor: price,
			MessageLimit:      limit,
			Type:              tariffType,
			IsBase:            base,
		}, key, digest)
	}

	if err != nil {
		return domain.Tariff{}, fmt.Errorf("create tariff: %w", err)
	}

	return tariff, nil
}

// ListTariffs возвращает каталог.
func (s *Service) ListTariffs(ctx context.Context, owner domain.Owner) ([]domain.Tariff, error) {
	if err := requireOwner(owner); err != nil {
		return nil, err
	}

	tariffs, err := s.store.ListTariffs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tariffs: %w", err)
	}

	return tariffs, nil
}
