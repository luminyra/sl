package sl

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"
)

// Formatter rewrites one attribute. groups lists the enclosing group names,
// outermost first. It returns the new value and true, or false to leave the
// attribute unchanged.
type Formatter func(groups []string, a slog.Attr) (slog.Value, bool)

// NewFormatHandler returns a handler that applies formatters, then passes
// records to next.
func NewFormatHandler(next slog.Handler, formatters ...Formatter) slog.Handler {
	return &formatHandler{next: next, formatters: slices.Clone(formatters)}
}

// FormatMiddleware returns a Middleware that wraps a handler with
// NewFormatHandler.
func FormatMiddleware(formatters ...Formatter) Middleware {
	formatters = slices.Clone(formatters)
	return func(next slog.Handler) slog.Handler {
		return &formatHandler{next: next, formatters: formatters}
	}
}

type formatHandler struct {
	next       slog.Handler
	formatters []Formatter
	groups     []string
}

func (h *formatHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *formatHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.format(h.groups, a))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h *formatHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	formatted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		formatted[i] = h.format(h.groups, a)
	}
	return &formatHandler{next: h.next.WithAttrs(formatted), formatters: h.formatters, groups: h.groups}
}

func (h *formatHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &formatHandler{
		next:       h.next.WithGroup(name),
		formatters: h.formatters,
		groups:     append(slices.Clip(h.groups), name),
	}
}

func (h *formatHandler) format(groups []string, a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	for _, f := range h.formatters {
		if v, ok := f(groups, a); ok {
			a.Value = v.Resolve()
		}
	}
	if a.Value.Kind() == slog.KindGroup {
		inner := append(slices.Clip(groups), a.Key)
		members := a.Value.Group()
		formatted := make([]slog.Attr, len(members))
		for i, m := range members {
			formatted[i] = h.format(inner, m)
		}
		a.Value = slog.GroupValue(formatted...)
	}
	return a
}

// ForKey rewrites attributes with the given key at any depth.
func ForKey(key string, fn func(slog.Value) slog.Value) Formatter {
	return func(_ []string, a slog.Attr) (slog.Value, bool) {
		if a.Key != key {
			return a.Value, false
		}
		return fn(a.Value), true
	}
}

// ForKind rewrites attributes whose value has the given kind.
func ForKind(kind slog.Kind, fn func(slog.Value) slog.Value) Formatter {
	return func(_ []string, a slog.Attr) (slog.Value, bool) {
		if a.Value.Kind() != kind {
			return a.Value, false
		}
		return fn(a.Value), true
	}
}

// ForType rewrites attributes whose value, stored with [slog.Any], is of
// type T or, when T is an interface, implements it.
func ForType[T any](fn func(T) slog.Value) Formatter {
	return func(_ []string, a slog.Attr) (slog.Value, bool) {
		if a.Value.Kind() != slog.KindAny {
			return a.Value, false
		}
		v, ok := a.Value.Any().(T)
		if !ok {
			return a.Value, false
		}
		return fn(v), true
	}
}

// Redact replaces the values of the given keys with "*******" at any depth.
func Redact(keys ...string) Formatter {
	keys = slices.Clone(keys)
	return func(_ []string, a slog.Attr) (slog.Value, bool) {
		if !slices.Contains(keys, a.Key) {
			return a.Value, false
		}
		return slog.StringValue("*******"), true
	}
}

// FormatErrors turns error values into a group with the error message and
// its Go type.
func FormatErrors() Formatter {
	return ForType(func(err error) slog.Value {
		return slog.GroupValue(
			slog.String("message", err.Error()),
			slog.String("type", fmt.Sprintf("%T", err)),
		)
	})
}

// FormatTimes formats time values with layout in loc. A nil loc keeps each
// time's own location.
func FormatTimes(layout string, loc *time.Location) Formatter {
	return ForKind(slog.KindTime, func(v slog.Value) slog.Value {
		t := v.Time()
		if loc != nil {
			t = t.In(loc)
		}
		return slog.StringValue(t.Format(layout))
	})
}
