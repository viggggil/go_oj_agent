# 需求文档（Requirements）

## 1. 项目概述

**Distributed Online Judge & AI Coding Agent** 是一个面向编程学习与在线判题场景的分布式平台。

核心目标：

- 使用 Go-Kratos 构建领域化微服务。
- 使用 gRPC + Protobuf 统一内部服务契约。
- 使用 RabbitMQ 构建可靠异步判题链路。
- 使用 Go 实现 Judge Scheduler / Worker。
- 使用 Sandbox 隔离用户提交的不可信代码。
- 使用 FastAPI + LangChain/LangGraph 构建 AI Coding Agent。
- Agent 通过受控 Tool / gRPC 获取题目、提交与判题上下文。
- 建立自动化测试、可观测性和 CI/CD 工程体系。

---

## 2. 目标用户

### 2.1 普通用户 / 学习者

主要需求：

- 注册、登录和维护账户。
- 浏览、搜索题目。
- 提交代码并查看判题状态与结果。
- 查看自己的提交历史。
- 在比赛/作业场景中完成题目。
- 向 AI Agent 请求错误分析、Hint、知识解释和题目推荐。

### 2.2 题目管理员

主要需求：

- 创建、修改、发布题目。
- 管理题目标签和测试数据。
- 查看题目和判题运行情况。
- 后续可扩展 AI 辅助生成题面、测试点草稿，但必须保留人工审核。

### 2.3 系统维护者

主要需求：

- 查看服务健康状态、日志、指标和 Trace。
- 定位判题失败、消息积压和 Agent 调用异常。
- 通过自动化测试和 CI 控制代码质量。
- 支持服务独立扩缩容和故障隔离。

---

## 3. 业务范围

### 3.1 MVP 范围

第一阶段必须覆盖：

1. User
2. Problem
3. Submission
4. Distributed Judge
5. AI Coding Agent
6. 基础测试与 CI
7. 基础可观测性

Contest / Leaderboard 可以在核心链路稳定后加入。

### 3.2 非目标

第一阶段不主动引入：

- Kafka
- Istio / Service Mesh
- ClickHouse
- MongoDB
- 多注册中心
- 复杂 Multi-Agent
- 多地域部署

原则：优先把核心链路做深，而不是堆叠技术栈。

---

## 4. 核心业务流程

### 4.1 用户认证

```text
Register / Login
        ↓
JWT Access Token
+
Refresh Token
        ↓
Gateway Authentication
        ↓
Owning Service Authorization
```

要求：

- 密码不能明文保存。
- Access Token 生命周期应短于 Refresh Token。
- 最终资源权限校验必须发生在资源所属服务。
- 外部客户端不能伪造内部身份 Header。

### 4.2 题目浏览

用户可以：

- 获取题目列表。
- 按关键词、标签、难度筛选。
- 获取题目详情。
- 获取当前用户可见的题目内容。

测试数据正文不通过普通题目 API 暴露。

### 4.3 创建提交

```text
User
  ↓
Gateway
  ↓
Submission Service
  ↓
MySQL Transaction
  ├── Submission
  └── Outbox Event
  ↓
RabbitMQ
  ↓
Judge
```

创建成功后立即返回 `submission_id` 和初始状态，判题异步进行。

### 4.4 判题

判题至少支持以下结果：

- AC — Accepted
- WA — Wrong Answer
- TLE — Time Limit Exceeded
- MLE — Memory Limit Exceeded
- RE — Runtime Error
- CE — Compile Error

Judge Worker 负责：

1. 获取测试数据。
2. 编译源代码。
3. 在 Sandbox 中执行。
4. 限制 CPU、内存、进程、文件和执行时间。
5. 比较输出。
6. 聚合结果。
7. 发布判题结果事件。

### 4.5 实时状态

客户端可以通过 SSE 观察：

```text
QUEUED
  ↓
COMPILING
  ↓
RUNNING
  ↓
AC / WA / TLE / MLE / RE / CE
```

### 4.6 AI Coding Agent

Agent 不是普通聊天机器人，第一阶段聚焦 Coding Learning 场景。

支持：

- 编译错误分析。
- WA / TLE / RE 等提交诊断。
- 分级 Hint。
- 算法知识解释。
- RAG 检索。
- 历史提交复盘。
- 题目推荐。

Agent 获取业务信息时必须：

```text
Agent
  ↓
Tool
  ↓
gRPC Client
  ↓
Owning Go Service
  ↓
Authorization
```

Agent 不允许直接查询 User / Problem / Submission / Contest 业务数据库。

---

## 5. 功能需求

### FR-01 用户

系统应支持：

- 注册。
- 登录。
- Token 刷新。
- 获取当前用户信息。
- 基础 RBAC。

### FR-02 题目

系统应支持：

- 创建、更新、读取题目。
- 题目分页与搜索。
- 标签和难度。
- 测试点元数据管理。
- 测试数据存储到 MinIO。

