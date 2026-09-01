GO ?= go
BUF ?= buf

GO_PACKAGES := ./...
GO_FILES := $(shell git ls-files '*.go')

.PHONY: init proto fmt fmt-check lint vet build test test-unit test-integration test-e2e agent-eval infra-up infra-down dev

init:
	@$(GO) version
	@$(BUF) --version

proto:
	@$(BUF) dep update
	@$(BUF) lint

fmt:
	@if [ -n "$(GO_FILES)" ]; then \
		gofmt -w $(GO_FILES); \
	fi

fmt-check:
	@if [ -n "$(GO_FILES)" ]; then \
		unformatted="$$(gofmt -l $(GO_FILES))"; \
		if [ -n "$$unformatted" ]; then \
			printf 'gofmt required for:\n%s\n' "$$unformatted"; \
			exit 1; \
		fi; \
	fi

lint:
	@$(MAKE) proto
	@$(MAKE) fmt-check
	@$(MAKE) vet

vet:
	@$(GO) vet $(GO_PACKAGES)

build:
	@$(GO) build $(GO_PACKAGES)

test:
	@$(GO) test $(GO_PACKAGES)

test-unit:
	@$(GO) test $(GO_PACKAGES)

test-integration:
	@echo "integration tests are not wired yet"

test-e2e:
	@echo "e2e tests are not wired yet"

agent-eval:
	@echo "agent evaluation is not wired yet"

infra-up:
	@echo "infra bootstrap is not wired yet"

infra-down:
	@echo "infra teardown is not wired yet"

dev:
	@echo "dev workflow is not wired yet"
