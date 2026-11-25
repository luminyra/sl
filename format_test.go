package sl

import (
	"bytes"
	"errors"
	"io/fs"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func newFormatLogger(buf *bytes.Buffer, formatters ...Formatter) *slog.Logger {
	text := slog.NewTextHandler(buf, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})
	return slog.New(NewFormatHandler(text, formatters...))
}

func TestRedactAtAnyDepth(t *testing.T) {
	var buf bytes.Buffer
	logger := newFormatLogger(&buf, Redact("password", "token"))
	logger.With("token", "abc").WithGroup("user").Info("login",
		"name", "ann",
		"password", "secret",
		slog.Group("auth", "token", "xyz"),
	)
	got := buf.String()
	if strings.Contains(got, "secret") || strings.Contains(got, "abc") || strings.Contains(got, "xyz") {
		t.Fatalf("secret leaked: %q", got)
	}
	for _, want := range []string{"token=*******", "user.name=ann", "user.password=*******", "user.auth.token=*******"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestFormatErrors(t *testing.T) {
	var buf bytes.Buffer
	newFormatLogger(&buf, FormatErrors()).Error("failed", "err", fs.ErrNotExist, "other", errors.New("x"))
	got := buf.String()
	if !strings.Contains(got, `err.message="file does not exist" err.type=*errors.errorString`) {
		t.Fatalf("output = %q", got)
	}
}

func TestTimeAndKeyFormatters(t *testing.T) {
	var buf bytes.Buffer
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	logger := newFormatLogger(&buf,
		FormatTimes(time.DateOnly, time.UTC),
		ForKey("n", func(v slog.Value) slog.Value { return slog.Int64Value(v.Int64() * 2) }),
	)
	logger.Info("x", "at", at, "n", 21)
	if got := buf.String(); !strings.Contains(got, "at=2026-01-02 n=42") {
		t.Fatalf("output = %q", got)
	}
}

type secret string

func TestForTypeAndMiddleware(t *testing.T) {
	var buf bytes.Buffer
	text := slog.NewTextHandler(&buf, nil)
	h := FormatMiddleware(ForType(func(secret) slog.Value { return slog.StringValue("hidden") }))(text)
	slog.New(h).Info("x", "key", secret("k-123"), "plain", "k-123")
	got := buf.String()
	if !strings.Contains(got, "key=hidden") || !strings.Contains(got, "plain=k-123") {
		t.Fatalf("output = %q", got)
	}
}

func TestForTypeIgnoresOtherKindsAndEmptyGroup(t *testing.T) {
	var buf bytes.Buffer
	logger := newFormatLogger(&buf, ForType(func(int) slog.Value { return slog.StringValue("never") }))
	logger.WithGroup("").Info("x", "n", 1, "s", "text")
	if got := buf.String(); !strings.Contains(got, "n=1 s=text") {
		t.Fatalf("output = %q", got)
	}
}
