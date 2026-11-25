// Command handlers combines sl with the standard library: it redacts secrets,
// formats errors, samples without ever dropping errors, and fans out to
// several outputs with slog.NewMultiHandler.
package main

import (
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/luminyra/sl"
)

// dropTime removes the time attribute to keep the example output stable.
func dropTime(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return a
}

func main() {
	// region: setup
	// Every record goes to a text handler; errors also go to a JSON handler,
	// for example one that feeds an alerting system. The standard
	// slog.NewMultiHandler (Go 1.26+) does the fan-out.
	text := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: dropTime})
	alerts := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError, ReplaceAttr: dropTime})

	// Samplers are plain functions and compose: keep every error, and keep
	// only the first two records of any other message per second.
	sampler := sl.AnyOf(
		sl.LevelAtLeast(slog.LevelError),
		sl.ThresholdSampler(time.Second, 2, nil),
	)

	// The first middleware passed to Chain is the outermost: redact and
	// format first, then sample, then fan out.
	handler := sl.Chain(slog.NewMultiHandler(text, alerts),
		sl.FormatMiddleware(sl.Redact("password", "token"), sl.FormatErrors()),
		sl.SampleMiddleware(sampler),
	)
	logger := slog.New(handler)
	// endregion

	// region: use
	logger.Info("user logged in", "user", "alice", "password", "hunter2", slog.Group("auth", "token", "abc123"))
	logger.Error("query failed", "err", errors.New("connection refused"))

	for i := range 5 {
		logger.Warn("retrying", "attempt", i+1)
	}
	// endregion
}
