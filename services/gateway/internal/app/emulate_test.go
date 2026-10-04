package app_test

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/services/gateway/internal/app"
)

func TestEmulatorBounds(t *testing.T) {
	t.Parallel()

	success, err := app.NewEmulator(0, 0, 0, rand.New(rand.NewPCG(1, 2)))
	require.NoError(t, err)
	outcome, err := success.Emulate(context.Background())
	require.NoError(t, err)
	require.False(t, outcome.Failed)
	require.Equal(t, app.EmulatedText, outcome.Text)
	require.GreaterOrEqual(t, outcome.Tokens, int64(1))
	require.LessOrEqual(t, outcome.Tokens, int64(128))

	failed, err := app.NewEmulator(0, 0, 100, rand.New(rand.NewPCG(1, 2)))
	require.NoError(t, err)
	outcome, err = failed.Emulate(context.Background())
	require.NoError(t, err)
	require.True(t, outcome.Failed)
	require.Empty(t, outcome.Text)
	require.Positive(t, outcome.Tokens)

	_, err = app.NewEmulator(time.Second, 0, 5, nil)
	require.Error(t, err)
	_, err = app.NewEmulator(0, time.Second, 101, nil)
	require.Error(t, err)
}

func TestEmulatorStopsWhenContextCanceled(t *testing.T) {
	t.Parallel()

	emulator, err := app.NewEmulator(time.Hour, time.Hour, 0, rand.New(rand.NewPCG(1, 2)))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = emulator.Emulate(ctx)
	require.ErrorIs(t, err, context.Canceled)
}
