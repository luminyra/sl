# Changelog

All notable changes to this project are documented in this file. The format
is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- slog extensions: `Failover`, `Router`, `Chain`, attribute formatters, composable samplers and HTTP request logging.
- The format handler passes records through unchanged when no formatter applies, and `HTTPMiddleware` skips work when the level is disabled.
- `FormatErrors` keeps the structured details of a wrapped error that implements `slog.LogValuer`, such as an `errs.Error` wrapped with `fmt.Errorf`, by adding the members of the group it logs as.
- Benchmarks (`make bench`).
- A Makefile with the development tasks (`make help`).
- Requires Go 1.27 or later.
