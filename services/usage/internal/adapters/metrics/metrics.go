package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

// Recorder считает сообщения, токены и овердрафт.
type Recorder struct {
	messages  *prometheus.CounterVec
	tokens    *prometheus.CounterVec
	overdraft *prometheus.CounterVec
}

// New регистрирует бизнес-метрики Usage.
func New(reg prometheus.Registerer) (*Recorder, error) {
	recorder := &Recorder{
		messages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "messages_consumed_total",
			Help: "Списанные сообщения.",
		}, []string{"type"}),
		tokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tokens_used_total",
			Help: "Затраченные токены.",
		}, []string{"type"}),
		overdraft: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "overdraft_count",
			Help: "Списания, после которых остаток стал отрицательным.",
		}, []string{"type"}),
	}

	for _, collector := range []prometheus.Collector{recorder.messages, recorder.tokens, recorder.overdraft} {
		if err := reg.Register(collector); err != nil {
			return nil, fmt.Errorf("register usage metrics: %w", err)
		}
	}

	return recorder, nil
}

// MessageConsumed увеличивает счётчик списаний.
func (r *Recorder) MessageConsumed(kind string) {
	r.messages.WithLabelValues(kind).Inc()
}

// TokensUsed добавляет токены успешной или неуспешной генерации.
func (r *Recorder) TokensUsed(kind string, tokens int64) {
	if tokens <= 0 {
		return
	}

	r.tokens.WithLabelValues(kind).Add(float64(tokens))
}

// Overdraft увеличивает счётчик овердрафта.
func (r *Recorder) Overdraft(kind string) {
	r.overdraft.WithLabelValues(kind).Inc()
}
