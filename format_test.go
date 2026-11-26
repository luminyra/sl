package sl

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"slices"
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

// structuredError stands in for an error type, such as errs.Error, that logs
// itself as a group.
type structuredError struct{ code string }

func (e structuredError) Error() string { return "not found" }

func (e structuredError) LogValue() slog.Value {
	return slog.GroupValue(slog.String("message", e.Error()), slog.String("code", e.code))
}

type scalarError struct{}

func (scalarError) Error() string        { return "scalar" }
func (scalarError) LogValue() slog.Value { return slog.StringValue("ignored") }

type emptyWrapper struct{}

func (emptyWrapper) Error() string { return "empty" }
func (emptyWrapper) Unwrap() error { return nil }

type panickingError struct{}

func (panickingError) Error() string        { return "panicking" }
func (panickingError) LogValue() slog.Value { panic("boom") }

func TestFormatErrorsKeepsWrappedLogValuer(t *testing.T) {
	inner := structuredError{code: "USER_NOT_FOUND"}
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"direct", inner, `err.message="not found" err.code=USER_NOT_FOUND`},
		{"wrapped", fmt.Errorf("handler: %w", inner), `err.message="handler: not found" err.type=*fmt.wrapError err.code=USER_NOT_FOUND`},
		{"joined", errors.Join(errors.New("first"), inner), `err.type=*errors.joinError err.code=USER_NOT_FOUND`},
		{"scalar", fmt.Errorf("w: %w", scalarError{}), `err.message="w: scalar" err.type=*fmt.wrapError`},
		{"panicking", fmt.Errorf("w: %w", panickingError{}), `err.message="w: panicking" err.type=*fmt.wrapError`},
		{"joined plain", errors.Join(errors.New("a"), errors.New("b")), `err.message="a\nb" err.type=*errors.joinError`},
		{"nil unwrap", emptyWrapper{}, `err.message=empty err.type=sl.emptyWrapper`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			newFormatLogger(&buf, FormatErrors()).Error("failed", "err", tc.err)
			got := buf.String()
			if !strings.Contains(got, tc.want) {
				t.Fatalf("output = %q, want it to contain %q", got, tc.want)
			}
			if strings.Count(got, "err.message=") != 1 || strings.Contains(got, "ignored") {
				t.Fatalf("output = %q, want one message and no scalar LogValue", got)
			}
		})
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

func BenchmarkFormatHandler(b *testing.B) {
	attrs := []any{"user", "alice", "password", "secret", "err", errors.New("boom"), slog.Group("req", "id", 7, "token", "t")}
	b.Run("bare", func(b *testing.B) {
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		for b.Loop() {
			logger.Info("login", attrs...)
		}
	})
	b.Run("nothing to rewrite", func(b *testing.B) {
		plain := []any{"user", "alice", "n", 3, slog.Group("req", "id", 7, "path", "/")}
		logger := slog.New(NewFormatHandler(slog.NewJSONHandler(io.Discard, nil), Redact("password", "token"), FormatErrors()))
		for b.Loop() {
			logger.Info("login", plain...)
		}
	})
	b.Run("Redact+FormatErrors", func(b *testing.B) {
		logger := slog.New(NewFormatHandler(slog.NewJSONHandler(io.Discard, nil), Redact("password", "token"), FormatErrors()))
		for b.Loop() {
			logger.Info("login", attrs...)
		}
	})
}

// TestFormatterSeesGroupPaths checks the group paths passed to formatters for
// nested and sibling groups, which share one buffer while formatting.
func TestFormatterSeesGroupPaths(t *testing.T) {
	var seen []string
	record := func(groups []string, a slog.Attr) (slog.Value, bool) {
		if a.Value.Kind() != slog.KindGroup {
			seen = append(seen, strings.Join(append(slices.Clone(groups), a.Key), "."))
		}
		return a.Value, false
	}
	logger := slog.New(NewFormatHandler(slog.NewTextHandler(io.Discard, nil), record)).WithGroup("req")
	logger.Info("x",
		slog.Group("a", slog.Group("b", "k1", 1), "k2", 2),
		slog.Group("c", "k3", 3),
		"k4", 4,
	)
	expected := []string{"req.a.b.k1", "req.a.k2", "req.c.k3", "req.k4"}
	if !slices.Equal(seen, expected) {
		t.Fatalf("paths = %v, expected %v", seen, expected)
	}
}

type lazyValue struct{}

func (lazyValue) LogValue() slog.Value { return slog.StringValue("resolved") }

func TestUnchangedRecordsPassThrough(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewFormatHandler(slog.NewTextHandler(&buf, nil), Redact("password")))
	logger.Info("plain", "user", "alice", slog.Group("g", "n", 1), "lazy", lazyValue{})
	got := buf.String()
	for _, want := range []string{"user=alice", "g.n=1", "lazy=resolved"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}
