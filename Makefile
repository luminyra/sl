# Development tasks. Run `make` or `make help` to list them.

GO    ?= go
BENCH ?= .
COUNT ?= 6
COVER_MIN ?= 95

.DEFAULT_GOAL := help
.PHONY: help fmt fmt-check tidy vet fix test race cover bench examples check clean

help: ## List the available targets
	@awk 'BEGIN { FS = ":.*## " } /^[a-z-]+:.*## / { printf "  %-10s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

fmt: ## Format all Go files
	gofmt -w .

fmt-check: ## Fail if a Go file is not formatted
	@files="$$(gofmt -l .)"; if [ -n "$$files" ]; then echo "not formatted (run make fmt):"; echo "$$files"; exit 1; fi

tidy: ## Fail if go.mod or go.sum need tidying
	$(GO) mod tidy -diff

vet: ## Run go vet
	$(GO) vet ./...

fix: ## Show the modernizations go fix would apply
	$(GO) fix -diff ./...

test: ## Run the tests
	$(GO) test -count=1 ./...

race: ## Run the tests with the race detector, in random order
	$(GO) test -race -count=1 -shuffle=on ./...

cover: ## Check the library's test coverage against COVER_MIN percent
	$(GO) test -count=1 -coverpkg=. -coverprofile=coverage.out ./...
	@total=$$($(GO) tool cover -func=coverage.out | awk '/^total:/ { sub(/%/, "", $$NF); print $$NF }'); \
	echo "coverage: $$total% of the library package (minimum $(COVER_MIN)%)"; \
	awk -v total="$$total" -v min="$(COVER_MIN)" 'BEGIN { exit !(total >= min) }' || { echo "coverage is below $(COVER_MIN)%"; exit 1; }

bench: ## Run benchmarks (BENCH=regexp, COUNT=runs) and save them to bench.txt
	$(GO) test -run='^$$' -bench='$(BENCH)' -benchmem -count=$(COUNT) . | tee bench.txt

examples: ## Run every program in examples/
	@for dir in examples/*/; do echo "== $$dir"; $(GO) run "./$$dir" || exit 1; done

check: fmt-check tidy vet race cover ## Run the same checks as CI

clean: ## Remove files created by cover and bench
	rm -f coverage.out bench.txt
