package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunUsage(t *testing.T) {
	require.Equal(t, exitUsage, run(nil))
	require.Equal(t, exitUsage, run([]string{"missing", "up"}))
}

func TestRunRequiresDatabaseURL(t *testing.T) {
	t.Setenv("AUTH_DATABASE_URL", "")

	require.Equal(t, 1, run([]string{"auth", "up"}))
}
