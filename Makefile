# Development and quality-gate commands.
# Paths stay relative because the project directory may contain spaces.

GO          ?= go
GOLANGCI    ?= golangci-lint
COVER_FILE  ?= coverage.out
COVER_HTML  ?= coverage.html
AIR_VERSION ?= v1.67.4
# `go run pkg@version` pins Air without adding it to go.mod or requiring a global install.
AIR         ?= $(GO) run github.com/air-verse/air@$(AIR_VERSION)
MOCKERY_VERSION ?= v3.9.1
MOCKERY         ?= $(GO) run github.com/vektra/mockery/v3@$(MOCKERY_VERSION)

.DEFAULT_GOAL := help

.PHONY: help dev db-up db-down mocks fmt fmt-check build vet lint test test-race \
        test-coverage coverage-html coverage-check test-integration check clean

help: ## Show available targets
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z_-]+:.*## / { printf "  %-18s %s\n", $$1, $$2 }' Makefile

dev: ## Run the API with live reload (Air, config in .air.toml)
	$(AIR) -c .air.toml

db-up: ## Start MongoDB in Docker and wait until it is healthy
	docker compose up -d --wait mongo

db-down: ## Stop MongoDB (data stays in the mongo-data volume)
	docker compose down

mocks: ## Regenerate test mocks from .mockery.yml
	$(MOCKERY)

fmt: ## Format all Go files in place
	gofmt -w .

fmt-check: ## Fail if any Go file is not gofmt-formatted
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi

build: ## Compile every package
	$(GO) build ./...

vet: ## Run go vet
	$(GO) vet ./...

lint: ## Run golangci-lint (includes architecture rules via depguard)
	$(GOLANGCI) run ./...

test: ## Run unit tests
	$(GO) test ./...

test-race: ## Run unit tests with the race detector
	$(GO) test -race ./...

test-coverage: ## Run unit tests and write a coverage profile
	$(GO) test ./... -coverprofile=$(COVER_FILE)
	$(GO) tool cover -func=$(COVER_FILE)

coverage-html: test-coverage ## Render the coverage profile as HTML
	$(GO) tool cover -html=$(COVER_FILE) -o $(COVER_HTML)

coverage-check: test-coverage ## Enforce per-layer coverage thresholds
	./scripts/coverage-check.sh $(COVER_FILE)

test-integration: ## Run unit and integration tests (requires MongoDB)
	$(GO) test -tags=integration -count=1 ./...

check: fmt-check build test test-race coverage-check vet lint ## Run the full quality gate

clean: ## Remove build and coverage artifacts
	rm -rf bin $(COVER_FILE) $(COVER_HTML)
