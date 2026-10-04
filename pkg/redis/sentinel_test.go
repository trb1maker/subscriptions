package redis

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSentinel(t *testing.T) {
	t.Parallel()

	options, err := parseSentinel("sentinel://:secret@redis-1:26379,redis-2:26379/0?master=usage")
	require.NoError(t, err)
	require.Equal(t, "usage", options.MasterName)
	require.Equal(t, []string{"redis-1:26379", "redis-2:26379"}, options.SentinelAddrs)
	require.Equal(t, "secret", options.Password)
	require.Equal(t, 0, options.DB)
}

func TestParseSentinelDecodesPassword(t *testing.T) {
	t.Parallel()

	options, err := parseSentinel("sentinel://:p%40ss@redis-1:26379/1?master=usage")
	require.NoError(t, err)
	require.Equal(t, "p@ss", options.Password)
	require.Equal(t, 1, options.DB)
}

func TestParseSentinelRequiresMaster(t *testing.T) {
	t.Parallel()

	_, err := parseSentinel("sentinel://:secret@redis-1:26379/0")
	require.Error(t, err)
}

func TestParseSentinelRequiresAddress(t *testing.T) {
	t.Parallel()

	_, err := parseSentinel("sentinel://:secret@/0?master=usage")
	require.Error(t, err)
}
