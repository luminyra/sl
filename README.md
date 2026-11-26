<img align="right" src="https://images.luminyra.dev/luminyra/github/sl/logo.webp" alt="Stacked log slips with a charcoal redaction bar and a vermilion error marker." width="96" height="96">

# sl

[![Go Reference](https://pkg.go.dev/badge/github.com/luminyra/sl.svg)](https://pkg.go.dev/github.com/luminyra/sl)
[![CI](https://github.com/luminyra/sl/actions/workflows/ci.yml/badge.svg)](https://github.com/luminyra/sl/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/luminyra/sl)](https://goreportcard.com/report/github.com/luminyra/sl)
[![Go 1.27+](https://img.shields.io/badge/Go-1.27%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/doc/devel/release)

sl adds the pieces that production logging needs and the standard `log/slog` package leaves out: hiding secrets, sampling noisy messages, sending records to different destinations, falling back when a destination fails, and logging HTTP requests.

Every piece is an ordinary `slog.Handler` or a function that wraps one, so sl works with any handler you already use, from `slog.NewJSONHandler` to a third-party exporter.

- **Redaction and formatting.** Hide passwords and tokens at any depth, and turn errors, times or your own types into readable values.
- **Composable sampling.** A sampler is a plain function, so "keep every error, and the first ten copies of anything else per second" is one expression.
- **Routing and failover.** Send records to the handlers that match them, or try a backup when the primary destination is down.
- **HTTP request logs.** One structured record per request, with the level chosen by the response status.
- **Pay only for what you use.** Records with nothing to rewrite pass through unchanged, and HTTP requests at a disabled level cost about 55 ns.

<p align="center">
  <img src="https://images.luminyra.dev/luminyra/github/sl/cover.webp" alt="A log press processes a paper slip beside labeled log cards." width="640">
</p>

## Contents

- [When to use it](#when-to-use-it) · [Install](#install) · [Getting started](#getting-started)
- [Common tasks](#common-tasks) · [How it works](#how-it-works) · [Performance](#performance) · [API at a glance](#api-at-a-glance)

## When to use it

Use sl when your program logs with `log/slog` and you need any of the above without adopting a different logging library.

If all you need is to send every record to several handlers, the standard library already does it: use `slog.NewMultiHandler` (Go 1.26+). For shipping logs to a backend in bulk, a log collector such as Vector or the OpenTelemetry Collector is usually a better place than your process.

## Install

```sh
go get github.com/luminyra/sl
```

sl requires Go 1.27 or later and has no dependencies.

## Getting started

Copy the program into `main.go` and run it with `go run .`:

```go
package main

import (
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/luminyra/sl"
)

func main() {
	// The handler that finally writes the records; the time is dropped to
	// keep the output stable.
	text := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})

	// Wrap it: hide secrets, turn errors into readable groups, and keep only
	// the first two copies of a repeated message per second, but every error.
	handler := sl.Chain(text,
		sl.FormatMiddleware(sl.Redact("password"), sl.FormatErrors()),
		sl.SampleMiddleware(sl.AnyOf(
			sl.LevelAtLeast(slog.LevelError),
			sl.ThresholdSampler(time.Second, 2, nil),
		)),
	)
	logger := slog.New(handler)

	logger.Info("user signed in", "user", "alice", "password", "hunter2")
	for attempt := range 4 {
		logger.Warn("retrying", "attempt", attempt+1)
	}
	logger.Error("giving up", "err", errors.New("connection refused"))
}
```

```text
level=INFO msg="user signed in" user=alice password=*******
level=WARN msg=retrying attempt=1
level=WARN msg=retrying attempt=2
level=ERROR msg="giving up" err.message="connection refused" err.type=*errors.errorString
```

The password was masked, the third and fourth "retrying" records were sampled away, and the error was kept and expanded. `Chain` applies middleware from the outside in: the first one sees each record first.

<p align="center">
  <a href="https://images.luminyra.dev/luminyra/github/sl/demo.webp"><img src="https://images.luminyra.dev/luminyra/github/sl/demo.webp" alt="The logging example masks sensitive fields, keeps the first two repeated warnings and preserves the error record." width="760"></a>
</p>

## Common tasks

### Hide secrets

```go
handler = sl.NewFormatHandler(handler, sl.Redact("password", "token", "authorization"))
```

`Redact` matches keys at any depth, including inside groups and attributes added with `Logger.With`.

### Format values your way

```go
handler = sl.NewFormatHandler(handler,
	sl.FormatErrors(),                          // errors become {message, type}
	sl.FormatTimes(time.RFC3339, time.UTC),     // times become UTC strings
	sl.ForType(func(u User) slog.Value {        // your own types
		return slog.GroupValue(slog.Int("id", u.ID))
	}),
	sl.ForKey("email", func(v slog.Value) slog.Value { return slog.StringValue(maskEmail(v.String())) }),
)
```

`ForKind` matches by `slog.Kind`, and any `func(groups []string, a slog.Attr) (slog.Value, bool)` is a `Formatter`.

### Sample noisy logs without losing errors

```go
sampler := sl.AnyOf(
	sl.LevelAtLeast(slog.LevelError), // always keep errors
	sl.ThresholdSampler(time.Second, 10, sl.UniformSampler(0.01)),
	// otherwise keep the first 10 copies of each message per second, then 1%
)
handler = sl.NewSampleHandler(handler, sampler)
```

`AllOf` keeps a record only if every sampler keeps it, and a `Sampler` is any `func(context.Context, slog.Record) bool`.

### Send records to different destinations

```go
handler := sl.Router(
	sl.Route{Handler: auditHandler, Match: func(_ context.Context, r slog.Record) bool {
		return strings.HasPrefix(r.Message, "audit:")
	}},
	sl.Route{Handler: appHandler}, // a nil Match receives every record
)
```

To send every record everywhere, use `slog.NewMultiHandler(a, b)` from the standard library.

### Fall back when a destination fails

```go
handler := sl.Failover(remoteHandler, slog.NewJSONHandler(os.Stderr, nil))
```

Each record goes to the first enabled handler that accepts it without an error.

### Log HTTP requests

```go
logged := sl.HTTPMiddleware(logger, sl.HTTPConfig{
	RequestIDHeader: "X-Request-Id",
	Skip:            func(r *http.Request) bool { return r.URL.Path == "/healthz" },
})(mux)
http.ListenAndServe(":8080", logged)
```

Each request produces one record with `request.method`, `request.path`, `request.query`, `request.ip`, `request.user_agent`, `response.status`, `response.bytes` and `response.latency`. Responses below 400 log at Info, 4xx at Warn and 5xx at Error; set `HTTPConfig.Level` to change that.

### Write your own middleware

A `Middleware` is `func(next slog.Handler) slog.Handler`, so anything that wraps a handler plugs into `Chain`:

```go
handler := sl.Chain(base, addRequestID, sl.FormatMiddleware(sl.Redact("token")))
```

## How it works

- Formatting runs on every attribute, including attributes inside groups and those added with `Logger.With` and `Logger.WithGroup`, so a secret cannot slip through a nested group.
- Where a record goes to several handlers (`Router`, `Failover`), each handler receives its own clone, so one handler's changes cannot leak into another's.
- Samplers run on the record as logged. Attributes added with `Logger.With` are already part of the handler, not the record.
- `HTTPMiddleware` writes its record after the handler returns; a request whose handler panics is not logged. The response writer it passes on supports `http.ResponseController` and `http.Flusher`.
- Handlers from this package are safe for concurrent use whenever the handlers they wrap are.

## Performance

Measured with the benchmarks in this repository (`make bench`; Go 1.27.4, linux/arm64, 5 CPUs, median of 6 runs). Run them on your hardware for numbers that matter to you.

| Setup | Time | Allocations |
| --- | --- | --- |
| `slog.NewJSONHandler` alone, four attributes | 433 ns | 0 |
| … wrapped in `NewFormatHandler`, nothing to rewrite | 512 ns | 1 (a group in the record) |
| … with `Redact` and `FormatErrors` rewriting values | 800 ns | 5 |
| `net/http` handler alone | 345 ns | 10 |
| … wrapped in `HTTPMiddleware`, logging | 1.11 µs | 13 |
| … wrapped in `HTTPMiddleware`, level disabled | 399 ns | 11 |
| Sampling away a record | 194 ns | 0 |

The format handler copies a record only when a formatter changes something, and the HTTP middleware checks the level before building any attribute.

## API at a glance

| Task | API |
| --- | --- |
| Compose handlers | `Chain`, `Middleware`, `Router`, `Route`, `Failover` |
| Format attributes | `NewFormatHandler`, `FormatMiddleware`, `Formatter`, `Redact`, `FormatErrors`, `FormatTimes`, `ForKey`, `ForKind`, `ForType` |
| Sample records | `NewSampleHandler`, `SampleMiddleware`, `Sampler`, `UniformSampler`, `ThresholdSampler`, `LevelAtLeast`, `AllOf`, `AnyOf` |
| HTTP | `HTTPMiddleware`, `HTTPConfig`, `StatusLevel` |

The full reference with runnable examples is on [pkg.go.dev](https://pkg.go.dev/github.com/luminyra/sl). Complete programs live in [`examples/`](examples): [`handlers`](examples/handlers/main.go) and [`http`](examples/http/main.go).

## Versioning

sl follows [semantic versioning](https://semver.org). Until v1.0.0, a minor release may change the API; every change is listed in the [changelog](CHANGELOG.md). It supports the two most recent Go releases, starting with Go 1.27.

## Contributing

Questions, bug reports and pull requests are welcome. See the [contributing guide](https://github.com/luminyra/.github/blob/main/CONTRIBUTING.md), and report security issues as described in the [security policy](https://github.com/luminyra/.github/blob/main/SECURITY.md).

sl was inspired by samber's [slog-multi](https://github.com/samber/slog-multi), [slog-formatter](https://github.com/samber/slog-formatter), [slog-sampling](https://github.com/samber/slog-sampling) and [slog-http](https://github.com/samber/slog-http). It is released under the [MIT License](LICENSE).
