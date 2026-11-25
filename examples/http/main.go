// Command http logs one record per request with sl's net/http middleware,
// using a custom status-to-level mapping and skipping the health check
// endpoint.
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/luminyra/sl"
)

func main() {
	// Drop the time and the latency to keep the example output stable.
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey || a.Key == "latency" {
				return slog.Attr{}
			}
			return a
		},
	}))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "user %s", r.PathValue("id"))
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// region: middleware
	// The zero HTTPConfig works as is. This one also logs the request ID,
	// logs 404s at Debug, and skips the health check.
	handler := sl.HTTPMiddleware(logger, sl.HTTPConfig{
		RequestIDHeader: "X-Request-Id",
		Level: func(status int) slog.Level {
			if status == http.StatusNotFound {
				return slog.LevelDebug
			}
			return sl.StatusLevel(status) // the default: 2xx/3xx Info, 4xx Warn, 5xx Error
		},
		Skip: func(r *http.Request) bool { return r.URL.Path == "/healthz" },
	})(mux)
	// In a real server: http.ListenAndServe(":8080", handler)
	// endregion

	// Simulate three requests with httptest: a success, a 404, and the
	// skipped health check.
	for i, target := range []string{"/users/42", "/missing", "/healthz"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("X-Request-Id", fmt.Sprintf("req-%d", i+1))
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}
}
