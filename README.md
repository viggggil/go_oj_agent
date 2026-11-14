# Go OJ Agent

Go OJ Agent 是一个集在线判题、比赛和 AI 编程辅助于一体的平台。当前版本提供完整的题目提交与异步判题链路、比赛排行榜、Agent 管理和流式对话，以及面向运维的监控与链路追踪。

## 当前功能

- **在线判题**：用户可浏览题目、选择编程语言并提交代码；Judge Service 通过 RabbitMQ 将任务分发给独立 Worker，在 Sandbox 中编译和运行代码，并提供提交状态与测试点结果。
- **比赛**：支持比赛管理、报名、比赛提交和排行榜。MySQL 保存成绩事实，Redis 保存可重建的排行榜投影。
- **AI 编程 Agent**：支持有界多轮对话、LangGraph ReAct、受限只读业务工具、RAG 知识检索、SSE 流式输出、停止运行、配置管理和 Eval。Agent 通过 Gateway 委托身份调用业务服务，不直接读取业务数据库。
- **平台监控**：提供指标详情、业务运行状态、Agent/Judge Trace 和日志查询；Trace 可查看链路阶段与关联日志。
- **Web 应用**：Vue 3 前端包含题库、提交记录、比赛、个人资料、Agent 对话与管理、监控页面。

## 技术栈

| 范围 | 技术 |
| --- | --- |
| 后端服务 | Go、Go-Kratos、gRPC、Protobuf |
| Agent | Python、FastAPI、LangGraph、SSE |
| 前端 | Vue 3、Pinia、Axios、Vue Router |
| 数据与消息 | MySQL、Redis、RabbitMQ、MinIO、Qdrant |
| 判题 | Go Judge Worker、隔离 Sandbox |
| 可观测性 | OpenTelemetry、Prometheus、Grafana、Loki、Tempo |
| 本地环境 | Docker Compose、Make |

主要服务包括 Gateway、User、Problem、Judge、Judge Worker、Contest 和 Agent Service。判题提交、Outbox 事件意图在同一 MySQL 事务中保存；消息按至少一次语义投递，由消费者幂等处理。

## 本地运行

需要 Go、Node.js、Python 3.12、uv 和 Docker Compose。启动本地依赖：

```bash
make infra-up
```

启动 Web 前端：

```bash
cd web
cp .env.example .env.local
npm install
npm run dev
```

开发 Agent 前，按 [Agent 服务说明](agent/README.md) 配置环境变量、Agent 数据库迁移和模型 Provider，再运行：

```bash
make agent-init
make agent-dev
```

Compose 部署、服务配置和迁移说明见[部署文档](deploy/compose/README.md)及[迁移目录说明](migrations/README.md)。

## 常用检查

```bash
make proto
make fmt-check
make vet
make test-unit
make build
make test-integration
```

Agent 检查：

```bash
make agent-check
make agent-test-unit
make agent-test-integration
```

## 文档

- [文档索引](docs/README.md)：按主题分类的架构、接口、数据、Agent、可观测性、部署和测试文档。
- [架构说明](docs/design/architecture.md) · [API 契约](docs/design/api.md) · [数据库设计](docs/design/database.md)
- [Agent 服务](agent/README.md) · [本地 Compose](deploy/compose/README.md)
- [压力测试工具说明](docs/design/load-testing.md) · [云端压力测试报告](docs/reports/cloud-load-test-2026-11-01/REPORT.md)
