package app_test

import (
	"context"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/services/gateway/internal/app"
	"github.com/trb1maker/subscriptions/services/gateway/internal/domain"
)

func TestGeneratePublishesProcessedEvent(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	when := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	subs := &fakeSubscriptions{active: true}
	usage := &fakeUsage{status: app.LimitStatus{Allowed: true, Remaining: 2, OwnerID: ownerID, OwnerKind: "user"}}
	published := &fakePublisher{}
	emulator := &fakeEmulator{outcome: app.Outcome{Tokens: 9, Text: app.EmulatedText}}
	svc := newGenerate(t, subs, usage, published, emulator, when)
	key := uuid.New()

	result, err := svc.Generate(context.Background(), "  hello  ", key.String())
	require.NoError(t, err)
	require.Equal(t, app.Generated{Status: "processed", Text: app.EmulatedText, Tokens: 9}, result)
	require.Equal(t, 1, subs.calls)
	require.Equal(t, 1, usage.calls)
	require.Equal(t, 1, emulator.calls)
	require.Equal(t, []app.MessageEvent{{
		ID: key, OccurredAt: when, OwnerID: ownerID, OwnerKind: "user", Tokens: 9,
	}}, published.events)

	again, err := svc.Generate(context.Background(), "hello", key.String())
	require.NoError(t, err)
	require.Equal(t, result, again)
	require.Equal(t, 1, subs.calls)
	require.Equal(t, 1, usage.calls)
	require.Equal(t, 1, emulator.calls)
	require.Len(t, published.events, 1)

	_, err = svc.Generate(context.Background(), "other", key.String())
	require.ErrorIs(t, err, domain.ErrConflict)
	require.Len(t, published.events, 1)
}

func TestGeneratePublishesFailureWithoutText(t *testing.T) {
	t.Parallel()

	ownerID := uuid.New()
	published := &fakePublisher{}
	emulator := &fakeEmulator{outcome: app.Outcome{Failed: true, Tokens: 4}}
	svc := newGenerate(t,
		&fakeSubscriptions{active: true},
		&fakeUsage{status: app.LimitStatus{Allowed: true, Remaining: 1, OwnerID: ownerID, OwnerKind: "organization"}},
		published,
		emulator,
		time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC),
	)

	result, err := svc.Generate(context.Background(), "prompt", uuid.New().String())
	require.NoError(t, err)
	require.Equal(t, "failed", result.Status)
	require.Empty(t, result.Text)
	require.Equal(t, int64(4), result.Tokens)
	require.True(t, published.events[0].Failed)
	require.Equal(t, "organization", published.events[0].OwnerKind)
	require.Equal(t, ownerID, published.events[0].OwnerID)
}

func TestGenerateStopsBeforeEmulation(t *testing.T) {
	t.Parallel()

	published := &fakePublisher{}
	emulator := &fakeEmulator{}
	usage := &fakeUsage{}
	subs := &fakeSubscriptions{}
	svc := newGenerate(t, subs, usage, published, emulator, time.Now())

	_, err := svc.Generate(context.Background(), "prompt", uuid.New().String())
	require.ErrorIs(t, err, domain.ErrSubscriptionInactive)
	require.Equal(t, 1, subs.calls)
	require.Zero(t, usage.calls)
	require.Zero(t, emulator.calls)
	require.Empty(t, published.events)

	subs.active = true
	_, err = svc.Generate(context.Background(), "prompt", uuid.New().String())
	require.ErrorIs(t, err, domain.ErrLimitExceeded)
	require.Equal(t, 1, usage.calls)
	require.Zero(t, emulator.calls)
	require.Empty(t, published.events)
}

func TestGenerateRejectsEmptyOwner(t *testing.T) {
	t.Parallel()

	published := &fakePublisher{}
	emulator := &fakeEmulator{}
	svc := newGenerate(t,
		&fakeSubscriptions{active: true},
		&fakeUsage{status: app.LimitStatus{Allowed: true, Remaining: 1}},
		published,
		emulator,
		time.Now(),
	)

	_, err := svc.Generate(context.Background(), "prompt", uuid.New().String())
	require.ErrorIs(t, err, domain.ErrInternal)
	require.Zero(t, emulator.calls)
	require.Empty(t, published.events)
}

func TestGenerateRetriesPublishWithSameEvent(t *testing.T) {
	t.Parallel()

	when := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	ownerID := uuid.New()
	published := &fakePublisher{err: domain.ErrUnavailable}
	emulator := &fakeEmulator{outcome: app.Outcome{Failed: true, Tokens: 4}}
	subs := &fakeSubscriptions{active: true}
	usage := &fakeUsage{status: app.LimitStatus{Allowed: true, Remaining: 1, OwnerID: ownerID, OwnerKind: "user"}}
	svc := newGenerate(t, subs, usage, published, emulator, when)
	key := uuid.New()

	_, err := svc.Generate(context.Background(), "prompt", key.String())
	require.ErrorIs(t, err, domain.ErrUnavailable)
	require.Equal(t, 1, emulator.calls)
	require.Empty(t, published.events)

	published.err = nil
	emulator.outcome = app.Outcome{Tokens: 99, Text: app.EmulatedText}
	result, err := svc.Generate(context.Background(), "prompt", key.String())
	require.NoError(t, err)
	require.Equal(t, app.Generated{Status: "failed", Tokens: 4}, result)
	require.Equal(t, 1, emulator.calls)
	require.Equal(t, 1, subs.calls)
	require.Equal(t, 1, usage.calls)
	require.Equal(t, []app.MessageEvent{{
		ID: key, OccurredAt: when, OwnerID: ownerID, OwnerKind: "user", Failed: true, Tokens: 4,
	}}, published.events)

	again, err := svc.Generate(context.Background(), "prompt", key.String())
	require.NoError(t, err)
	require.Equal(t, result, again)
	require.Len(t, published.events, 1)

	_, err = svc.Generate(context.Background(), "other", key.String())
	require.ErrorIs(t, err, domain.ErrConflict)
	require.Len(t, published.events, 1)
}

