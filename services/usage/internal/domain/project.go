package domain

import "uuid"

// Project восстанавливает остаток: последняя установка минус уникальные списания после неё.
// Одинаковые события с одним идентификатором считаются один раз.
// Разные тела с одним идентификатором — конфликт.
func Project(events []Event) (int64, error) {
	unique, err := uniqueEvents(events)
	if err != nil {
		return 0, err
	}

	var baseline *Event
	for i := range unique {
		event := &unique[i]
		if !event.SetsAllowance() {
			continue
		}

		if baseline == nil || event.Later(*baseline) {
			baseline = event
		}
	}

	var balance int64
	if baseline != nil {
		balance = baseline.Allowance
	}

	for _, event := range unique {
		if !event.Decrements() {
			continue
		}

		if baseline == nil || event.Later(*baseline) {
			balance--
		}
	}

	return balance, nil
}

func uniqueEvents(events []Event) ([]Event, error) {
	byID := make(map[uuid.UUID]Event, len(events))
	order := make([]uuid.UUID, 0, len(events))
	for _, event := range events {
		event = event.Normalized()
		previous, found := byID[event.ID]
		if !found {
			byID[event.ID] = event
			order = append(order, event.ID)

			continue
		}

		if !previous.Same(event) {
			return nil, ErrIdempotencyConflict
		}
	}

	unique := make([]Event, 0, len(order))
	for _, id := range order {
		unique = append(unique, byID[id])
	}

	return unique, nil
}
