package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Start поднимает TracerProvider. Пустой endpoint оставляет трейсы в процессе.
// Ошибка экспортёра не мешает старту: в stderr уходит одна запись.
func Start(ctx context.Context, log *slog.Logger, service, endpoint string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	res, err := resource.New(ctx, resource.WithAttributes(attribute.String("service.name", service)))
	if err != nil && res == nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	if err != nil {
		log.ErrorContext(ctx, "otlp resource init failed", "error", err)
	}

	options := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if host, ok := exporterEndpoint(endpoint); ok {
		exp, expErr := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(host),
			otlptracegrpc.WithInsecure(),
		)
		if expErr != nil {
			log.ErrorContext(ctx, "otlp exporter init failed", "error", expErr)
		} else {
			options = append(options, sdktrace.WithBatcher(exp))
		}
	}

	provider := sdktrace.NewTracerProvider(options...)
	otel.SetTracerProvider(provider)

	return func(shutdownCtx context.Context) error {
		if err := provider.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown tracer: %w", err)
		}

		return nil
	}, nil
}

func exporterEndpoint(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "none" {
		return "", false
	}

	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" {
			return "", false
		}

		return parsed.Host, true
	}

	return raw, true
}
