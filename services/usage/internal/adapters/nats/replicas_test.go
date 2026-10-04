package nats

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeReplicas(t *testing.T) {
	t.Parallel()

	require.Equal(t, 1, normalizeReplicas(0))
	require.Equal(t, 1, normalizeReplicas(1))
	require.Equal(t, 3, normalizeReplicas(3))
}
