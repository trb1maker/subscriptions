package nats

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/trb1maker/subscriptions/api/gen/events/v1"
	"github.com/trb1maker/subscriptions/services/gateway/internal/app"
	"github.com/trb1maker/subscriptions/services/gateway/internal/domain"
)

const subjectPrefix = "usage.owner."

type jetStream interface {
	Publish(ctx context.Context, subject string, payload []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// Publisher отправляет событие генерации в поток USAGE.
type Publisher struct {
	js  jetStream
	log *slog.Logger
}

// NewPublisher собирает адаптер поверх JetStream. Поток создаёт Usage.
func NewPublisher(js jetStream, log *slog.Logger) *Publisher {
	return &Publisher{js: js, log: log}
}

// Subject — субъект владельца остатка.
func Subject(kind, ownerID string) string {
	return subjectPrefix + kind + "." + ownerID
}

// Publish сериализует событие и ставит Nats-Msg-Id равным event_id.
// PubAck.Duplicate — это подтверждение, что событие с этим id уже принято.
func (p *Publisher) Publish(ctx context.Context, event app.MessageEvent) error {
	body, err := proto.Marshal(message(event))
	if err != nil {
		p.log.ErrorContext(ctx, "encode event failed", "error", err)

		return domain.ErrInternal
	}

	subject := Subject(event.OwnerKind, event.OwnerID.String())
	ack, err := p.js.Publish(ctx, subject, body, jetstream.WithMsgID(event.ID.String()))
	if err != nil {
		p.log.ErrorContext(ctx, "publish event failed", "error", err, "event_id", event.ID.String())

		return fmt.Errorf("publish event: %w", domain.ErrUnavailable)
	}

	if ack != nil && ack.Duplicate {
		p.log.InfoContext(ctx, "event already accepted", "event_id", event.ID.String())
	}

	return nil
}

func message(event app.MessageEvent) *eventsv1.UsageEvent {
	encoded := &eventsv1.UsageEvent{
		EventId:    event.ID.String(),
		OccurredAt: event.OccurredAt.UTC().Format(time.RFC3339Nano),
		OwnerKind:  event.OwnerKind,
		OwnerId:    event.OwnerID.String(),
	}
	if event.Failed {
		encoded.Kind = &eventsv1.UsageEvent_MessageFailed{MessageFailed: &eventsv1.MessageFailed{Tokens: event.Tokens}}

		return encoded
	}

	encoded.Kind = &eventsv1.UsageEvent_MessageProcessed{MessageProcessed: &eventsv1.MessageProcessed{Tokens: event.Tokens}}

	return encoded
}
