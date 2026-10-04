package nats_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/pkg/nats"
)

func TestConnectRejectsEmptyURL(t *testing.T) {
	t.Parallel()

	_, _, err := nats.Connect(context.Background(), "")
	require.Error(t, err)
}
