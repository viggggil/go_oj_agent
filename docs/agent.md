# Project Agent Guide

## 1. Project Overview

本项目是一个 **Distributed Online Judge + AI Coding Agent**。

核心业务：

- 用户、题目、提交、比赛。
- RabbitMQ 异步判题。
- Go Judge Worker + Sandbox。
- FastAPI + LangChain/LangGraph Agent。

目标用户主要是编程学习者和题目管理员。

---

## 2. Tech Stack

```text
Go / Go-Kratos
gRPC / Protobuf
Python / FastAPI / LangChain / LangGraph

MySQL
Redis
RabbitMQ
MinIO
etcd
Chroma / Qdrant

OpenTelemetry
Prometheus
Grafana
Jaeger / Tempo

Docker Compose
GitHub Actions
```

---

## 3. Repository Structure

```text
api/        Protobuf contracts
services/   Go services
agent/      Python Agent service
pkg/        shared Go packages
tests/      integration / contract / e2e
migrations/ database migrations
deploy/     deployment files
docs/       design documents
```

---

## 4. Architecture

Go Service dependency:

```text
Transport
  ↓
Service
  ↓
Biz / Use Case
  ↓
Repository Interface
  ↑
Data Implementation
```

Rules:

- Service owns its data.
- No cross-service direct DB access.
- Sync communication: gRPC.
- Async communication: RabbitMQ.
- Agent accesses business data only through gRPC Tools.
- DB state + reliable MQ event uses Transactional Outbox.

---

## 5. Domain Rules

- MySQL is the source of truth.
- Redis is cache / realtime state, not the only business store.
- RabbitMQ uses at-least-once delivery; consumers must be idempotent.
- Judge user code is untrusted.
- Final authorization happens in the owning Go service.
- LLM / Prompt is never a security boundary.

---

## 6. Coding Conventions

Go:

- Pass `context.Context` through IO calls.
- Keep concurrency bounded.
- Handle errors; do not silently ignore important errors.
- Do not put SQL or MQ SDK calls in `biz`.
- Do not manually edit generated code.

General:

- Prefer existing abstractions and dependencies.
- Keep diffs focused on the task.
- Do not add unrelated refactors.
- Do not log secrets, tokens, passwords or private keys.

---

## 7. Development Commands

Use repository `Makefile` as the source of truth.

Expected commands:

```bash
make init
make proto
make fmt
make lint
make build
make test
make test-unit
make test-integration
make test-e2e
make agent-eval
make infra-up
make infra-down
make dev
```

If a command does not exist, inspect the repository before inventing a new workflow.

---

## 8. Testing

New behavior requires tests.

- Business logic → Unit Test.
- MySQL / Redis / RabbitMQ / MinIO / gRPC → Integration Test.
- Proto / Event changes → Contract Test.
- Judge parser / comparator → consider Fuzz Test.
- Agent changes → Agent Eval.
- Security changes → Negative Test.

Never delete or weaken a valid test just to make CI pass.

---

## 9. API Conventions

- External: REST + SSE.
- Internal: gRPC + Protobuf.
- Proto packages are versioned.
- Never reuse removed protobuf field numbers.
- Use `reserved` for removed fields.
- Event schema is independent from ORM models.
- API changes must consider Go and Python clients.

---

## 10. Database Conventions

- A service may only access its own schema/tables.
- Schema changes require migrations.
- Cross-service data uses gRPC or events.
- Check index, transaction, unique constraint and pagination.
- Reliable `DB change + MQ event` uses Transactional Outbox.
- Redis keys must define owner, TTL and invalidation behavior.

---

## 11. Security

Never:

- Commit secrets.
- Trust user code.
- Run Judge code with host network / privileged access.
- Concatenate untrusted input into shell commands.
- Let Agent directly query business databases.
- Let LLM decide authorization.
- Log passwords, JWTs, API keys or private keys.

---

## 12. Git / PR Workflow

Use short-lived branches:

```text
feat/*
fix/*
refactor/*
test/*
docs/*
chore/*
ci/*
perf/*
```

Commit format:

```text
type(scope): description
```

Example:

```text
feat(submission): add transactional outbox
fix(judge): handle duplicate result event
test(agent): add authorization eval
```

Prefer PR + CI + Squash Merge into `main`.

### 12.1 Change Routing

Use the following routing rule to decide whether a change needs an Issue before implementation:

| Change Type | Workflow |
| --- | --- |
| Documentation-only updates | Commit directly to `main` when the change does not affect code, contracts, database schema, CI behavior, or release process |
| Small bug fixes | Branch -> PR -> CI -> Merge, usually without a separate Issue |
| Simple configuration changes | Branch -> PR -> CI -> Merge, usually without a separate Issue |
| Features, architecture changes, database changes, API changes | Issue -> Branch -> PR -> CI -> Review -> Merge |

Guidelines:

- Keep documentation updates direct when they only clarify project notes, plans, or guidance.
- Keep small updates lightweight when the scope is clear and the risk is low.
- Use an Issue when the change affects contracts, data shape, service boundaries, or multi-step coordination.
- Keep the Issue, branch name, commit, and PR title aligned for traceability.
- Do not skip CI before merge, even for tiny changes.
- After a PR is merged remotely, fetch/prune and sync the local `main` branch to the latest `origin/main` before starting the next branch.

Do not force-push, rewrite history, merge PR, create tags/releases or push directly to `main` unless explicitly asked.

---

## 13. Definition of Done

Before finishing a task:

- [ ] Requirement is satisfied.
- [ ] Service/data boundaries are preserved.
- [ ] Error and failure paths are handled.
- [ ] Relevant tests are added and executed when possible.
- [ ] Security-sensitive changes include negative tests.
- [ ] API/Event/Migration changes are updated.
- [ ] Documentation is updated when behavior changes.
- [ ] No secrets or unintended files are included.
- [ ] Final diff has been reviewed.

Report what changed, tests executed, limitations and follow-ups. Never claim tests passed if they were not run.

---

## 14. Do Not

Do not:

- Access another service's database directly.
- Bypass gRPC/Event boundaries for convenience.
- Replace core infrastructure without an explicit requirement/ADR.
- Create unbounded goroutines, channels or retries.
- ACK RabbitMQ messages before the business action succeeds.
- Manually edit generated Protobuf/OpenAPI code.
- Expand task scope with unrelated cleanup.
- Weaken CI gates to make current code pass.
- Guess high-risk architecture or security decisions when repository evidence conflicts with the task.
