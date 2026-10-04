package metrics

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/trb1maker/subscriptions/pkg/health"
)

const defaultHealthInterval = 5 * time.Second

// Границы в секундах. Верхние покрывают эмуляцию генерации.
//
//nolint:mnd // набор границ histogram, не параметры бизнес-логики
var httpBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120}

// Metrics — реестр Prometheus одного процесса. Метка service стоит на всех рядах.
type Metrics struct {
	reg        *prometheus.Registry
	registerer prometheus.Registerer
	requests   *prometheus.CounterVec
	duration   *prometheus.HistogramVec
	errors     *prometheus.CounterVec
	serverRPC  *prometheus.CounterVec
	serverTime *prometheus.HistogramVec
	clientRPC  *prometheus.CounterVec
	clientTime *prometheus.HistogramVec
	healthy    prometheus.Gauge
}

// New собирает технические метрики HTTP и gRPC.
func New(service string) *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	registerer := prometheus.WrapRegistererWith(prometheus.Labels{"service": service}, reg)

	m := &Metrics{
		reg:        reg,
		registerer: registerer,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Количество HTTP-запросов.",
		}, []string{"endpoint", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Время HTTP-ответа.",
			Buckets: httpBuckets,
		}, []string{"endpoint"}),
		errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_request_errors_total",
			Help: "HTTP-ответы с кодом 400 и выше.",
		}, []string{"endpoint", "error_type"}),
		serverRPC: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "grpc_server_requests_total",
			Help: "Входящие gRPC-вызовы.",
		}, []string{"rpc", "code"}),
		serverTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "grpc_server_request_duration_seconds",
			Help:    "Время входящего gRPC-вызова.",
			Buckets: prometheus.DefBuckets,
		}, []string{"rpc"}),
		clientRPC: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "grpc_client_requests_total",
			Help: "Исходящие gRPC-вызовы.",
		}, []string{"rpc", "code"}),
		clientTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "grpc_client_request_duration_seconds",
			Help:    "Время исходящего gRPC-вызова.",
			Buckets: prometheus.DefBuckets,
		}, []string{"rpc"}),
		healthy: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "service_healthy",
			Help: "1, если проверки здоровья проходят.",
		}),
	}

	registerer.MustRegister(
		m.requests, m.duration, m.errors,
		m.serverRPC, m.serverTime, m.clientRPC, m.clientTime,
		m.healthy,
	)

	return m
}

// Registerer принимает бизнес-метрики того же процесса.
func (m *Metrics) Registerer() prometheus.Registerer {
	return m.registerer
}

// Handler отдаёт текст Prometheus.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// SkipPath — служебные маршруты, которые не попадают в RED-метрики и трейсы.
func SkipPath(path string) bool {
	switch path {
	case "/metrics", "/health":
		return true
	default:
		return false
	}
}

// Middleware считает запросы после того, как Chi выбрал шаблон маршрута.
func (m *Metrics) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if SkipPath(r.URL.Path) {
				next.ServeHTTP(w, r)

				return
			}

			start := time.Now()
			wrapped := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(wrapped, r)

			endpoint := r.URL.Path
			if route := chi.RouteContext(r.Context()); route != nil && route.RoutePattern() != "" {
				endpoint = route.RoutePattern()
			}

			status := wrapped.Status()
			if status == 0 {
				status = http.StatusOK
			}

			m.ObserveHTTP(endpoint, status, time.Since(start))
		})
	}
}

// ObserveHTTP обновляет RED-метрики одного ответа.
func (m *Metrics) ObserveHTTP(endpoint string, status int, elapsed time.Duration) {
	code := strconv.Itoa(status)
	m.requests.WithLabelValues(endpoint, code).Inc()
	m.duration.WithLabelValues(endpoint).Observe(elapsed.Seconds())
	if status >= http.StatusBadRequest {
		m.errors.WithLabelValues(endpoint, code).Inc()
	}
}

// ObserveServer записывает входящий gRPC-вызов.
func (m *Metrics) ObserveServer(method, code string, elapsed time.Duration) {
	m.serverRPC.WithLabelValues(method, code).Inc()
	m.serverTime.WithLabelValues(method).Observe(elapsed.Seconds())
}

// ObserveClient записывает исходящий gRPC-вызов.
func (m *Metrics) ObserveClient(method, code string, elapsed time.Duration) {
	m.clientRPC.WithLabelValues(method, code).Inc()
	m.clientTime.WithLabelValues(method).Observe(elapsed.Seconds())
}

// WatchHealth ставит service_healthy в 1 или 0. По отмене context значение становится 0.
func (m *Metrics) WatchHealth(ctx context.Context, every time.Duration, checks ...health.Check) {
	if every <= 0 {
		every = defaultHealthInterval
	}

	go watchHealth(ctx, m.healthy, every, checks)
}

func watchHealth(ctx context.Context, gauge prometheus.Gauge, every time.Duration, checks []health.Check) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	gauge.Set(healthValue(ctx, checks))

	for {
		select {
		case <-ctx.Done():
			gauge.Set(0)

			return
		case <-ticker.C:
			gauge.Set(healthValue(ctx, checks))
		}
	}
}

func healthValue(ctx context.Context, checks []health.Check) float64 {
	for _, check := range checks {
		if err := check.Fn(ctx); err != nil {
			return 0
		}
	}

	return 1
}
