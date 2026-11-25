package sl

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// failing is a handler that always fails.
type failing struct{ slog.Handler }

func (failing) Handle(context.Context, slog.Record) error { return errors.New("down") }

func textHandler(buf *bytes.Buffer, level slog.Level) slog.Handler {
	return slog.NewTextHandler(buf, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})
}

func record(level slog.Level, msg string) slog.Record {
	return slog.NewRecord(time.Time{}, level, msg, 0)
}

func TestFailover(t *testing.T) {
	var primary, backup bytes.Buffer
	h := Failover(failing{textHandler(&primary, slog.LevelInfo)}, textHandler(&backup, slog.LevelInfo))
	if err := h.Handle(context.Background(), record(slog.LevelInfo, "x")); err != nil {
		t.Fatalf("Failover err = %v", err)
	}
	if !strings.Contains(backup.String(), "msg=x") {
		t.Fatalf("backup = %q", backup.String())
	}

	allDown := Failover(failing{textHandler(&primary, slog.LevelInfo)}, failing{textHandler(&backup, slog.LevelInfo)})
	if err := allDown.Handle(context.Background(), record(slog.LevelInfo, "x")); err == nil {
		t.Fatal("Failover with every handler down returned nil")
	}
}

func TestRouter(t *testing.T) {
	var errorsOnly, all bytes.Buffer
	h := Router(
		Route{
			Handler: textHandler(&errorsOnly, slog.LevelDebug),
			Match:   func(_ context.Context, r slog.Record) bool { return r.Level >= slog.LevelError },
		},
		Route{Handler: textHandler(&all, slog.LevelDebug)},
	)
	logger := slog.New(h)
	logger.Info("info")
	logger.Error("bad")

	if got := errorsOnly.String(); strings.Contains(got, "info") || !strings.Contains(got, "bad") {
		t.Errorf("error route = %q", got)
	}
	if got := all.String(); !strings.Contains(got, "info") || !strings.Contains(got, "bad") {
		t.Errorf("catch-all route = %q", got)
	}
}

// prefix is a middleware that adds a tag attribute to every record.
func prefix(tag string) Middleware {
	return func(next slog.Handler) slog.Handler { return tagging{Handler: next, tag: tag} }
}

type tagging struct {
	slog.Handler
	tag string
}

func (h tagging) Handle(ctx context.Context, r slog.Record) error {
	r.AddAttrs(slog.String("mw", h.tag))
	return h.Handler.Handle(ctx, r)
}

func TestChainOrder(t *testing.T) {
	var buf bytes.Buffer
	h := Chain(textHandler(&buf, slog.LevelInfo), prefix("first"), prefix("second"))
	slog.New(h).Info("x")
	if got := buf.String(); !strings.Contains(got, "mw=first mw=second") {
		t.Fatalf("Chain output = %q", got)
	}
}

func TestFailoverAndRouterKeepAttrsAndGroups(t *testing.T) {
	var failoverOut, routerOut bytes.Buffer
	disabled := textHandler(&failoverOut, slog.LevelError) // skipped for Info records
	failoverLogger := slog.New(Failover(disabled, textHandler(&failoverOut, slog.LevelInfo)))
	routerLogger := slog.New(Router(
		Route{Handler: textHandler(&routerOut, slog.LevelError)}, // disabled for Info records
		Route{Handler: textHandler(&routerOut, slog.LevelInfo)},
	))

	for _, logger := range []*slog.Logger{failoverLogger, routerLogger} {
		logger.With("svc", "api").WithGroup("req").WithGroup("").Info("hello", "id", 1)
	}
	for name, out := range map[string]string{"failover": failoverOut.String(), "router": routerOut.String()} {
		if strings.Count(out, "msg=hello svc=api req.id=1") != 1 {
			t.Errorf("%s output = %q", name, out)
		}
	}
	if failoverLogger.Enabled(t.Context(), slog.LevelDebug) || !failoverLogger.Enabled(t.Context(), slog.LevelInfo) {
		t.Error("Failover.Enabled must report whether any handler is enabled")
	}
}
