package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

// Recorder считает исходы платежей.
type Recorder struct {
	payments *prometheus.CounterVec
}

// New регистрирует payments_total.
func New(reg prometheus.Registerer) (*Recorder, error) {
	recorder := &Recorder{
		payments: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "payments_total",
			Help: "Платежи по статусу проведения.",
		}, []string{"status"}),
	}
	if err := reg.Register(recorder.payments); err != nil {
		return nil, fmt.Errorf("register payments_total: %w", err)
	}

	return recorder, nil
}

// Payment увеличивает счётчик статуса received или failed.
func (r *Recorder) Payment(status string) {
	r.payments.WithLabelValues(status).Inc()
}
