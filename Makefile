GO ?= go
BUF ?= buf
PROTOC ?= protoc
KRATOS_THIRD_PARTY ?= $(shell $(GO) env GOPATH)/pkg/mod/github.com/go-kratos/kratos/v3@v3.0.0
GO_ERRORS_PLUGIN ?= github.com/go-kratos/kratos/cmd/protoc-gen-go-errors/v3@v3.0.0-20260626125723-668db92c2c00
GO_HTTP_PLUGIN ?= github.com/go-kratos/kratos/cmd/protoc-gen-go-http/v3@v3.0.0-20260626125723-668db92c2c00
COMPOSE ?= docker compose
COMPOSE_FILE ?= deploy/compose/compose.yaml
UV ?= uv

GO_PACKAGES := ./...
GO_FILES := $(shell git ls-files '*.go')
API_PROTO_FILES := $(shell find api -name '*.proto' -type f | sort)
BUF_GENERATE_PATHS := --path api --path services/user/internal/conf --path services/problem/internal/conf --path services/gateway/internal/conf --path services/judge/internal/conf --path services/contest/internal/conf

.PHONY: init proto generate validate errors fmt fmt-check lint vet build test test-unit test-integration test-e2e agent-init agent-dev agent-proto agent-fmt agent-fmt-check agent-lint agent-type agent-check agent-test-unit agent-test-integration agent-eval infra-up infra-down dev

init:
	@$(GO) version
	@$(BUF) --version

proto:
	@$(BUF) dep update
	@$(BUF) lint

generate:
	@$(BUF) dep update
	@GOBIN=/tmp $(GO) install $(GO_ERRORS_PLUGIN)
	@GOBIN=/tmp $(GO) install $(GO_HTTP_PLUGIN)
	@PATH=/tmp:$$PATH $(BUF) generate $(BUF_GENERATE_PATHS)

# 使用 protoc 生成 API 参数校验代码。
validate:
	@command -v $(PROTOC) >/dev/null || (echo "需要安装 protoc 才能执行 validate" && exit 1)
	@$(PROTOC) \
		--proto_path=. \
		--proto_path=./third_party \
		--proto_path=$(KRATOS_THIRD_PARTY) \
		--plugin=protoc-gen-go=$(shell command -v protoc-gen-go) \
		--plugin=protoc-gen-validate=$${PROTOC_GEN_VALIDATE:-$$(command -v protoc-gen-validate)} \
		--go_out=paths=source_relative:. \
		--validate_out=paths=source_relative,lang=go:. \
		$(API_PROTO_FILES)

# 使用 Kratos errors 插件生成 Proto 错误代码。
errors:
	@command -v $(PROTOC) >/dev/null || (echo "需要安装 protoc 才能执行 errors" && exit 1)
	@GOBIN=/tmp $(GO) install $(GO_ERRORS_PLUGIN)
	@$(PROTOC) \
		--proto_path=. \
		--proto_path=./third_party \
		--proto_path=$(KRATOS_THIRD_PARTY) \
		--plugin=protoc-gen-go=$(shell command -v protoc-gen-go) \
		--plugin=protoc-gen-go-errors=/tmp/protoc-gen-go-errors \
		--go_out=paths=source_relative:. \
		--go-errors_out=paths=source_relative:. \
		$(API_PROTO_FILES)

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
	@./tests/integration/run.sh

test-e2e:
	@./tests/integration/run.sh

agent-eval:
	@echo "agent evaluation is not wired yet"

agent-init:
	@$(UV) sync --directory agent --frozen

agent-dev:
	@$(UV) run --directory agent --frozen python -m app

agent-proto:
	@$(UV) run --directory agent --frozen python scripts/generate_grpc.py

agent-fmt:
	@$(UV) run --directory agent --frozen ruff format --exclude app/grpcgen app tests

agent-fmt-check:
	@$(UV) run --directory agent --frozen ruff format --check --exclude app/grpcgen app tests

agent-lint:
	@$(UV) run --directory agent --frozen ruff check app tests

agent-type:
	@$(UV) run --directory agent --frozen mypy app tests

agent-check: agent-fmt-check agent-lint agent-type

agent-test-unit:
	@$(UV) run --directory agent --frozen pytest -m 'not integration'

agent-test-integration:
	@UV="$(UV)" ./tests/agent/run.sh

infra-up:
	@$(COMPOSE) -f $(COMPOSE_FILE) up --build --detach --wait

infra-down:
	@$(COMPOSE) -f $(COMPOSE_FILE) --profile agent down

dev:
	@echo "dev workflow is not wired yet"
