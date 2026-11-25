package sl

import (
	"log/slog"
	"net/http"
	"time"
)

// HTTPConfig controls what HTTPMiddleware logs. The zero HTTPConfig is ready
// to use: it logs every request at StatusLevel and no request ID.
type HTTPConfig struct {
	// Level returns the level for a response status. Nil means StatusLevel.
	Level func(status int) slog.Level
	// RequestIDHeader names the request header logged as request.id, such as
	// "X-Request-Id". Empty disables it.
	RequestIDHeader string
	// Skip, when set, reports requests that must not be logged, such as
	// health checks.
	Skip func(r *http.Request) bool
}

// StatusLevel is the default [HTTPConfig.Level]: Info below 400, Warn for 4xx
// and Error for 5xx and above.
func StatusLevel(status int) slog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case status >= http.StatusBadRequest:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// HTTPMiddleware returns net/http middleware that logs one record per request
// to logger with cfg. The record is written after the handler returns; a
// request whose handler panics is not logged. The response writer passed to
// the handler supports [http.ResponseController] through Unwrap.
func HTTPMiddleware(logger *slog.Logger, cfg HTTPConfig) func(http.Handler) http.Handler {
	levelOf := cfg.Level
	if levelOf == nil {
		levelOf = StatusLevel
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.Skip != nil && cfg.Skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w}
			next.ServeHTTP(rw, r)

			status := rw.statusCode()
			reqAttrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("query", r.URL.RawQuery),
				slog.String("ip", r.RemoteAddr),
				slog.String("user_agent", r.UserAgent()),
			}
			if cfg.RequestIDHeader != "" {
				if id := r.Header.Get(cfg.RequestIDHeader); id != "" {
					reqAttrs = append(reqAttrs, slog.String("id", id))
				}
			}
			logger.LogAttrs(r.Context(), levelOf(status), "http request",
				slog.GroupAttrs("request", reqAttrs...),
				slog.GroupAttrs("response",
					slog.Int("status", status),
					slog.Int64("bytes", rw.bytes),
					slog.Duration("latency", time.Since(start)),
				),
			)
		})
	}
}

// responseWriter records the status code and the number of body bytes.
type responseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *responseWriter) WriteHeader(code int) {
	// Informational 1xx responses may precede the final status.
	if w.status == 0 && code >= http.StatusOK {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Unwrap lets [http.ResponseController] reach the underlying writer.
func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Flush keeps streaming handlers working when they assert [http.Flusher].
func (w *responseWriter) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}
