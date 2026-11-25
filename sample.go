package sl

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"slices"
	"sync"
	"time"
)

// Sampler reports whether a record should be kept.
type Sampler func(ctx context.Context, r slog.Record) bool

// NewSampleHandler returns a handler that passes to next only the records
// kept by sampler.
func NewSampleHandler(next slog.Handler, sampler Sampler) slog.Handler {
	return &sampleHandler{next: next, sampler: sampler}
}

// SampleMiddleware returns a Middleware that wraps a handler with
// NewSampleHandler.
func SampleMiddleware(sampler Sampler) Middleware {
	return func(next slog.Handler) slog.Handler { return NewSampleHandler(next, sampler) }
}

type sampleHandler struct {
	next    slog.Handler
	sampler Sampler
}

func (h *sampleHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *sampleHandler) Handle(ctx context.Context, r slog.Record) error {
	if !h.sampler(ctx, r) {
		return nil
	}
	return h.next.Handle(ctx, r)
}

func (h *sampleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &sampleHandler{next: h.next.WithAttrs(attrs), sampler: h.sampler}
}

func (h *sampleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &sampleHandler{next: h.next.WithGroup(name), sampler: h.sampler}
}

// UniformSampler keeps each record with probability rate. A rate of 0 or less drops
// every record and a rate of 1 or more keeps every record.
func UniformSampler(rate float64) Sampler {
	return func(context.Context, slog.Record) bool {
		return rate >= 1 || (rate > 0 && rand.Float64() < rate)
	}
}

// ThresholdSampler keeps the first threshold records of each level and message
// pair in every interval and passes the rest to then, which decides whether
// to keep them; a nil then drops them. Counters reset at the start of each
// interval. ThresholdSampler panics if interval is not positive.
//
//	sl.ThresholdSampler(time.Second, 10, sl.UniformSampler(0.01))
func ThresholdSampler(interval time.Duration, threshold uint64, then Sampler) Sampler {
	if interval <= 0 {
		panic("sl: ThresholdSampler interval must be positive")
	}
	var (
		mu     sync.Mutex
		start  time.Time
		counts = make(map[thresholdKey]uint64)
	)
	return func(ctx context.Context, r slog.Record) bool {
		mu.Lock()
		if now := time.Now(); now.Sub(start) >= interval {
			start = now
			clear(counts)
		}
		key := thresholdKey{level: r.Level, message: r.Message}
		counts[key]++
		n := counts[key]
		mu.Unlock()
		return n <= threshold || (then != nil && then(ctx, r))
	}
}

type thresholdKey struct {
	level   slog.Level
	message string
}

// AllOf returns a Sampler that keeps a record only when every sampler keeps
// it. It stops at the first sampler that drops the record.
func AllOf(samplers ...Sampler) Sampler {
	samplers = slices.Clone(samplers)
	return func(ctx context.Context, r slog.Record) bool {
		for _, s := range samplers {
			if !s(ctx, r) {
				return false
			}
		}
		return true
	}
}

// AnyOf returns a Sampler that keeps a record when at least one sampler keeps
// it. It stops at the first sampler that keeps the record.
func AnyOf(samplers ...Sampler) Sampler {
	samplers = slices.Clone(samplers)
	return func(ctx context.Context, r slog.Record) bool {
		for _, s := range samplers {
			if s(ctx, r) {
				return true
			}
		}
		return false
	}
}

// LevelAtLeast returns a Sampler that keeps records at level or above. Combine
// it with AnyOf to never sample away errors:
//
//	sl.AnyOf(sl.LevelAtLeast(slog.LevelError), sl.UniformSampler(0.1))
func LevelAtLeast(level slog.Level) Sampler {
	return func(_ context.Context, r slog.Record) bool {
		return r.Level >= level
	}
}
