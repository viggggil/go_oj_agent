# Go OJ Agent

Go OJ Agent 是一个面向编程学习者和题目管理员的分布式在线判题与智能编程平台。项目把题目管理、代码提交、自动判题、比赛排行榜和 Agent 服务放在同一套可扩展的工程中，前端通过 Gateway 使用统一的认证和业务接口。

## 当前能力

- **题库与提交**：管理员可以维护题目和测试点；用户可以浏览题目、提交 Go/Python 等代码，并查看提交状态、判题结果和源码。
- **自动判题**：Judge Service 和 Judge Worker 在隔离环境中编译、运行用户代码，通过 RabbitMQ 驱动异步判题和结果投影。
- **比赛与排行榜**：支持比赛创建、编辑、报名、提交和排行榜。排行榜以 MySQL 为事实来源，Redis 作为可重建的查询投影，支持版本保护、回源和故障恢复。
- **Agent Service**：Python + FastAPI 服务已经接入 Gateway 的可信 RS256 委托和 SSE 流式链路，支持会话、消息和 Run 状态持久化。当前 Runtime 是明确标记的 Fake Runtime，生产环境不会自动降级到模拟回答。
- **Web 前端**：Vue 3 + Pinia + Axios + Vue Router 前端提供首页、登录、注册、题库、提交记录、比赛和个人资料页面。

Agent 当前不包含真实模型、业务 gRPC Tools、RAG、管理员控制面或完整聊天 UI；这些属于后续阶段。Fake Runtime 只用于本地开发和测试，必须显式启用。

## 技术组成

- Go / Go-Kratos：用户、题目、提交、判题和比赛服务
- Python / FastAPI：Agent Service、会话存储和流式运行时
- Vue 3 / Pinia / Axios：Web 前端
- gRPC / Protobuf：Go 服务之间的同步契约
- RabbitMQ：判题任务和结果事件
- MySQL：业务事实、会话、消息和运行状态
- Redis：比赛排行榜查询缓存
- MinIO：题目测试数据和提交源码对象
- Docker Compose：本地开发和集成测试环境

## 仓库结构

```text
api/        Protobuf 合约
services/   Go 业务服务
pkg/        Go 共享包
agent/      Python Agent Service
web/        Vue 前端
migrations/ MySQL 初始化和服务迁移
tests/      集成、契约和端到端测试
deploy/     Docker Compose 与部署文件
docs/       API、架构、数据库和运维文档
```

## 本地启动

项目需要 Go、Node.js、Python 3.12、uv 和 Docker Compose。先启动后端基础设施：

```bash
make infra-up
```

然后启动前端：

```bash
cd web
cp .env.example .env.local
npm install
npm run dev
```

前端默认地址是 `http://127.0.0.1:5173`，通过 `VITE_API_BASE_URL` 指向 Gateway，默认 Gateway 地址为 `http://127.0.0.1:8080`。

Agent 本地开发需要先准备 `oj_agent` 数据库并应用 [Agent migration](migrations/agent/000001_create_agent_runtime.up.sql)，再执行：

```bash
cp agent/.env.example agent/.env
make agent-init
make agent-dev
```

如需启用 Agent Chat，必须显式配置 `AGENT_RUNTIME_MODE=fake` 或 `langgraph_fake`、数据库地址和 Gateway 公钥；生产环境禁止 Fake Runtime。通过 Compose 启用 Agent profile 的命令和密钥挂载方式见 [部署说明](deploy/compose/README.md)。

## 常用检查

```bash
make proto
make fmt-check
make vet
make test-unit
make build
make test-integration
```

Agent 的 Python 检查和测试：

```bash
make agent-check
make agent-test-unit
make agent-test-integration
```

`make test-integration` 和 `make agent-test-integration` 会创建独立的 Docker Compose project、网络和数据卷，测试结束后清理测试资源。已有数据库卷不会因为服务启动而自动应用新 migration，升级前请按照对应文档显式执行迁移和回填。

## 相关文档

- [API 文档](docs/api.md)
- [架构说明](docs/architecture.md)
- [数据库说明](docs/database.md)
- [Agent 服务说明](agent/README.md)
- [比赛排行榜缓存运维说明](docs/contest-cache-operations.md)
- [Compose 部署说明](deploy/compose/README.md)

项目首页也提供了面向使用者的简要介绍；访问前端根路径 `/` 即可查看。首页与本 README 的项目状态保持同步。
