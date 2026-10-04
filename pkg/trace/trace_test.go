package trace_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"

	apptrace "github.com/trb1maker/subscriptions/pkg/trace"
)

func TestHeadersRoundTrip(t *testing.T) {
	t.Parallel()

	provider := trace.NewTracerProvider()
	ctx, span := provider.Tracer("test").Start(context.Background(), "publish")
	t.Cleanup(func() { span.End() })

	headers := apptrace.Headers(ctx)
	require.NotEmpty(t, http.Header(headers).Get("traceparent"))

	got := apptrace.Context(context.Background(), headers)
	remote := oteltrace.SpanFromContext(got).SpanContext()
	require.Equal(t, span.SpanContext().TraceID(), remote.TraceID())
	require.Equal(t, span.SpanContext().SpanID(), remote.SpanID())
}

func TestHeadersWithoutSpan(t *testing.T) {
	t.Parallel()

	require.Nil(t, apptrace.Headers(context.Background()))
}
