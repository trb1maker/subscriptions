package nats

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/trb1maker/subscriptions/services/usage/internal/domain"
)

const (
	streamName     = "USAGE"
	consumerName   = "usage"
	subjectFilter  = subjectPrefix + ">"
	ackWait        = 30 * time.Second
	redeliverAfter = time.Second
	handleTimeout  = 10 * time.Second
)

// Applier применяет одно событие.
type Applier interface {
	Apply(ctx context.Context, event domain.Event) error
}

// Run читает поток USAGE по одному сообщению, пока не закроется ctx.
func Run(ctx context.Context, js jetstream.JetStream, applier Applier, log *slog.Logger) error {
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{subjectFilter},
	})
	if err != nil {
		return fmt.Errorf("ensure usage stream: %w", err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       consumerName,
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: subjectFilter,
		AckWait:       ackWait,
	})
	if err != nil {
		return fmt.Errorf("ensure usage consumer: %w", err)
	}

	iter, err := consumer.Messages()
	if err != nil {
		return fmt.Errorf("open usage messages: %w", err)
	}
	defer iter.Stop()

	go func() {
		<-ctx.Done()
		iter.Stop()
	}()

	for {
		msg, nextErr := iter.Next()
		if nextErr != nil {
			if errors.Is(nextErr, jetstream.ErrMsgIteratorClosed) || ctx.Err() != nil {
				return nil
			}

			return fmt.Errorf("read usage message: %w", nextErr)
		}

		handle(ctx, msg, applier, log)
	}
}

func handle(ctx context.Context, msg jetstream.Msg, applier Applier, log *slog.Logger) {
	msgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), handleTimeout)
	defer cancel()

	messageID := msg.Headers().Get(jetstream.MsgIDHeader)
	event, err := Decode(msg.Data(), messageID)
	eventID := messageID
	if err == nil {
		eventID = event.ID.String()
		err = applier.Apply(msgCtx, event)
	}

	settle(msgCtx, log, msg, eventID, err)
}

func settle(ctx context.Context, log *slog.Logger, msg jetstream.Msg, eventID string, err error) {
	if err == nil {
		if ackErr := msg.Ack(); ackErr != nil {
			log.ErrorContext(ctx, "event ack failed", "error", ackErr, "event_id", eventID)
		}

		return
	}

	if errors.Is(err, domain.ErrInvalidArgument) || errors.Is(err, domain.ErrIdempotencyConflict) {
		log.ErrorContext(ctx, "event rejected", "error", err, "event_id", eventID)
		if termErr := msg.Term(); termErr != nil {
			log.ErrorContext(ctx, "event term failed", "error", termErr, "event_id", eventID)
		}

		return
	}

	log.ErrorContext(ctx, "event failed", "error", err, "event_id", eventID)
	if nakErr := msg.NakWithDelay(redeliverAfter); nakErr != nil {
		log.ErrorContext(ctx, "event nak failed", "error", nakErr, "event_id", eventID)
	}
}
