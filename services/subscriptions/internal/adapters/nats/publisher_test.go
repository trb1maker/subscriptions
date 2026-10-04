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
	"github.com/trb1maker/subscriptions/services/subscriptions/internal/app"
	"github.com/trb1maker/subscriptions/services/subscriptions/internal/domain"
)

func TestPublishPaymentReceived(t *testing.T) {
	t.Parallel()

	log, err := logger.New(io.Discard, "error")
	require.NoError(t, err)

	eventID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ownerID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	when := time.Date(2026, 10, 4, 12, 0, 0, int(time.Millisecond), time.UTC)
	event := app.PaymentNotice{
		ID: eventID, OccurredAt: when, Owner: domain.Owner{ID: ownerID, Kind: domain.OwnerUser},
		Allowance: 20, PaymentID: eventID.String(), AmountMinor: 100,
	}

	stream := &fakeStream{ack: &jetstream.PubAck{Duplicate: true}}
	require.NoError(t, NewPublisher(stream, log).PublishPaymentReceived(context.Background(), event))
	require.Equal(t, "usage.owner.user."+ownerID.String(), stream.subject)

	var decoded eventsv1.UsageEvent
	require.NoError(t, proto.Unmarshal(stream.payload, &decoded))
	require.Equal(t, eventID.String(), decoded.GetEventId())
	require.Equal(t, when.Format(time.RFC3339Nano), decoded.GetOccurredAt())
	require.Equal(t, int64(20), decoded.GetPaymentReceived().GetAllowance())
	require.Equal(t, eventID.String(), decoded.GetPaymentReceived().GetPaymentId())
	require.Equal(t, int64(100), decoded.GetPaymentReceived().GetAmountMinor())

	stream.err = errors.New("no responders")
	err = NewPublisher(stream, log).PublishPaymentReceived(context.Background(), event)
	require.ErrorIs(t, err, domain.ErrUnavailable)
}

type fakeStream struct {
	ack     *jetstream.PubAck
	err     error
	payload []byte
	subject string
	calls   int
}

func (f *fakeStream) PublishMsg(_ context.Context, msg *nats.Msg, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	f.calls++
	f.subject = msg.Subject
	f.payload = append([]byte(nil), msg.Data...)
	if f.err != nil {
		return nil, f.err
	}

	return f.ack, nil
}
