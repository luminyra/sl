package sl

import (
	"bytes"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func newRecord(level slog.Level, msg string) slog.Record {
	return slog.NewRecord(time.Time{}, level, msg, 0)
}

func TestUniformBounds(t *testing.T) {
	ctx := t.Context()
	r := newRecord(slog.LevelInfo, "x")
	keepAll, dropAll, half := UniformSampler(1), UniformSampler(0), UniformSampler(0.5)
	kept := 0
	for range 10000 {
		if !keepAll(ctx, r) || dropAll(ctx, r) {
			t.Fatal("UniformSampler(1) must keep and UniformSampler(0) must drop every record")
		}
		if half(ctx, r) {
			kept++
		}
	}
	if kept < 4000 || kept > 6000 {
		t.Fatalf("UniformSampler(0.5) kept %d of 10000", kept)
	}
}

func TestThresholdPerMessageAndReset(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sampler := ThresholdSampler(time.Second, 2, nil)
		ctx := t.Context()
		a, b := newRecord(slog.LevelInfo, "a"), newRecord(slog.LevelInfo, "b")

		got := []bool{sampler(ctx, a), sampler(ctx, a), sampler(ctx, a), sampler(ctx, b)}
		if expected := []bool{true, true, false, true}; !slices.Equal(got, expected) {
			t.Fatalf("first interval = %v, expected %v", got, expected)
		}
		time.Sleep(time.Second)
		if !sampler(ctx, a) {
			t.Fatal("counter did not reset in the next interval")
		}
	})
}

func TestThresholdHandsOverflowToNextSampler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		r := newRecord(slog.LevelInfo, "a")
		keepOverflow := ThresholdSampler(time.Second, 1, UniformSampler(1))
		dropOverflow := ThresholdSampler(time.Second, 1, UniformSampler(0))
		for range 3 {
			keepOverflow(ctx, r)
			dropOverflow(ctx, r)
		}
		if !keepOverflow(ctx, r) || dropOverflow(ctx, r) {
			t.Fatal("overflow records must be decided by the next sampler")
		}
	})
}

func TestThresholdPanicsOnBadInterval(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ThresholdSampler(0) did not panic")
		}
	}()
	ThresholdSampler(0, 1, nil)
}

func TestCombinators(t *testing.T) {
	ctx := t.Context()
	keep, drop := UniformSampler(1), UniformSampler(0)
	info, failure := newRecord(slog.LevelInfo, "x"), newRecord(slog.LevelError, "x")

	tests := []struct {
		name     string
		sampler  Sampler
		record   slog.Record
		expected bool
	}{
		{name: "all of keeps", sampler: AllOf(keep, keep), record: info, expected: true},
		{name: "all of drops", sampler: AllOf(keep, drop), record: info, expected: false},
		{name: "all of nothing keeps", sampler: AllOf(), record: info, expected: true},
		{name: "any of keeps", sampler: AnyOf(drop, keep), record: info, expected: true},
		{name: "any of drops", sampler: AnyOf(drop, drop), record: info, expected: false},
		{name: "errors always kept", sampler: AnyOf(LevelAtLeast(slog.LevelError), drop), record: failure, expected: true},
		{name: "info sampled away", sampler: AnyOf(LevelAtLeast(slog.LevelError), drop), record: info, expected: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sampler(ctx, tt.record); got != tt.expected {
				t.Errorf("got %t, expected %t", got, tt.expected)
			}
		})
	}
}

func TestSampleHandler(t *testing.T) {
	var buf bytes.Buffer
	text := slog.NewTextHandler(&buf, nil)
	logger := slog.New(SampleMiddleware(UniformSampler(0))(text)).With("k", "v")
	logger.Info("dropped")
	slog.New(NewSampleHandler(text, UniformSampler(1))).WithGroup("g").Info("kept", "n", 1)
	slog.New(NewSampleHandler(text, UniformSampler(1))).WithGroup("").Info("ungrouped")

	got := buf.String()
	if strings.Contains(got, "dropped") || !strings.Contains(got, "msg=kept g.n=1") || !strings.Contains(got, "ungrouped") {
		t.Fatalf("output = %q", got)
	}
}

func BenchmarkSampleHandler(b *testing.B) {
	sampler := AnyOf(LevelAtLeast(slog.LevelError), ThresholdSampler(time.Hour, 1, nil))
	logger := slog.New(NewSampleHandler(slog.NewJSONHandler(io.Discard, nil), sampler))
	logger.Info("noisy") // the first copy is kept; the rest are dropped
	for b.Loop() {
		logger.Info("noisy", "n", 1)
	}
}
