package caller_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/trb1maker/subscriptions/pkg/caller"
	"github.com/trb1maker/subscriptions/pkg/logger"
)

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := logger.WithRequestID(context.Background(), "req-1")
	ctx = caller.NewContext(ctx, caller.Caller{
		SubjectID: "user-1",
		Kind:      "user",
		Roles:     []string{"admin", "user"},
	})

	outgoing := caller.AppendOutgoing(ctx)
	md, ok := metadata.FromOutgoingContext(outgoing)
	require.True(t, ok)

	incoming := caller.FromIncoming(metadata.NewIncomingContext(context.Background(), md))
	got, ok := caller.FromContext(incoming)
	require.True(t, ok)
	require.Equal(t, "user-1", got.SubjectID)
	require.Equal(t, "user", got.Kind)
	require.Equal(t, []string{"admin", "user"}, got.Roles)
	require.Equal(t, "req-1", logger.RequestID(incoming))
}

func TestAppendOutgoingWithoutCallerKeepsRequestID(t *testing.T) {
	t.Parallel()

	ctx := caller.AppendOutgoing(logger.WithRequestID(context.Background(), "req-2"))
	md, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)
	require.Equal(t, []string{"req-2"}, md.Get(caller.MetadataRequestID))
	require.Empty(t, md.Get(caller.MetadataSubject))
}

func TestNewContextIgnoresEmptySubject(t *testing.T) {
	t.Parallel()

	ctx := caller.NewContext(context.Background(), caller.Caller{Kind: "user"})
	_, ok := caller.FromContext(ctx)
	require.False(t, ok)
}

func TestFromContextCopiesRoles(t *testing.T) {
	t.Parallel()

	ctx := caller.NewContext(context.Background(), caller.Caller{
		SubjectID: "user-1",
		Roles:     []string{"admin"},
	})
	got, ok := caller.FromContext(ctx)
	require.True(t, ok)
	got.Roles[0] = "changed"

	stored, ok := caller.FromContext(ctx)
	require.True(t, ok)
	require.Equal(t, []string{"admin"}, stored.Roles)
}
