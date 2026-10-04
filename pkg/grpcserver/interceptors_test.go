package grpcserver

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/trb1maker/subscriptions/pkg/logger"
)

func TestRecoverInterceptor(t *testing.T) {
	t.Parallel()

	log, err := logger.New(io.Discard, "error")
	require.NoError(t, err)

	_, err = recoverInterceptor(log)(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/auth.v1.AuthService/Register"},
		func(context.Context, any) (any, error) {
			panic("boom")
		},
	)

	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.Internal, st.Code())
}

func TestDeadlineInterceptorSetsDeadline(t *testing.T) {
	t.Parallel()

	_, err := deadlineInterceptor(time.Second)(context.Background(), nil, nil, func(ctx context.Context, _ any) (any, error) {
		_, ok := ctx.Deadline()
		require.True(t, ok)

		return "ok", nil
	})
	require.NoError(t, err)
}

func TestDeadlineInterceptorKeepsExistingDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	deadline, ok := ctx.Deadline()
	require.True(t, ok)

	_, err := deadlineInterceptor(time.Second)(ctx, nil, nil, func(ctx context.Context, _ any) (any, error) {
		got, hasDeadline := ctx.Deadline()
		require.True(t, hasDeadline)
		require.Equal(t, deadline, got)

		return "ok", nil
	})
	require.NoError(t, err)
}
