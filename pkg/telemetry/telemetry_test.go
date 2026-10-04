package telemetry_test

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/pkg/logger"
	"github.com/trb1maker/subscriptions/pkg/telemetry"
)

func TestStartWithoutExporter(t *testing.T) {
	t.Parallel()

	log, err := logger.New(io.Discard, "error")
	require.NoError(t, err)

	stop, err := telemetry.Start(context.Background(), log, "auth", "")
	require.NoError(t, err)
	require.NoError(t, stop(context.Background()))
}
