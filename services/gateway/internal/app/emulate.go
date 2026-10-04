package app

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"
)

const (
	tokenChoices = 128
	percentBase  = 100
	// EmulatedText — текст успешной эмуляции. В событие не входит.
	EmulatedText = "emulated response"
)

// Outcome — результат эмуляции. Text заполнен только при успехе.
type Outcome struct {
	Failed bool
	Tokens int64
	Text   string
}

// Emulator имитирует ответ модели.
type Emulator interface {
	Emulate(ctx context.Context) (Outcome, error)
}

// RandomEmulator ждёт случайную задержку и с заданной вероятностью возвращает ошибку генерации.
type RandomEmulator struct {
	min            time.Duration
	max            time.Duration
	failurePercent int
	mu             sync.Mutex
	rng            *rand.Rand
	sleep          func(context.Context, time.Duration) error
}

// NewEmulator собирает эмулятор. rng == nil включает новый генератор.
// failurePercent — доля неуспеха в процентах от 0 до 100. Задержка выбирается от min до max включительно.
func NewEmulator(minDelay, maxDelay time.Duration, failurePercent int, rng *rand.Rand) (*RandomEmulator, error) {
	if minDelay < 0 || maxDelay < minDelay || failurePercent < 0 || failurePercent > percentBase {
		return nil, errors.New("invalid emulator")
	}

	if rng == nil {
		rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}

	return &RandomEmulator{
		min:            minDelay,
		max:            maxDelay,
		failurePercent: failurePercent,
		rng:            rng,
		sleep:          sleep,
	}, nil
}

// Emulate спит, затем выбирает исход и число токенов от 1 до 128.
func (e *RandomEmulator) Emulate(ctx context.Context) (Outcome, error) {
	delay, failed, tokens := e.roll()
	if err := e.sleep(ctx, delay); err != nil {
		return Outcome{}, err
	}

	outcome := Outcome{Failed: failed, Tokens: tokens}
	if !failed {
		outcome.Text = EmulatedText
	}

	return outcome, nil
}

func (e *RandomEmulator) roll() (time.Duration, bool, int64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	delay := e.min
	if e.max > e.min {
		span := int64(e.max - e.min)
		delay += time.Duration(e.rng.Int64N(span + 1))
	}

	failed := e.rng.IntN(percentBase) < e.failurePercent
	tokens := int64(e.rng.IntN(tokenChoices)) + 1

	return delay, failed, tokens
}

func sleep(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("wait: %w", err)
	}

	if delay == 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return fmt.Errorf("wait: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}
