package nats

import (
	"context"
	"fmt"
	"time"

	natsio "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const connectTimeout = 5 * time.Second

// Connect открывает соединение и JetStream. Ошибка значит, что брокер недоступен при старте.
func Connect(ctx context.Context, url string) (*natsio.Conn, jetstream.JetStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("connect nats: %w", err)
	}

	conn, err := natsio.Connect(url, natsio.Timeout(connectTimeout))
	if err != nil {
		return nil, nil, fmt.Errorf("connect nats: %w", err)
	}

	stream, err := jetstream.New(conn)
	if err != nil {
		conn.Close()

		return nil, nil, fmt.Errorf("open jetstream: %w", err)
	}

	return conn, stream, nil
}
