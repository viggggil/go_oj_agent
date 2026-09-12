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
- `web/` Vue 3 + Pinia + Axios authentication frontend
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

前端认证页面位于 `web/`，包含注册、登录和个人资料页面。启动后端 Compose 环境后执行：

```bash
cd web
cp .env.example .env.local
npm install
npm run dev
```

默认前端地址为 `http://127.0.0.1:5173`，通过 `VITE_API_BASE_URL` 指向 Gateway（默认 `http://127.0.0.1:8080`）。

The initial GitHub Actions workflow validates:

- Proto lint
- Go format check
- go vet
- Go unit test
- Go build
