package nats

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/trb1maker/subscriptions/api/gen/events/v1"
	"github.com/trb1maker/subscriptions/services/subscriptions/internal/app"
	"github.com/trb1maker/subscriptions/services/subscriptions/internal/domain"
)

const subjectPrefix = "usage.owner."

type jetStream interface {
	Publish(ctx context.Context, subject string, payload []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// Publisher отправляет PaymentReceived в поток USAGE.
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

// PublishPaymentReceived сериализует платёж и ставит Nats-Msg-Id равным event_id.
func (p *Publisher) PublishPaymentReceived(ctx context.Context, event app.PaymentNotice) error {
	body, err := proto.Marshal(payment(event))
	if err != nil {
		p.log.ErrorContext(ctx, "encode event failed", "error", err)

		return domain.ErrInternal
	}

	subject := Subject(string(event.Owner.Kind), event.Owner.ID.String())
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

func payment(event app.PaymentNotice) *eventsv1.UsageEvent {
	return &eventsv1.UsageEvent{
		EventId:    event.ID.String(),
		OccurredAt: event.OccurredAt.UTC().Format(time.RFC3339Nano),
		OwnerKind:  string(event.Owner.Kind),
		OwnerId:    event.Owner.ID.String(),
		Kind: &eventsv1.UsageEvent_PaymentReceived{PaymentReceived: &eventsv1.PaymentReceived{
			Allowance:   event.Allowance,
			PaymentId:   event.PaymentID,
			AmountMinor: event.AmountMinor,
		}},
	}
}
