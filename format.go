package sl

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"
)

// Formatter rewrites one attribute. groups lists the enclosing group names,
// outermost first; it is only valid during the call and must not be kept. It
// returns the new value and true, or false to leave the attribute unchanged.
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

// maxInlineAttrs is the number of attributes that Handle formats in a stack
// buffer before it starts to allocate.
const maxInlineAttrs = 16

func (h *formatHandler) Handle(ctx context.Context, r slog.Record) error {
	var (
		attrsBuf  [maxInlineAttrs]slog.Attr
		isChanged bool
	)
	attrs := attrsBuf[:0]
	r.Attrs(func(a slog.Attr) bool {
		formatted, changed := h.format(h.groups, a)
		attrs = append(attrs, formatted)
		isChanged = isChanged || changed
		return true
	})
	if !isChanged {
		// Nothing to rewrite: pass the record on as is, without copying it.
		return h.next.Handle(ctx, r)
	}
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	out.AddAttrs(attrs...)
	return h.next.Handle(ctx, out)
}

func (h *formatHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	formatted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		formatted[i], _ = h.format(h.groups, a)
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

// format applies the formatters to a and, for a group, to its members. It
// reports whether the result differs from a, so that unchanged records can
// skip being copied.
func (h *formatHandler) format(groups []string, a slog.Attr) (slog.Attr, bool) {
	// A LogValuer is resolved once here; handing the resolved value on spares
	// the next handler from calling LogValue again.
	changed := a.Value.Kind() == slog.KindLogValuer
	a.Value = a.Value.Resolve()
	for _, f := range h.formatters {
		if v, ok := f(groups, a); ok {
			a.Value = v.Resolve()
			changed = true
		}
	}
	if a.Value.Kind() == slog.KindGroup {
		members := a.Value.Group()
		// Groups are formatted depth first, so nested levels can share one
		// buffer. It is allocated, with room to spare, only when a record
		// has a group; h.groups itself is clipped and never written to.
		inner := groups
		if len(inner) == cap(inner) {
			inner = slices.Grow(inner, 4)
		}
		inner = append(inner, a.Key)
		var formatted []slog.Attr // allocated on the first changed member
		for i, m := range members {
			fm, ok := h.format(inner, m)
			if ok && formatted == nil {
				formatted = make([]slog.Attr, len(members))
				copy(formatted, members[:i])
			}
			if formatted != nil {
				formatted[i] = fm
			}
		}
		if formatted != nil {
			a.Value = slog.GroupValue(formatted...)
			changed = true
		}
	}
	return a, changed
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
