GO ?= go
BUF ?= buf
PROTOC ?= protoc
KRATOS_THIRD_PARTY ?= $(shell $(GO) env GOPATH)/pkg/mod/github.com/go-kratos/kratos/v3@v3.0.0
GO_ERRORS_PLUGIN ?= github.com/go-kratos/kratos/cmd/protoc-gen-go-errors/v3@v3.0.0-20260626125723-668db92c2c00

GO_PACKAGES := ./...
GO_FILES := $(shell git ls-files '*.go')
API_PROTO_FILES := $(shell find api -name '*.proto' -type f | sort)
BUF_GENERATE_PATHS := --path api --path services/user/internal/conf --path services/gateway/internal/conf

.PHONY: init proto generate validate errors fmt fmt-check lint vet build test test-unit test-integration test-e2e agent-eval infra-up infra-down dev

init:
	@$(GO) version
	@$(BUF) --version

proto:
	@$(BUF) dep update
	@$(BUF) lint

generate:
	@$(BUF) dep update
	@GOBIN=/tmp $(GO) install $(GO_ERRORS_PLUGIN)
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
