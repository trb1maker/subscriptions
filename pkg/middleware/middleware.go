package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/trb1maker/subscriptions/pkg/logger"
)

// Use ставит middleware Chi: идентификатор запроса, access-log и recover.
// В журнал пишет переданный slog-логгер, а не текстовый логгер Chi.
func Use(router chi.Router, log *slog.Logger) {
	router.Use(chimw.RequestID)
	router.Use(copyRequestID)
	router.Use(chimw.RequestLogger(slogFormatter{log: log}))
	router.Use(chimw.Recoverer)
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
	e.log.DebugContext(e.req.Context(), "request completed",
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
