package nats

import (
	"fmt"
	"time"
	"uuid"

	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/trb1maker/subscriptions/api/gen/events/v1"
	"github.com/trb1maker/subscriptions/services/usage/internal/domain"
)

const subjectPrefix = "usage.owner."

// Subject — субъект JetStream владельца. Все его события идут в один субъект.
func Subject(owner domain.Owner) string {
	return subjectPrefix + string(owner.Kind) + "." + owner.ID.String()
}

// Decode разбирает protobuf события. messageID — заголовок Nats-Msg-Id, если он есть.
func Decode(data []byte, messageID string) (domain.Event, error) {
	var message eventsv1.UsageEvent
	if err := proto.Unmarshal(data, &message); err != nil {
		return domain.Event{}, fmt.Errorf("unmarshal event: %w", domain.ErrInvalidArgument)
	}

	occurredAt, err := time.Parse(time.RFC3339Nano, message.GetOccurredAt())
	if err != nil {
		return domain.Event{}, fmt.Errorf("parse occurred at: %w", domain.ErrInvalidArgument)
	}

	eventID, err := uuid.Parse(message.GetEventId())
	if err != nil {
		return domain.Event{}, fmt.Errorf("parse event id: %w", domain.ErrInvalidArgument)
	}

	ownerID, err := uuid.Parse(message.GetOwnerId())
	if err != nil {
		return domain.Event{}, fmt.Errorf("parse owner id: %w", domain.ErrInvalidArgument)
	}

	if messageID != "" && messageID != eventID.String() {
		return domain.Event{}, fmt.Errorf("message id mismatch: %w", domain.ErrInvalidArgument)
	}

	event := domain.Event{
		ID:         eventID,
		OccurredAt: occurredAt,
		Owner: domain.Owner{
			ID:   ownerID,
			Kind: domain.OwnerKind(message.GetOwnerKind()),
		},
	}
	if err := fillKind(&event, message.GetKind()); err != nil {
		return domain.Event{}, err
	}

	event = event.Normalized()
	if err := event.Validate(); err != nil {
		return domain.Event{}, err
	}

	return event, nil
}

func fillKind(event *domain.Event, kind any) error {
	switch body := kind.(type) {
	case *eventsv1.UsageEvent_MessageProcessed:
		event.Type = domain.EventMessageProcessed
		event.Tokens = body.MessageProcessed.GetTokens()
	case *eventsv1.UsageEvent_MessageFailed:
		event.Type = domain.EventMessageFailed
		event.Tokens = body.MessageFailed.GetTokens()
	case *eventsv1.UsageEvent_PaymentReceived:
		event.Type = domain.EventPaymentReceived
		event.HasAllowance = true
		event.Allowance = body.PaymentReceived.GetAllowance()
		event.PaymentID = body.PaymentReceived.GetPaymentId()
		event.AmountMinor = body.PaymentReceived.GetAmountMinor()
	case *eventsv1.UsageEvent_SubscriptionChanged:
		event.Type = domain.EventSubscriptionChanged
		event.HasAllowance = true
		event.Allowance = body.SubscriptionChanged.GetAllowance()
	case *eventsv1.UsageEvent_SubscriptionPeriodEnded:
		event.Type = domain.EventSubscriptionPeriodEnded
		event.HasAllowance = true
		event.Allowance = body.SubscriptionPeriodEnded.GetAllowance()
	default:
		return fmt.Errorf("missing event kind: %w", domain.ErrInvalidArgument)
	}

	return nil
}
