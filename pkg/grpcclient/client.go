package grpcclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/trb1maker/subscriptions/pkg/caller"
)

// Dial соединяется с target по mTLS и прокидывает вызывающего из context.
func Dial(ctx context.Context, target string, tlsCfg *tls.Config) (*grpc.ClientConn, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("dial grpc: %w", err)
	}

	if tlsCfg == nil {
		return nil, errors.New("missing tls config")
	}

	conn, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithChainUnaryInterceptor(UnaryClientInterceptor()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial grpc: %w", err)
	}

	return conn, nil
}

// UnaryClientInterceptor дописывает request id и вызывающего в исходящий вызов.
func UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if err := invoker(caller.AppendOutgoing(ctx), method, req, reply, cc, opts...); err != nil {
			return fmt.Errorf("invoke grpc: %w", err)
		}

		return nil
	}
}
