package sl_test

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/luminyra/sl"
)

// stable drops the time and latency attributes so that example output does
// not change between runs.
func stable(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey || a.Key == "latency" {
		return slog.Attr{}
	}
	return a
}

func ExampleChain() {
	text := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: stable})
	logger := slog.New(sl.Chain(text,
		sl.FormatMiddleware(sl.Redact("password"), sl.FormatErrors()),
	))

	logger.Info("login", "user", "alice", "password", "hunter2")
	logger.Error("query failed", "err", errors.New("connection refused"))
	// Output:
	// level=INFO msg=login user=alice password=*******
	// level=ERROR msg="query failed" err.message="connection refused" err.type=*errors.errorString
}

func ExampleAnyOf() {
	text := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: stable})
	sampler := sl.AnyOf(
		sl.LevelAtLeast(slog.LevelError),       // never drop errors
		sl.ThresholdSampler(time.Hour, 1, nil), // otherwise keep the first record of each message
	)
	logger := slog.New(sl.NewSampleHandler(text, sampler))

	for range 3 {
		logger.Warn("retrying")
		logger.Error("giving up")
	}
	// Output:
	// level=WARN msg=retrying
	// level=ERROR msg="giving up"
	// level=ERROR msg="giving up"
	// level=ERROR msg="giving up"
}

func ExampleHTTPMiddleware() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: stable}))
	handler := sl.HTTPMiddleware(logger, sl.HTTPConfig{})(http.NotFoundHandler())

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))
	// Output: level=WARN msg="http request" request.method=GET request.path=/missing request.query="" request.ip=192.0.2.1:1234 request.user_agent="" response.status=404 response.bytes=19
}
