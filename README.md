# go_oj_agent

Distributed Online Judge + AI Coding Agent.

This repository is scaffolded from the project design docs and is organized around:

- Go-Kratos domain services
- gRPC + Protobuf internal contracts
- RabbitMQ-based asynchronous judging
- A Python AI agent service
- Shared infrastructure, tests, migrations, and deployment assets

## Repository Layout

- `api/` Protobuf contracts by domain
- `services/` Go business services
- `pkg/` shared Go packages
- `agent/` Python agent service
- `tests/` integration, contract, and e2e test suites
- `migrations/` database migrations
- `deploy/` deployment manifests and compose files
- `scripts/` utility scripts
- `docs/` project documentation

## Development Baseline

The first project stage uses a single Go module:

```text
github.com/viggggil/go_oj_agent
```

Core commands:

```bash
make init
make proto
make fmt
make lint
make test
make build
```

The initial GitHub Actions workflow validates:

- Proto lint
- Go format check
- go vet
- Go unit test
- Go build
