package logger_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/pkg/logger"
)

func TestNewRejectsUnknownLevel(t *testing.T) {
	t.Parallel()

	_, err := logger.New(io.Discard, "verbose")
	require.Error(t, err)
}

func TestNewFiltersByLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log, err := logger.New(&buf, "error")
	require.NoError(t, err)

	log.InfoContext(context.Background(), "hidden")
	log.ErrorContext(context.Background(), "visible")

	require.NotContains(t, buf.String(), "hidden")
	require.Contains(t, buf.String(), "visible")
}