### FR-03 提交

系统应支持：

- 创建提交。
- 获取提交详情。
- 查询用户提交历史。
- 查询实时状态和最终结果。
- 防止用户读取其他用户的私有提交内容。

### FR-04 Judge

系统应支持：

- 异步任务调度。
- 按语言路由 Judge Task。
- Worker 水平扩容。
- Publisher Confirm。
- Manual ACK。
- Retry。
- DLQ。
- 幂等消费。
- Worker 异常重启后的任务恢复。

### FR-05 Agent

系统应支持：

- Streaming Response。
- Tool Calling。
- Problem / Submission / Judge Result 上下文读取。
- RAG。
- Agent Eval。
- Tool 权限边界。

### FR-06 Contest（后续）

系统可扩展：

- 比赛/作业。
- Contest Problem。
- 参与者。
- Redis ZSET 实时排行榜。
- MySQL 最终结果持久化。

---

## 6. 非功能需求

### 6.1 可靠性

- Submission 创建与 Judge Event Intent 必须通过 Transactional Outbox 保持一致。
- MQ 采用 At-least-once 语义，Consumer 必须幂等。
- Retry 必须有上限并可观测。
- 不可恢复消息进入 DLQ。
- 外部 IO 必须考虑 timeout / cancellation。

### 6.2 安全

- Secret 不进入 Git。
- 用户代码视为不可信输入。
- Judge 禁止默认访问 Host Network / Host Filesystem。
- 用户代码必须受到 CPU、内存、进程数和运行时间限制。
- Agent Tool 采用最小权限。
- Prompt、题面、源码、编译输出和 RAG 文档均视为不可信内容。
- Prompt 不能替代真正的 Authorization。

### 6.3 可扩展性

以下组件应可以独立水平扩展：

- Gateway
- Go Business Services
- Judge Worker
- Agent Service

Judge Worker 的扩容模型独立于 Submission Service。

### 6.4 可观测性

核心链路需要具备：

- Structured Logging
- Metrics
- Distributed Tracing
- request_id
- trace_id

需要能够追踪：

```text
HTTP
 ↓
Gateway
 ↓
Submission
 ↓
RabbitMQ
 ↓
Scheduler
 ↓
Worker
```

### 6.5 可测试性

新功能应与测试一起进入 PR。

测试体系包括：

- Unit Test
- Integration Test
- Contract Test
- E2E Test
- Go Fuzz Test
- Agent Eval

---

## 7. 数据与一致性要求

- MySQL 是核心业务事实的 Source of Truth。
- Redis 主要保存 Cache / Realtime View / Temporary State。
- MinIO 保存测试数据、提交产物和 RAG 原始文档。
- Vector DB 保存用于检索的 Embedding。
- 服务只能直接访问自己拥有的数据。
- 跨服务数据访问通过 gRPC 或 Domain Event。
- DB State Change + MQ Event 的可靠写入必须使用 Transactional Outbox。

---

## 8. 关键验收场景

### AC-01 提交成功

```text
Create Submission
      ↓
Submission Row Created
      ↓
Outbox Row Created
      ↓
Event Published
      ↓
Judge Completed
      ↓
Submission Updated
```

### AC-02 MQ 重投不重复更新

相同 `event_id` 被重复投递时：

- 不应产生重复业务副作用。
- Consumer 可以安全 ACK。

### AC-03 Judge 超时

恶意或死循环程序：

- 必须被终止。
- Worker 不应永久阻塞。
- 结果可被标记为 TLE / System Error。
- Worker 可继续处理后续任务。

### AC-04 Agent 权限

用户 A 请求 Agent 分析用户 B 的私有提交时：

- Tool 调用到 Submission Service。
- Submission Service 拒绝访问。
- Agent 不得绕过权限读取数据库。

### AC-05 CI

Pull Request 至少验证：

- Go format / lint / test / build。
- Python lint / type check / pytest。
- Proto lint / breaking check。
- 核心 Integration Test。

---

## 9. 版本路线

### Phase 0 — Engineering Foundation

- Monorepo
- Go Workspace
- Kratos Skeleton
- Proto
- Docker Compose
- GitHub Actions

### Phase 1 — Core Services

- Gateway
- User
- Problem
- Submission
- MySQL
- Redis
- gRPC
- Consul

### Phase 2 — Distributed Judge

- RabbitMQ
- Transactional Outbox
- Judge Scheduler
- Judge Worker
- MinIO
- Sandbox
- Retry / DLQ / Idempotency

### Phase 3 — AI Coding Agent

- FastAPI
- LangChain
- LangGraph
- gRPC Tools
- RAG
- SSE
- Agent Eval

### Phase 4 — Engineering Hardening

- OpenTelemetry
- Prometheus
- Grafana
- Jaeger / Tempo
- E2E
- k6
- Security Scan

### Phase 5 — Extensions

- Contest
- Leaderboard
- Qdrant
- Admin HITL
- Kubernetes
