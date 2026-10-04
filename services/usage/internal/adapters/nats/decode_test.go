package nats_test

import (
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/trb1maker/subscriptions/api/gen/events/v1"
	"github.com/trb1maker/subscriptions/services/usage/internal/adapters/nats"
	"github.com/trb1maker/subscriptions/services/usage/internal/domain"
)

func TestDecodePayment(t *testing.T) {
	t.Parallel()

	eventID := uuid.New()
	ownerID := uuid.New()
	when := time.Date(2026, 10, 4, 12, 0, 0, 123000000, time.UTC)
	body, err := proto.Marshal(&eventsv1.UsageEvent{
		EventId:    eventID.String(),
		OccurredAt: when.Format(time.RFC3339Nano),
		OwnerKind:  string(domain.OwnerUser),
		OwnerId:    ownerID.String(),
		Kind: &eventsv1.UsageEvent_PaymentReceived{PaymentReceived: &eventsv1.PaymentReceived{
			Allowance:   10,
			PaymentId:   "pay-1",
			AmountMinor: 9900,
		}},
	})
	require.NoError(t, err)

	event, err := nats.Decode(body, eventID.String())
	require.NoError(t, err)
	require.Equal(t, domain.EventPaymentReceived, event.Type)
	require.Equal(t, int64(10), event.Allowance)
	require.True(t, event.HasAllowance)
	require.Equal(t, "pay-1", event.PaymentID)
	require.Equal(t, ownerID, event.Owner.ID)
	require.True(t, event.OccurredAt.Equal(when))
}

func TestDecodeRejectsMismatchedMessageID(t *testing.T) {
	t.Parallel()

	body, err := proto.Marshal(&eventsv1.UsageEvent{
		EventId:    uuid.New().String(),
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
		OwnerKind:  string(domain.OwnerUser),
		OwnerId:    uuid.New().String(),
		Kind:       &eventsv1.UsageEvent_MessageFailed{MessageFailed: &eventsv1.MessageFailed{Tokens: 1}},
	})
	require.NoError(t, err)

	_, err = nats.Decode(body, uuid.New().String())
	require.ErrorIs(t, err, domain.ErrInvalidArgument)
}

func TestDecodeRejectsEmptyKind(t *testing.T) {
	t.Parallel()

	body, err := proto.Marshal(&eventsv1.UsageEvent{
		EventId:    uuid.New().String(),
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
		OwnerKind:  string(domain.OwnerOrganization),
		OwnerId:    uuid.New().String(),
	})
	require.NoError(t, err)

	_, err = nats.Decode(body, "")
	require.ErrorIs(t, err, domain.ErrInvalidArgument)
}