func TestGenerateCanceledEmulationDoesNotPublish(t *testing.T) {
	t.Parallel()

	published := &fakePublisher{}
	svc := newGenerate(t,
		&fakeSubscriptions{active: true},
		&fakeUsage{status: app.LimitStatus{Allowed: true, Remaining: 1, OwnerID: uuid.New(), OwnerKind: "user"}},
		published,
		&fakeEmulator{err: context.Canceled},
		time.Now(),
	)

	_, err := svc.Generate(context.Background(), "prompt", uuid.New().String())
	require.ErrorIs(t, err, domain.ErrUnavailable)
	require.Empty(t, published.events)
}

func TestGenerateRejectsBadInput(t *testing.T) {
	t.Parallel()

	svc := newGenerate(t, &fakeSubscriptions{}, &fakeUsage{}, &fakePublisher{}, &fakeEmulator{}, time.Now())

	_, err := svc.Generate(context.Background(), "prompt", "not-a-uuid")
	require.ErrorIs(t, err, domain.ErrInvalidArgument)
	_, err = svc.Generate(context.Background(), "   ", uuid.New().String())
	require.ErrorIs(t, err, domain.ErrInvalidArgument)
	_, err = svc.Generate(context.Background(), "prompt", uuid.Nil().String())
	require.ErrorIs(t, err, domain.ErrInvalidArgument)
}

func TestGenerateConcurrentReplayPublishesOnce(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	emulator := &fakeEmulator{block: func(ctx context.Context) error {
		once.Do(func() { close(entered) })
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}, outcome: app.Outcome{Tokens: 3, Text: app.EmulatedText}}
	published := &fakePublisher{}
	svc := newGenerate(t,
		&fakeSubscriptions{active: true},
		&fakeUsage{status: app.LimitStatus{Allowed: true, Remaining: 1, OwnerID: uuid.New(), OwnerKind: "user"}},
		published,
		emulator,
		time.Now().UTC(),
	)
	key := uuid.New().String()

	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() {
		_, err := svc.Generate(context.Background(), "prompt", key)
		first <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("emulator did not start")
	}
	go func() {
		_, err := svc.Generate(context.Background(), "prompt", key)
		second <- err
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)

	require.NoError(t, <-first)
	require.NoError(t, <-second)
	require.Equal(t, 1, emulator.calls)
	require.Len(t, published.events, 1)
}

func newGenerate(
	t *testing.T,
	subs *fakeSubscriptions,
	usage *fakeUsage,
	published *fakePublisher,
	emulator *fakeEmulator,
	now time.Time,
) *app.Service {
	t.Helper()

	svc, err := app.New(subs, usage, published, emulator, func() time.Time { return now })
	require.NoError(t, err)

	return svc
}

type fakeSubscriptions struct {
	active bool
	err    error
	calls  int
}

func (f *fakeSubscriptions) CheckSubscription(context.Context) (app.SubscriptionStatus, error) {
	f.calls++

	return app.SubscriptionStatus{Active: f.active}, f.err
}

func (f *fakeSubscriptions) CreateTariff(context.Context, string, string, int64, int32, string, bool) (app.Tariff, error) {
	return app.Tariff{}, nil
}

func (f *fakeSubscriptions) ListTariffs(context.Context) ([]app.Tariff, error) {
	return nil, nil
}

func (f *fakeSubscriptions) CreateSubscription(context.Context, string, string) (app.Subscription, error) {
	return app.Subscription{}, nil
}

func (f *fakeSubscriptions) ChangeSubscription(context.Context, string, string, string) (app.Subscription, error) {
	return app.Subscription{}, nil
}

func (f *fakeSubscriptions) GetSubscription(context.Context, string) (app.Subscription, error) {
	return app.Subscription{}, nil
}

func (f *fakeSubscriptions) ProcessPayment(context.Context, string, string, int64, time.Time) error {
	return nil
}

type fakeUsage struct {
	status app.LimitStatus
	err    error
	calls  int
}

func (f *fakeUsage) CheckLimit(context.Context) (app.LimitStatus, error) {
	f.calls++

	return f.status, f.err
}

type fakePublisher struct {
	events []app.MessageEvent
	err    error
}

func (f *fakePublisher) Publish(_ context.Context, event app.MessageEvent) error {
	if f.err != nil {
		return f.err
	}

	f.events = append(f.events, event)

	return nil
}

type fakeEmulator struct {
	outcome app.Outcome
	err     error
	calls   int
	block   func(context.Context) error
	mu      sync.Mutex
}

func (f *fakeEmulator) Emulate(ctx context.Context) (app.Outcome, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()

	if f.block != nil {
		if err := f.block(ctx); err != nil {
			return app.Outcome{}, err
		}
	}

	return f.outcome, f.err
}
