package sl

import (
	"context"
	"errors"
	"log/slog"
	"slices"
)

// Failover returns a handler that tries the enabled handlers in order and
// stops at the first one that succeeds. It returns the joined errors when
// every handler fails. To send every record to all handlers instead, use
// [slog.NewMultiHandler] from the standard library.
func Failover(handlers ...slog.Handler) slog.Handler {
	return &failover{handlers: slices.Clone(handlers)}
}

type failover struct{ handlers []slog.Handler }

func (h *failover) Enabled(ctx context.Context, level slog.Level) bool {
	return slices.ContainsFunc(h.handlers, func(n slog.Handler) bool { return n.Enabled(ctx, level) })
}

func (h *failover) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, next := range h.handlers {
		if !next.Enabled(ctx, r.Level) {
			continue
		}
		err := next.Handle(ctx, r.Clone())
		if err == nil {
			return nil
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (h *failover) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &failover{handlers: mapHandlers(h.handlers, func(n slog.Handler) slog.Handler { return n.WithAttrs(attrs) })}
}

func (h *failover) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &failover{handlers: mapHandlers(h.handlers, func(n slog.Handler) slog.Handler { return n.WithGroup(name) })}
}

// Route pairs a handler with the predicate that selects its records. A nil
// Match selects every record.
type Route struct {
	Handler slog.Handler
	// Match sees the record as logged; attributes added with Logger.With are
	// not part of it.
	Match func(ctx context.Context, r slog.Record) bool
}

// Router returns a handler that sends each record to every enabled route whose
// Match reports true, and joins their errors.
func Router(routes ...Route) slog.Handler {
	return &router{routes: slices.Clone(routes)}
}

type router struct{ routes []Route }

func (h *router) Enabled(ctx context.Context, level slog.Level) bool {
	return slices.ContainsFunc(h.routes, func(rt Route) bool { return rt.Handler.Enabled(ctx, level) })
}

func (h *router) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, rt := range h.routes {
		if !rt.Handler.Enabled(ctx, r.Level) || (rt.Match != nil && !rt.Match(ctx, r)) {
			continue
		}
		if err := rt.Handler.Handle(ctx, r.Clone()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (h *router) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &router{routes: mapRoutes(h.routes, func(n slog.Handler) slog.Handler { return n.WithAttrs(attrs) })}
}

func (h *router) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &router{routes: mapRoutes(h.routes, func(n slog.Handler) slog.Handler { return n.WithGroup(name) })}
}

// Middleware wraps a handler with extra behavior.
type Middleware func(next slog.Handler) slog.Handler

// Chain wraps h with middlewares and returns the result. The first
// middleware is the outermost, so it sees each record first.
func Chain(h slog.Handler, middlewares ...Middleware) slog.Handler {
	for _, mw := range slices.Backward(middlewares) {
		h = mw(h)
	}
	return h
}

func mapHandlers(handlers []slog.Handler, fn func(slog.Handler) slog.Handler) []slog.Handler {
	result := make([]slog.Handler, len(handlers))
	for i, h := range handlers {
		result[i] = fn(h)
	}
	return result
}

func mapRoutes(routes []Route, fn func(slog.Handler) slog.Handler) []Route {
	result := make([]Route, len(routes))
	for i, rt := range routes {
		result[i] = Route{Handler: fn(rt.Handler), Match: rt.Match}
	}
	return result
}
