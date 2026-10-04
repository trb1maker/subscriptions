package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"

	"github.com/trb1maker/subscriptions/pkg/logger"
	"github.com/trb1maker/subscriptions/pkg/metrics"
)

// Option настраивает трассировку и метрики поверх access-log.
type Option func(*settings)

type settings struct {
	metrics *metrics.Metrics
	trace   bool
}

// WithMetrics считает HTTP RED-метрики.
func WithMetrics(m *metrics.Metrics) Option {
	return func(cfg *settings) {
		cfg.metrics = m
	}
}

// WithTracing открывает span до access-log, чтобы в строку попал trace_id.
func WithTracing() Option {
	return func(cfg *settings) {
		cfg.trace = true
	}
}

// Use ставит middleware Chi: идентификатор запроса, access-log и recover.
// В журнал пишет переданный slog-логгер, а не текстовый логгер Chi.
func Use(router chi.Router, log *slog.Logger, opts ...Option) {
	cfg := settings{}
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.trace {
		router.Use(otelhttp.NewMiddleware("http",
			otelhttp.WithFilter(func(r *http.Request) bool {
				return !metrics.SkipPath(r.URL.Path)
			}),
			otelhttp.WithSpanNameFormatter(spanName),
		))
		// Форматтер вызывается до routeHTTP, шаблона ещё нет.
		// Имя по шаблону ставится после next, когда Chi маршрут уже выбран.
		router.Use(renameSpan)
	}

	router.Use(chimw.RequestID)
	router.Use(copyRequestID)
	if cfg.metrics != nil {
		router.Use(cfg.metrics.Middleware())
	}

	router.Use(chimw.RequestLogger(slogFormatter{log: log}))
	router.Use(chimw.Recoverer)
}

func spanName(_ string, r *http.Request) string {
	pattern := r.URL.Path
	if route := chi.RouteContext(r.Context()); route != nil && route.RoutePattern() != "" {
		pattern = route.RoutePattern()
	}

	return r.Method + " " + pattern
}

func renameSpan(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)

		route := chi.RouteContext(r.Context())
		if route == nil || route.RoutePattern() == "" {
			return
		}

		trace.SpanFromContext(r.Context()).SetName(spanName("", r))
	})
}

func copyRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chimw.GetReqID(r.Context())
		if id != "" {
			w.Header().Set(chimw.RequestIDHeader, id)
			r = r.WithContext(logger.WithRequestID(r.Context(), id))
		}

		next.ServeHTTP(w, r)
	})
}

type slogFormatter struct {
	log *slog.Logger
}

func (f slogFormatter) NewLogEntry(r *http.Request) chimw.LogEntry {
	return &slogEntry{log: f.log, req: r}
}

type slogEntry struct {
	log *slog.Logger
	req *http.Request
}

func (e *slogEntry) Write(status, _ int, _ http.Header, elapsed time.Duration, _ any) {
	e.log.InfoContext(e.req.Context(), "request completed",
		"method", e.req.Method,
		"path", e.req.URL.Path,
		"status", status,
		"duration", elapsed.String(),
	)
}

func (e *slogEntry) Panic(v any, stack []byte) {
	e.log.ErrorContext(e.req.Context(), "panic recovered",
		"panic", fmt.Sprint(v),
		"stack", string(stack),
	)
}
