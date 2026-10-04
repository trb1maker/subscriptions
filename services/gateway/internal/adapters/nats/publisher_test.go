package nats

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
	"uuid"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/trb1maker/subscriptions/api/gen/events/v1"
	"github.com/trb1maker/subscriptions/pkg/logger"
	"github.com/trb1maker/subscriptions/services/gateway/internal/app"
	"github.com/trb1maker/subscriptions/services/gateway/internal/domain"
)

func TestMessageUsesOwnerSubject(t *testing.T) {
	t.Parallel()

	eventID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ownerID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	when := time.Date(2026, 10, 4, 12, 0, 0, int(time.Millisecond), time.UTC)
	event := app.MessageEvent{
		ID: eventID, OccurredAt: when, OwnerID: ownerID, OwnerKind: "organization", Tokens: 7,
	}

	require.Equal(t, "usage.owner.organization."+ownerID.String(), Subject(event.OwnerKind, event.OwnerID.String()))

	var decoded eventsv1.UsageEvent
	require.NoError(t, proto.Unmarshal(mustMarshal(t, message(event)), &decoded))
	require.Equal(t, eventID.String(), decoded.GetEventId())
	require.Equal(t, when.Format(time.RFC3339Nano), decoded.GetOccurredAt())
	require.Equal(t, "organization", decoded.GetOwnerKind())
	require.Equal(t, ownerID.String(), decoded.GetOwnerId())
	require.Equal(t, int64(7), decoded.GetMessageProcessed().GetTokens())

	event.Failed = true
	decoded.Reset()
	require.NoError(t, proto.Unmarshal(mustMarshal(t, message(event)), &decoded))
	require.Equal(t, int64(7), decoded.GetMessageFailed().GetTokens())
	require.Nil(t, decoded.GetMessageProcessed())
}

func TestPublishAcceptsDuplicate(t *testing.T) {
	t.Parallel()

	log, err := logger.New(io.Discard, "error")
	require.NoError(t, err)

	stream := &fakeStream{ack: &jetstream.PubAck{Duplicate: true}}
	publisher := NewPublisher(stream, log)
	event := app.MessageEvent{
		ID: uuid.New(), OccurredAt: time.Now().UTC(), OwnerID: uuid.New(), OwnerKind: "user", Tokens: 3,
	}

	require.NoError(t, publisher.Publish(context.Background(), event))
	require.Equal(t, 1, stream.calls)

	var decoded eventsv1.UsageEvent
	require.NoError(t, proto.Unmarshal(stream.payload, &decoded))
	require.Equal(t, event.ID.String(), decoded.GetEventId())
	require.Equal(t, int64(3), decoded.GetMessageProcessed().GetTokens())

	stream.err = errors.New("no responders")
	err = publisher.Publish(context.Background(), event)
	require.ErrorIs(t, err, domain.ErrUnavailable)
	require.Equal(t, 2, stream.calls)
}

type fakeStream struct {
	ack     *jetstream.PubAck
	err     error
	payload []byte
	calls   int
}

func (f *fakeStream) PublishMsg(_ context.Context, msg *nats.Msg, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	f.calls++
	f.payload = append([]byte(nil), msg.Data...)

	return f.ack, f.err
}

func mustMarshal(t *testing.T, message *eventsv1.UsageEvent) []byte {
	t.Helper()

	body, err := proto.Marshal(message)
	require.NoError(t, err)

	return body
}
