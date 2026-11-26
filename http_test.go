package sl

import (
	"bytes"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serveHTTP(t *testing.T, cfg HTTPConfig, h http.HandlerFunc, target string, header http.Header) string {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	req := httptest.NewRequest(http.MethodGet, target, nil)
	maps.Copy(req.Header, header)
	HTTPMiddleware(logger, cfg)(h).ServeHTTP(httptest.NewRecorder(), req)
	return buf.String()
}

func TestHTTPLogsRequest(t *testing.T) {
	out := serveHTTP(t, HTTPConfig{RequestIDHeader: "X-Request-Id"}, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("hello"))
	}, "/users?id=1", http.Header{"X-Request-Id": {"req-1"}})

	for _, want := range []string{
		"level=INFO", `msg="http request"`, "request.method=GET", "request.path=/users",
		`request.query="id=1"`, "request.id=req-1", "response.status=200", "response.bytes=5", "response.latency=",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
}

func TestHTTPLevelByStatus(t *testing.T) {
	for status, level := range map[int]string{
		http.StatusNoContent:           "level=INFO",
		http.StatusNotFound:            "level=WARN",
		http.StatusInternalServerError: "level=ERROR",
	} {
		out := serveHTTP(t, HTTPConfig{}, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}, "/", nil)
		if !strings.Contains(out, level) {
			t.Errorf("status %d: %q", status, out)
		}
	}
}

func TestHTTPInformationalStatusIgnored(t *testing.T) {
	out := serveHTTP(t, HTTPConfig{}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusCreated)
	}, "/", nil)
	if !strings.Contains(out, "response.status=201") {
		t.Fatalf("output = %q", out)
	}
}

func TestHTTPSkip(t *testing.T) {
	cfg := HTTPConfig{}
	cfg.Skip = func(r *http.Request) bool { return r.URL.Path == "/healthz" }
	if out := serveHTTP(t, cfg, func(http.ResponseWriter, *http.Request) {}, "/healthz", nil); out != "" {
		t.Fatalf("skipped request logged: %q", out)
	}
}

func TestHTTPResponseControllerAndFlusher(t *testing.T) {
	serveHTTP(t, HTTPConfig{}, func(w http.ResponseWriter, _ *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Error("writer does not implement http.Flusher")
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("ResponseController.Flush = %v", err)
		}
	}, "/", nil)
}

func TestHTTPCustomLevel(t *testing.T) {
	cfg := HTTPConfig{Level: func(status int) slog.Level {
		if status == http.StatusNotFound {
			return slog.LevelDebug // 404s are expected for this service
		}
		return StatusLevel(status)
	}}
	out := serveHTTP(t, cfg, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}, "/", nil)
	if !strings.Contains(out, "level=DEBUG") || !strings.Contains(out, "response.status=404") {
		t.Fatalf("output = %q", out)
	}
}

func TestHTTPZeroConfigOmitsRequestID(t *testing.T) {
	out := serveHTTP(t, HTTPConfig{}, func(http.ResponseWriter, *http.Request) {}, "/", http.Header{"X-Request-Id": {"req-1"}})
	if strings.Contains(out, "request.id") {
		t.Fatalf("request id logged without RequestIDHeader: %q", out)
	}
}

func TestHTTPUnwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	h := HTTPMiddleware(slog.New(slog.DiscardHandler), HTTPConfig{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok || u.Unwrap() != rec {
			t.Error("Unwrap must return the underlying writer")
		}
	}))
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
}

func BenchmarkHTTPMiddleware(b *testing.B) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	req := httptest.NewRequest(http.MethodGet, "/users/42?x=1", nil)
	b.Run("bare", func(b *testing.B) {
		for b.Loop() {
			ok.ServeHTTP(httptest.NewRecorder(), req)
		}
	})
	b.Run("level disabled", func(b *testing.B) {
		logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
		h := HTTPMiddleware(logger, HTTPConfig{})(ok)
		for b.Loop() {
			h.ServeHTTP(httptest.NewRecorder(), req)
		}
	})
	b.Run("HTTPMiddleware", func(b *testing.B) {
		h := HTTPMiddleware(slog.New(slog.NewJSONHandler(io.Discard, nil)), HTTPConfig{})(ok)
		for b.Loop() {
			h.ServeHTTP(httptest.NewRecorder(), req)
		}
	})
}
