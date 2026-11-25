// Package sl extends [log/slog] for Go 1.27 with what the standard library
// does not provide: failover and routing between handlers, middleware
// chains, attribute formatting, composable sampling and net/http request
// logging. It is a single package that depends only on the standard library.
//
// # Composition
//
// [Failover] tries handlers in order, [Router] sends records to the handlers
// whose [Route] matches, and [Chain] wraps a handler with [Middleware]. To
// send every record to several handlers, use [log/slog.NewMultiHandler].
//
// # Formatting
//
// [NewFormatHandler] and [FormatMiddleware] rewrite attributes at any depth
// with [Formatter] values built by [ForKey], [ForKind], [ForType], [Redact],
// [FormatErrors] and [FormatTimes].
//
// # Sampling
//
// A [Sampler] is a plain function, so samplers compose. [UniformSampler]
// keeps a fraction of records, [ThresholdSampler] keeps the first records of
// each message per interval and hands the rest to another Sampler,
// [LevelAtLeast] keeps records by level, and [AllOf] and [AnyOf] combine
// them:
//
//	sampler := sl.AnyOf(
//		sl.LevelAtLeast(slog.LevelError), // never drop errors
//		sl.ThresholdSampler(time.Second, 10, sl.UniformSampler(0.01)),
//	)
//	logger := slog.New(sl.NewSampleHandler(handler, sampler))
//
// # HTTP
//
// [HTTPMiddleware] logs one record per request. The zero [HTTPConfig] is
// ready to use, and its Level function maps response statuses to levels,
// [StatusLevel] by default.
//
// Where records fan out, each handler receives its own clone of the record.
// Handlers returned by this package are safe for concurrent use when the
// wrapped handlers are.
//
// The package is inspired by samber's slog-multi, slog-formatter,
// slog-sampling and slog-http.
package sl
