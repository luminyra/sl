# sl

[![Go Reference](https://pkg.go.dev/badge/github.com/luminyra/sl.svg)](https://pkg.go.dev/github.com/luminyra/sl)
[![CI](https://github.com/luminyra/sl/actions/workflows/ci.yml/badge.svg)](https://github.com/luminyra/sl/actions/workflows/ci.yml)

What `log/slog` leaves out, in one dependency-free package for Go 1.27: failover and routing, middleware chains, attribute formatting, composable sampling and HTTP request logging.

```go
handler := sl.Chain(slog.NewMultiHandler(stdout, alerts),
	sl.FormatMiddleware(sl.Redact("password", "token"), sl.FormatErrors()),
	sl.SampleMiddleware(sl.AnyOf(
		sl.LevelAtLeast(slog.LevelError), // never drop errors
		sl.ThresholdSampler(time.Second, 10, sl.UniformSampler(0.01)),
	)),
)
slog.SetDefault(slog.New(handler))
```

## Install

```sh
go get github.com/luminyra/sl
```

Requires Go 1.27 or later. The module has no dependencies.

## Highlights

- **Standard library first.** Fan-out is `slog.NewMultiHandler`; sl adds `Failover`, `Router` and `Chain` for what slog does not cover.
- **Formatting at any depth.** `NewFormatHandler` rewrites attributes inside groups and `With` attributes alike, with formatters built by `ForKey`, `ForKind`, `ForType`, `Redact`, `FormatErrors` and `FormatTimes`.
- **Composable sampling.** A `Sampler` is a plain function: `UniformSampler`, `ThresholdSampler`, `LevelAtLeast`, `AllOf` and `AnyOf` combine into exactly the policy you need.
- **HTTP request logging.** `HTTPMiddleware` logs one record per request; the zero `HTTPConfig` works as is, and a `Level` function maps statuses to levels.

## Usage

```go
// Try the remote handler first and fall back to stderr.
handler := sl.Failover(remote, slog.NewTextHandler(os.Stderr, nil))

// Send audit records to their own sink.
handler = sl.Router(
	sl.Route{Handler: audit, Match: func(_ context.Context, r slog.Record) bool {
		return strings.HasPrefix(r.Message, "audit:")
	}},
	sl.Route{Handler: handler},
)

// Log HTTP requests, with 404s at Debug.
mux := http.NewServeMux()
logged := sl.HTTPMiddleware(slog.Default(), sl.HTTPConfig{
	RequestIDHeader: "X-Request-Id",
	Level: func(status int) slog.Level {
		if status == http.StatusNotFound {
			return slog.LevelDebug
		}
		return sl.StatusLevel(status)
	},
})(mux)
```

## Examples

Runnable programs live in [`examples/`](examples):

| Example | Shows |
| --- | --- |
| [`handlers`](examples/handlers/main.go) | redaction, error formatting, sampling, fan-out |
| [`http`](examples/http/main.go) | request logging with custom levels and skipped routes |

```sh
go run ./examples/handlers
```

## Design

sl is inspired by samber's [slog-multi](https://github.com/samber/slog-multi), [slog-formatter](https://github.com/samber/slog-formatter), [slog-sampling](https://github.com/samber/slog-sampling) and [slog-http](https://github.com/samber/slog-http). It targets Go 1.27 only, defers to `slog.NewMultiHandler` for fan-out, and keeps everything in a single package without dependencies.

## License

[MIT](LICENSE)
