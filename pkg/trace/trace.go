package trace

import (
	"context"
	"net/http"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/trb1maker/subscriptions"

func init() {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

// Headers кладёт W3C traceparent в заголовки NATS. Без активного span заголовков нет.
func Headers(ctx context.Context) nats.Header {
	carrier := http.Header{}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(carrier))
	if len(carrier) == 0 {
		return nil
	}

	return nats.Header(carrier)
}

// Context достаёт traceparent из заголовков сообщения.
func Context(ctx context.Context, headers nats.Header) context.Context {
	if len(headers) == 0 {
		return ctx
	}

	return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(http.Header(headers)))
}

// Start открывает span. Функция закрывает его.
func Start(ctx context.Context, name string) (context.Context, func()) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, name)

	return ctx, func() { span.End() }
}

// Fail помечает текущий span ошибкой.
func Fail(ctx context.Context, err error) {
	if err == nil {
		return
	}

	span := oteltrace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
