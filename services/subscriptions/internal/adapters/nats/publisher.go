package nats

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/trb1maker/subscriptions/api/gen/events/v1"
	"github.com/trb1maker/subscriptions/pkg/trace"
	"github.com/trb1maker/subscriptions/services/subscriptions/internal/app"
	"github.com/trb1maker/subscriptions/services/subscriptions/internal/domain"
)

const subjectPrefix = "usage.owner."

type jetStream interface {
	PublishMsg(ctx context.Context, msg *nats.Msg, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// Publisher отправляет события остатка в поток USAGE.
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
	return p.publish(ctx, event.ID, event.Owner, payment(event))
}

// PublishPeriodEnded сериализует окончание периода и ставит Nats-Msg-Id равным event_id.
func (p *Publisher) PublishPeriodEnded(ctx context.Context, event app.PeriodNotice) error {
	return p.publish(ctx, event.ID, event.Owner, periodEnded(event))
}

func (p *Publisher) publish(ctx context.Context, eventID uuid.UUID, owner domain.Owner, body *eventsv1.UsageEvent) error {
	payload, err := proto.Marshal(body)
	if err != nil {
		p.log.ErrorContext(ctx, "encode event failed", "error", err)

		return domain.ErrInternal
	}

	subject := Subject(string(owner.Kind), owner.ID.String())
	msg := &nats.Msg{Subject: subject, Data: payload, Header: trace.Headers(ctx)}
	ack, err := p.js.PublishMsg(ctx, msg, jetstream.WithMsgID(eventID.String()))
	if err != nil {
		p.log.ErrorContext(ctx, "publish event failed", "error", err, "event_id", eventID.String())

		return fmt.Errorf("publish event: %w", domain.ErrUnavailable)
	}

	if ack != nil && ack.Duplicate {
		p.log.InfoContext(ctx, "event already accepted", "event_id", eventID.String())
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

func periodEnded(event app.PeriodNotice) *eventsv1.UsageEvent {
	return &eventsv1.UsageEvent{
		EventId:    event.ID.String(),
		OccurredAt: event.OccurredAt.UTC().Format(time.RFC3339Nano),
		OwnerKind:  string(event.Owner.Kind),
		OwnerId:    event.Owner.ID.String(),
		Kind: &eventsv1.UsageEvent_SubscriptionPeriodEnded{SubscriptionPeriodEnded: &eventsv1.SubscriptionPeriodEnded{
			Allowance: event.Allowance,
		}},
	}
}
