# 架构设计文档（Architecture Design）

## 1. 设计目标

本项目采用 **Git Monorepo + Independent Services**。

核心设计原则：

1. 按业务能力拆服务，不按数据库表拆服务。
2. 外部使用 REST / SSE，内部同步通信使用 gRPC + Protobuf。
3. 异步领域事件与 Judge Task 使用 RabbitMQ。
4. 每个服务拥有自己的数据，禁止跨服务直接访问数据库。
5. `judge-service` 统一拥有 Submission、调度策略和结果状态，使用 Transactional Outbox 保证 Judge Task 可靠投递。
6. Judge Worker 与业务服务使用不同扩容模型。
7. Agent 只能通过受控 Tool / gRPC 访问业务数据。
8. 安全、测试和可观测性属于架构的一部分，而不是后补功能。

---

## 2. 总体架构

```mermaid
flowchart TB
    Client["Web / CLI Client"]

    subgraph Edge["Edge Layer"]
        Gateway["API Gateway<br/>Go-Kratos<br/>REST / SSE"]
    end

    subgraph Domain["Go Business Services"]
        User["User Service"]
        Problem["Problem Service"]
        Judge["Judge Service<br/>Submission + Dispatch"]
        Contest["Contest Service"]
    end

    subgraph JudgeDomain["Judge Execution Domain"]
        Worker["Judge Worker × N<br/>Go + Sandbox"]
    end

    subgraph AI["AI Domain"]
        Agent["Agent Service<br/>FastAPI + LangChain/LangGraph"]
        Vector["Qdrant"]
    end

    subgraph Infra["Infrastructure"]
        MySQL[("MySQL")]
        Redis[("Redis")]
        RabbitMQ[("RabbitMQ")]
        MinIO[("MinIO")]
        Consul[("Consul")]
    end

    subgraph Obs["Observability"]
        OTel["OpenTelemetry"]
        Prometheus["Prometheus"]
        Grafana["Grafana"]
        Trace["Jaeger / Tempo"]
    end

    Client -->|"REST / SSE"| Gateway

    Gateway -->|"gRPC"| User
    Gateway -->|"gRPC"| Problem
    Gateway -->|"gRPC"| Judge
    Gateway -->|"gRPC"| Contest
    Gateway -->|"HTTP / SSE"| Agent

    Judge -->|"Outbox Relay: judge.task.*"| RabbitMQ
    RabbitMQ --> Worker
    Worker -->|"judge.completed"| RabbitMQ
    RabbitMQ --> Judge

    Agent -->|"gRPC Tools"| User
    Agent -->|"gRPC Tools"| Problem
    Agent -->|"gRPC Tools"| Judge
    Agent --> Vector

    User --> MySQL
    Problem --> MySQL
    Judge --> MySQL
    Contest --> MySQL

    User --> Redis
    Problem --> Redis
    Judge --> Redis
    Contest --> Redis

    Problem --> MinIO
    Judge --> MinIO
    Worker --> MinIO
    Agent --> MinIO

    Gateway -.-> Consul
    User -.-> Consul
    Problem -.-> Consul
    Judge -.-> Consul
    Contest -.-> Consul

    Gateway -. telemetry .-> OTel
    User -. telemetry .-> OTel
    Problem -. telemetry .-> OTel
    Judge -. telemetry .-> OTel
    Worker -. telemetry .-> OTel
    Agent -. telemetry .-> OTel

    OTel --> Prometheus
    OTel --> Trace
    Prometheus --> Grafana
```

---

## 3. 服务边界

| Service | Technology | Responsibility | Main Dependencies |
| --- | --- | --- | --- |
| `gateway-service` | Go + Kratos | REST、SSE、认证入口、限流、路由、Trace | Redis、Consul |
| `user-service` | Go + Kratos | 用户、认证、RBAC | MySQL、Redis、Consul |
| `problem-service` | Go + Kratos | 题目、标签、测试点 Metadata | MySQL、Redis、MinIO、Consul |
| `judge-service` | Go + Kratos | 提交、源码对象、状态、结果、调度策略、Outbox Relay、结果消费 | MySQL、Redis、RabbitMQ、MinIO、Consul |
| `contest-service` | Go + Kratos | 比赛、作业、排行榜 | MySQL、Redis、Consul |
| `judge-worker` | Go | 编译、Sandbox 执行、结果聚合 | RabbitMQ、MinIO |
| `agent-service` | Python + FastAPI | Agent、RAG、Tool Calling、Streaming | gRPC、Vector DB、Redis |

### 3.1 数据所有权

禁止：

```text
contest-service
    ↓ SQL
submission tables
```

正确：

```text
contest-service
    ↓ gRPC
judge-service
```

或者：

```text
judge-service
    ↓ Domain Event
RabbitMQ
    ↓
contest-service
```

Agent 同样不允许直接访问 User / Problem / Submission / Contest 的业务表。

---

## 4. 仓库结构

```text
distributed-oj/
├── api/
│   ├── common/v1/
│   ├── user/v1/
│   ├── problem/v1/
│   ├── submission/v1/
│   ├── contest/v1/
│   └── judge/v1/
│
├── services/
│   ├── gateway/
│   ├── user/
│   ├── problem/
│   ├── judge/
│   ├── contest/
│   └── judge-worker/
│
├── pkg/
│   ├── auth/
│   ├── mq/
│   ├── cache/
│   ├── errors/
│   └── observability/
│
├── agent/
│   ├── app/
│   │   ├── api/
│   │   ├── graphs/
│   │   ├── tools/
│   │   ├── rag/
│   │   ├── clients/
│   │   └── core/
│   └── tests/
│
├── tests/
│   ├── integration/
│   ├── contract/
│   └── e2e/
│
├── migrations/
├── deploy/
├── scripts/
└── docs/
```

每个 Go-Kratos 服务内部保持：

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

---

## 5. 通信模型

### 5.1 External

```text
Client
  ↓
REST / SSE
  ↓
Gateway
```

REST：

- 普通 CRUD / Query。
- 登录、题目、提交等同步请求。

SSE：

- Judge 状态。
- Agent Streaming。
- 长任务进度。

### 5.2 Internal Synchronous

```text
Service
  ↓
gRPC + Protobuf
  ↓
Service
```

适用于：

- GetUser
- GetProblem
- GetSubmission
- Authorization Context
- Agent Tool Calling

### 5.3 Internal Asynchronous

```text
Producer
  ↓
RabbitMQ
  ↓
Consumer
```

适用于：

- Judge Task
- Judge Result
- Submission Event
- Contest Event

---

## 6. Submission / Judge 架构

```mermaid
sequenceDiagram
    autonumber

    participant C as Client
    participant G as Gateway
    participant J as Judge Service
    participant P as Problem Service
    participant DB as MySQL
    participant O as Outbox Relay
    participant MQ as RabbitMQ
    participant JW as Judge Worker
    participant M as MinIO

    C->>G: POST /api/v1/submissions
    G->>J: CreateSubmission
    J->>P: Get active judge_revision + limits
    J->>M: Upload immutable source object

    J->>DB: BEGIN
    J->>DB: INSERT submission(judge_revision)
    J->>DB: INSERT outbox_event(judge.requested, submission_id)
    J->>DB: COMMIT

    J-->>G: submission_id + QUEUED
    G-->>C: 202 Accepted

    O->>DB: Read unpublished outbox
    O->>O: Normalize task + select language route
    O->>MQ: Publish judge.task.<language>
    MQ-->>O: Publisher Confirm
    O->>DB: Mark published

    MQ->>JW: Consume judge task
    JW->>M: Download testcase
    JW->>JW: Compile
    JW->>GJ: go-judge gRPC Execute
    GJ->>GJ: Sandbox Execute
    JW->>JW: Compare / Aggregate

    JW->>MQ: Publish judge.completed
    MQ-->>JW: Publisher Confirm
    JW->>MQ: ACK task

    MQ->>J: Consume judge.completed
    J->>DB: TX dedup + submission/cases + judged outbox
    J->>MQ: ACK result
```

### 6.1 Transactional Outbox

禁止：

```text
INSERT submission
COMMIT
RabbitMQ.Publish()
```

因为可能产生：

```text
DB Success
MQ Failure
```

正确：

```text
BEGIN
  INSERT submission
  INSERT outbox_event
COMMIT
```

之后由 `judge-service` 内部的 Outbox Relay 读取事件，完成任务规范化和语言路由，直接发布到 `judge.task.<language>` 并等待 Publisher Confirm。首版只启动一个 `judge-service` 实例，API、Relay 和 Result Consumer 在同一进程中作为独立模块运行；后续可以拆成运行角色，但不会形成新的业务服务。

### 6.2 Delivery Model

默认：

```text
At-least-once Delivery
```

因此需要：

- `event_id`
- Publisher Confirm
- Manual ACK
- Idempotent Consumer
- bounded Retry
- DLQ
- bounded Prefetch
- Backoff

结果消息必须携带 `submission_id` 与 `judge_revision`。Judge Service 在同一
事务完成消费去重、Submission/Case Result 及 `submission.judged` Outbox；
已取消、已作废或已超时 Submission 的迟到结果不得覆盖终态。MySQL 是 SSE
可恢复的事实来源，Redis 只保存实时视图。

---

## 7. RabbitMQ Topology

建议 Topic Exchange：

```text
oj.events
```

Routing Keys：

```text
submission.created
submission.judged
submission.invalidated

judge.started
judge.completed
judge.failed

contest.started
contest.ended
```

Judge Task Queues：

```text
judge.task.cpp
judge.task.c
judge.task.go
judge.task.python
judge.task.java
```

失败消息：

```text
judge.retry.cpp
judge.retry.go
judge.retry.python
judge.retry.java
judge.dlq
```

Worker 捕获到可重试系统故障时，把原任务送入对应语言的 Retry Queue；队列
TTL 到期后通过 DLX 回到 `judge.task.<language>`。Consumer 根据 RabbitMQ
`x-death` 计数，最多重试 3 次后转入 `judge.dlq`。Worker 进程直接崩溃可能
只触发未 ACK 消息 redelivery，因此 Submission 还必须有总 deadline，由
Judge Service Reconciler 将长期无结果任务收敛为 `DONE/SYSTEM_ERROR`。

RabbitMQ 短暂不可用不立即产生 SYSTEM_ERROR。Outbox 保留未发布意图；Worker
必须在结果发布收到 Publisher Confirm 后才 ACK 原任务。只有 DLQ 耗尽或超过
总 deadline，Judge Service 才写入 SYSTEM_ERROR。

Event Envelope：

```json
{
  "event_id": "uuid",
  "event_type": "judge.completed",
  "event_version": 1,
  "occurred_at": "RFC3339 timestamp",
  "trace_id": "trace id",
  "data": {}
}
```

Judge Task 的公共 `event_type` 为 `judge.task`，语言由 routing key 区分；
`judge.requested` 只作为 Judge Service 内部 Outbox Intent。Envelope、Task、
Completed 和 Failed 的代码契约统一放在 `pkg/mq`。

Problem 的每个 immutable revision 在 manifest 顶层保存一份题目级时间和内存
限制，该 revision 下所有测试点共享。修改限制与修改测试点一样会生成新
revision，MySQL 仍只保存最新 active 指针。

---

## 8. Judge Worker

首版 Worker 通过 gRPC 调用独立的 `go-judge v1.12.3` Sandbox Service。Worker 不直接
执行用户代码，go-judge 不持有 RabbitMQ、MinIO、MySQL 或业务 JWT 凭据；Worker 使用
固定的 Go 编译参数和 manifest 中的题目级资源限制。Worker 已接入 MinIO immutable
input loader：源码来自 `submission-source`，manifest 和测试点来自 `problem-data`，
下载后验证 revision、对象 key、size 与 SHA-256。正式消息消费仍按后续 Worker Issue
接入。

目录建议：

```text
judge-worker/
├── consumer/
├── compiler/
├── runner/
├── sandbox/
├── comparator/
├── testcase/
└── reporter/
```

抽象：

```go
type LanguageRunner interface {
    Compile(ctx context.Context, source string) (*Artifact, error)
    Run(ctx context.Context, artifact *Artifact, input []byte) (*RunResult, error)
}

type Sandbox interface {
    Execute(ctx context.Context, req RunRequest) (RunResult, error)
}
```

重点使用：

- goroutine
- channel
- bounded worker pool
- `context.Context`
- timeout / cancellation
- `errgroup`
- resource cleanup

不要启动无上限 goroutine。

---

## 9. Sandbox

用户代码视为不可信代码。

最低安全约束：

```text
Linux Namespace
cgroups v2
seccomp
rlimit
non-root user
network disabled
read-only filesystem
process limit
CPU limit
memory limit
file size limit
execution timeout
syscall restriction
```

环境建议：

```text
Local Development: Docker Sandbox
Production-like: nsjail / gVisor
```

禁止将未经处理的用户输入直接拼接为 Shell Command。

---

## 10. Agent 架构

Agent Service 是面向编程学习的通用 Agent，不绑定单一的提交诊断流程。算法解释、题目理解、分级 Hint、提交诊断、历史复盘、题目推荐和学习计划都运行在同一个 Agent Runtime 上，由 Skill 声明目标、可用 Tool、Prompt、输出结构和预算。

```mermaid
flowchart TB
    Question["User Question"] --> API["FastAPI / SSE"]
    API --> Session["Session Resolver"]
    Session --> Intent["Intent + Entity Parser"]
    Intent --> Policy["Tool / Skill Policy"]
    Policy --> Config["Immutable Agent / Prompt / Skill Config"]
    Config --> Runtime["统一 ReAct Runtime"]

    Runtime --> React["Reason / Decide"]
    React -->|Finish| Verify["Evidence / Safety Check"]
    React --> Executor["Typed Tool Executor"]
    Executor --> Problem["Problem Tools"]
    Executor --> Submission["Submission Tools"]
    Executor --> Knowledge["Knowledge Tools"]
    Executor --> Profile["Learning Tools"]

    React --> Verify
    Verify --> Response["Response Generator"]

    Admin["Admin Console"] --> Control["Agent Control Plane"]
    Control --> Config
    Control --> Eval["Eval / Observability"]
    Runtime -. events .-> Eval
```

设计原则：

- Context Resolver 只解析 submission_id、problem_id、语言和时间范围等实体，不自动预取业务数据。
- Agent 从当前策略允许的 Tool 集合中选择工具；工具选择和参数必须经过程序校验。
- 第一阶段保持单 Agent + LangGraph Workflow，不为了展示技术强行拆多 Agent。
- ReAct 用于有限的动态取证；Plan-and-Solve 用于学习计划、历史复盘等多步骤任务。
- Reflection 只检查证据、引用、安全和不确定性，不负责授予权限。
- LLM、Prompt、Skill 和知识库内容都不是授权边界。
- Tool 使用 typed input / output，Tool Executor 统一处理超时、去重、脱敏和预算。
- Tool 的实现、RPC 方法、权限校验和安全前缀由代码保护；Prompt、Skill 绑定、预算和知识库内容由 Control Plane 管理。

### 10.1 Agent Runtime

PR2 提供 RunService、typed State/Event、Fake Runtime、最小 LangGraph 和 oj_agent 存储。
PR3 增加 Gateway → Agent HTTP SSE：Gateway 验证外部 Access JWT，再签发绑定 Agent
与 HTTP 操作的 RS256 委托；Agent 只从验证后的 actor/roles/request_id 创建 Principal。
默认 Chat/Runtime disabled，开发明确启用 Fake，生产禁止 Fake；真实 Direct Answer 在 PR6 接入，配置管理 HTTP 在 PR7 接入。

身份、输入、owner 和并发在响应头前校验，之后仅输出 SSE。Gateway 按路由设置独立
Agent 总预算，保留普通预算和父级取消；按有界 UTF-8 SSE 帧增量转发并校验 ID/序号/终态。
断连、写失败、deadline 与 shutdown 关闭上游和 AcceptedRun。可选 Compose profile
不映射 Agent 公网端口，Agent 只挂载 Gateway 公钥，使用仅有 oj_agent 数据权限的开发账户。

用户消息和 RUNNING 在事务中接受，完成答案和终态在事务中保存，成功持久化后才
发 `done`；失败/取消不保存部分 assistant 输出。单实例启动收敛旧 RUNNING 为
INTERRUPTED，同会话并发由数据库生成列唯一索引约束，不依赖内存锁。调用方消费流
必须使用 `AcceptedRun` 上下文或显式关闭，才能在流开始前/后取消并清理 Run。

运行时状态至少包括：

```text
user_id / conversation_id / run_id / trace_id
intent / goal / entities / constraints
skill_key / allowed_tools / config_snapshot
plan / observations / evidence / missing_information
tool_call_count / remaining_tokens / deadline / reflection_count
answer_draft / final_answer
```

真实模型统一执行 ReAct 图；历史 execution_mode 仅用于兼容旧配置读取，不再决定运行分支。

每次 Agent Run 开始时固定 Agent、Prompt、Skill、Tool Catalog、模型和知识库索引的具体配置 UUID 快照。一次回答执行期间不得切换配置；PR5 的替换会创建新 UUID，已开始的 Run 继续使用原快照。

### PR6 模型边界

真实 Direct Answer 使用版本化的 Agent → Model Profile → Provider → Credential，固定单 Provider。Provider/模型选择由管理员手动配置，不做调用顺序、自动切换、比价或余额查询。首个适配器为 Responses，su8 的三个 DeepSeek 模型请求固定 store=false；Future Chat Completions/其他厂商协议由独立适配器扩展。

凭据由 Agent 自有仓储 AES-GCM 加密，部署 keyring 独立于数据库；Provider、Credential 继续通过本机 CLI 管理。PR7 提供 Agent、Prompt、Skill 管理 HTTP，PR8 的 Vue 管理工作区通过 Gateway 接入；历史 UUID 详情仅管理员可见，模型详情严格脱敏，不返回 Provider/凭据。归档用于配置历史，撤销凭据用于阻止新调用；安全检查不允许模型/用户覆盖。部署 origin allowlist、禁用环境代理和重定向限制凭据发送目标；网络出口与 DNS 的信任由部署侧控制。

Provider 文本经类型/大小/终态校验转换为既有 SSE，取消贯穿 HTTP stream；最终答案和模型摘要随 Run 终态事务保存后才发送 done。默认不重试，仅在显式设置后对输出前的可分类临时 HTTP 故障在同一 Provider 有限重试。模型预算/超时受 Run 剩余上限约束，缺失精确 usage 时标记 estimated。

### Issue #180：统一真实 ReAct 工作流

所有真实 Run 经过 ModelRuntime 构建的同一张 ReAct Graph：

```text
START → prepare → check_budget → reason
                                ├ Finish → validate_answer → finalize → END
                                └ Action → validate_action → act → observe → check_budget
```

Skill 只声明任务说明与授权工具，不再配置执行模式；历史 execution_mode 可读取但不决定图选择。ReActState 显式记录决策、预算、观察、成功证据和脱敏工具轨迹；Provider 的加密续接信息、工具结果输入留在请求局部 ReActContext，不启用 checkpointer 或外部 tracing。

工具执行前整批校验 JSON、Schema、call_id、相同参数重复调用、权限和预算；ToolExecutor 与拥有数据的 Go 服务仍做最终授权。失败返回 Observation，只有成功结果成为 Evidence。有工具的轮次确认 Finish 后才发正文，无工具的 Finish 保留实时 token 流和消费确认。进度、官方公开摘要和安全工具时间线与正文分离，摘要能力未启用时不生成虚构摘要。

取消、deadline、背压及唯一终态继续由 RunService 协调；答案与 COMPLETED 在本地事务提交后才发 done。ModelSummary JSON 保存有界工具摘要（含 Run 归属），历史会话在归属校验后可恢复，失败/取消 Run 也保留已有工具轨迹。新配置、历史兼容、前后评测方案见 [重构说明](agent/react-refactor.md)。

### Issue #162 PR2：有界多轮上下文

RunService 在创建本轮 Run 后，通过 AgentStore 查询本人、同会话的成功问答。
仅选择 COMPLETED Run 中由同一 run_id 关联的 user/assistant 对，按用户消息 ID
选最近最多 20 轮、恢复时间顺序；失败、取消、中断、本轮 RUNNING、tool 和残缺问答不参与。
候选历史按整轮限制到 64,000 字符，创建本轮与完成事务的原有边界不变。

prepare 先估算安全前缀、解析后的 Agent/Skill 文本及当前问题，再预留 max_output_tokens，
取 max_input_tokens 与 context_window_tokens 减输出预留的较小上限。
历史内容按 UTF-8 字节上界加每条 128 的保守消息开销估算，只保留能容纳的最近完整后缀；
不因历史过大拒绝本轮，不截断单条消息，也不另调模型摘要。若必需输入本身超限，沿用
AGENT_MODEL_INPUT_LIMIT。有限重试使用相同 PreparedAnswer，完整输入估算计入每次调用预算。

Responses input 按 user/assistant 角色传递最近未归档历史，当前问题只追加一次；结构化会话摘要
作为明确标注的辅助上下文进入 instructions，不被当作系统指令，也不能修改安全规则。摘要
携带已覆盖的最后一条完整 assistant 消息 ID，装配器只读取该游标之后的最近完整轮次，避免同一
轮次同时出现在摘要和原始历史中。每轮请求仍 store=false、不使用 previous_response_id，服务端
已存消息是上下文来源，不引入供应商托管会话、持久化 checkpointer 或跨会话长期记忆。
配置版本仍由当前 Run 固定，新对话历史为空；同会话 Agent key 归属规则不变。

`agent_conversation_summaries` 提供结构化摘要、用途隔离和乐观版本写入边界。成功 Run 提交后
异步 Worker 才会被通知；它归档最近 6 轮以外的完整问答，按游标分批读取并调用增量摘要器。
摘要器保留最近 8 条归档上下文，将更早内容折叠为一条滚动摘要，并限制摘要文本总计
6,000 字符。最近未归档轮次仍以原文进入模型输入，摘要与原文不会重复。Worker 使用版本冲突
重试，只有摘要保存成功后游标才推进；失败或队列溢出不会影响已完成聊天，后续成功 Run
会再次触发补处理。摘要为确定性抽取式更新，不额外调用回答模型或保存模型内部推理。

### 只读工具边界

模型从授权 Function Schema 中自主决定调用顺序；不把自然语言当作动作。工具集合由 Agent/Skill 白名单、Registry enabled/read 和可信角色策略取交集。业务数据只经带委托 JWT 的 gRPC 访问。工具调用与结果只在本 Run 的 Provider 输入中续接，不进入长期对话正文。所有模型尝试、工具次数和结果大小都受预算限制；没有写工具或自动恢复。RAG 分组授权与离线 Eval 使用相同 Runtime，评审 Agent 独立使用无工具、无知识、单模型调用配置。

### 10.2 Tool Registry 与策略

代码 Registry 只注册 Tool 实现和不可绕过的安全元数据。第一批只读 Tool 包括：

```text
get_current_user
get_problem
list_problems
get_submission
get_submission_source
get_judge_result
list_submissions
retrieve_knowledge
```

后续增加：

```text
get_learning_profile
search_problems
recommend_problem
create_submission
rejudge_submission
```

每个 Tool 声明 `read_scope`、`allowed_roles`、`side_effect`、`sensitivity`、超时、成本和是否需要确认。管理员可以在 Skill 层调整 Tool allowlist 和启用状态，但不能修改 RPC 方法、目标服务或授权实现。写 Tool 默认关闭。

Issue #160 简化当前表单：后端生成稳定资源 key，管理员填写名称；Prompt 负责基础规则，Skill
直接保存任务说明，Agent 高级设置承载运行预算。版本仍不可变、依赖仍绑定具体 UUID。
开发 Compose 检测宿主 su8.key 后准备单 su8 Provider 和三个模型并运行真实 Direct Answer，
无 Key 时保留 Fake。Provider/凭据仍仅部署侧管理，浏览器不接触 Key。

### 10.3 Agent Control Plane

PR7 的已实现接口全部经 Gateway：管理员可分页查询、创建、替换、归档、恢复、停用和启用 Agent/Prompt/Skill，并查看历史版本、只读 Tools 元数据和 su8 模型选项。模型选择绑定 Model Profile UUID，Provider 和凭据无 HTTP 管理接口。写请求使用调用方保留的 UUID `X-Request-ID`，通过签名委托携带操作者和请求编号；仓储在事务内检查 `expected_id`、依赖、预算和工具白名单并写审计。管理端可以在 Chat 关闭时独立修复配置。

普通用户的 Agent/Skill 目录只返回名称、key、默认选项等聊天所需字段，按身份、有效期、停用依赖、运行模式和部署 Provider 网络策略在分页前过滤。目录在 Chat 开启且数据库配置模式时可用，独立于管理员管理开关；不返回 Prompt 正文、配置图或凭据。完整契约见 `docs/api.md`。PR8/PR9 已交付管理页面和对话页面。Web 通过同源 Gateway fetch POST 消费 SSE，独立有界 parser 校验身份与终态；取消与代次隔离防止旧 Run 污染新会话。Chat 不继承 Axios 的自动重放，认证刷新后需用户再次发送。多轮上下文及 RAG PR1–PR3 已实现；完整质量评测继续由后续阶段完成。

Control Plane 管理以下可版本化资源：

```text
Prompt / Prompt Version
Skill / Skill Version / Skill-Tool Binding
Tool Catalog Policy
Knowledge Document / Knowledge Version / Index
Eval Case / Eval Dataset / Eval Run
```

PR5 的 Prompt、Skill、Model Profile 和 Agent 不使用草稿状态：

```text
CREATED → CURRENT → ARCHIVED
```

配置版本不可变；修改生成新 UUID，恢复通过复制旧正文创建新 UUID。Prompt 创建时校验变量白名单、敏感信息、Tool 名称和禁止指令。Skill 只能引用代码已注册的 Tool，不能执行任意 Python。

### 10.4 管理员前端

管理员入口接受项目现有 `admin`、`system_admin` 或明确授权的 `agent_admin` 角色，至少提供：

- Prompt 管理：编辑、版本 Diff、样例 Eval、替换、归档、恢复和审计。
- Tool Catalog：查看描述、Schema、来源 RPC、敏感级别、允许角色、启用状态和调用统计；RPC 方法和授权代码只读展示。
- Skill 管理：填写名称、描述和任务说明、选择 Tool、替换、归档和复制旧版恢复；旧 Prompt 绑定兼容读取，新 Skill 默认继承 Agent 预算。
- 知识库管理：文档上传、元数据、标签、切分预览、索引、发布、归档和回滚。
- Eval 与质量：运行数据集、版本对比、失败样例、工具选择错误、引用错误和拒答错误。
- 运行观测：请求量、延迟、Token、Tool 错误、RAG 延迟、Skill 分布、Trace 和脱敏后的运行详情。

替换、恢复、归档和启用写 Tool 需要二次确认并写入审计日志；生产环境可以配置双人审批。管理员页面不能修改安全前缀、服务身份或目标 Go Service 的授权规则。

### 10.5 Agent 质量工程

离线 Eval、线上运行事件和人工抽检使用同一套配置版本标识。评估包括：

- 确定性行为：意图、Skill、Tool、参数、权限、状态转换、预算和输出 Schema；
- 模型质量：事实正确性、相关性、解释完整性、Hint 等级、引用一致性和不确定性标注；
- 人工抽检：按 Skill、模型和配置版本抽样，失败样例回流数据集。

核心指标包括：

```text
tool_selection_pass_rate
tool_argument_pass_rate
permission_denied_correct_rate
evidence_grounded_rate
citation_valid_rate
answer_relevance_score
answer_correctness_score
hint_level_accuracy
clarification_rate
unsafe_action_block_rate
fallback_rate
```

运行事件至少记录 `run_id`、`conversation_id`、`user_id`、`trace_id`、Skill/Prompt/Tool Catalog 版本、模型、Tool 延迟和状态、Token 使用、答案状态和 Eval 标签。源码、凭据、完整 Prompt 和原始 Tool Result 默认脱敏或只保存摘要。

## 11. RAG

```mermaid
flowchart LR
    Docs["Docs / Editorials / Notes"] --> Storage[("MinIO")]
    Storage --> Worker["RAG Ingestion Worker"]
    Worker --> Split["Loader + Splitter"]
    Split --> Embed["Embedding"]
    Embed --> Vector[("Qdrant")]

    Query["Agent Query"] --> Retriever["Retriever"]
    Retriever --> Vector
    Retriever --> Agent["Agent"]
```

Metadata 建议：

```text
document_id
source
version
problem_id
algorithm
difficulty
language
document_type
```

开发与部署统一使用 Qdrant。中文向量化固定采用本地 `BAAI/bge-small-zh-v1.5`，与 su8 回答模型独立。PR1 的管理员摄取/检索 API、MySQL 租约 worker 和文档版本发布，PR2 的按组管理，以及 PR3 的 LangGraph retrieve_knowledge 已实现。Run 固定已发布版本；检索前后重新确认 Agent 分组交集、文档启用状态及 Problem/Contest 业务授权。Contest 的内部批量 RPC 排除未来或进行中的比赛题解，依赖故障失败关闭。检索片段只给模型，Run JSON 只记录有界来源元数据，管理员可追溯，普通 SSE 不透传工具结果。选型与边界见 ADR-0002。

---

## 12. Redis / MinIO

### Redis

职责：

- Problem Cache
- Submission Realtime Status
- Refresh Token Metadata
- Rate Limit
- Idempotency
- Contest Leaderboard
- SSE Fan-out
- Temporary State

原则：

```text
Redis = Cache / Realtime View
MySQL = Source of Truth
```

### MinIO

Bucket / Prefix：

```text
problem-data/
submission-artifacts/
rag-documents/
```

MySQL 仅保存对象元数据，例如：

```text
object_key
sha256
size
version
```

---

## 13. 服务发现与部署

Local：

```text
Docker Compose
+
Consul
```

Production-like：

```text
Kubernetes
```

本地开发和非 Kubernetes 部署使用 Consul 提供服务注册与发现；进入 Kubernetes 后优先使用 Kubernetes Service Discovery，不强行叠加第二套发现机制。当前 `user-service` 已使用 Kratos Consul Registrar 完成服务注册和注销，并开启 Consul 健康检查；其他服务在各自接入阶段复用相同模式。

当前 Gateway 骨架位于 `services/gateway`，使用 `cmd/server` 作为入口，先提供 HTTP Server、配置、middleware、client 与 service 扩展点。Gateway 后续通过 gRPC 调用 `user-service`，不会直接访问用户数据库。

---

## 14. 可观测性

```mermaid
flowchart LR
    HTTP["HTTP Request"] --> Gateway["Gateway"]
    Gateway --> Judge["Judge Service"]
    Judge --> MQ["RabbitMQ"]
    MQ --> Worker["Worker"]

    Gateway -. "同步 gRPC 已接入" .-> OTel["OpenTelemetry"]
    Agent -. "阶段一已接入" .-> OTel
    Judge -. "同步调用已接入" .-> OTel
    Problem -. "同步调用已接入" .-> OTel
    Worker -. "异步 Trace 已接入" .-> OTel

    OTel --> Metrics["Prometheus"]
    OTel --> Traces["Jaeger / Tempo"]
    Metrics --> Grafana["Grafana"]
```

RabbitMQ Header 中传播 Trace Context。

可选本地观测栈通过 `make observability-up` 启动，默认 `make infra-up` 不启动观测组件。Gateway、Judge、Problem、Contest、Judge Worker 与 Agent 可将 Trace/Metric 发往 Collector，Trace 存入 Tempo，Collector 暴露 Prometheus 指标，Grafana 预置 Prometheus 与 Tempo 数据源。同步判题链路已接通 Gateway gRPC → Judge gRPC → Problem gRPC，并在 Judge 提交、重判入口创建业务 Span。PR2 已将 Judge Outbox 的 W3C 上下文持久化，并接通 Relay → RabbitMQ → Worker → Judge Result 的异步父子 Trace；PR3 继续接通 Judge Result 事务 → `submission.judged` → Contest Projection → Contest Cache Outbox/Relay。旧消息、无 Header 消息和历史 NULL 上下文仍可正常处理。未设置 `OTEL_EXPORTER_OTLP_ENDPOINT` 时使用 no-op provider。Collector/Tempo 不作为服务启动依赖；此阶段不启用 LangSmith 上传。

关键 Metrics：

- HTTP/gRPC latency / error rate。
- MQ publish failure / retry / DLQ size。
- Judge queue time / compile time / execute time / verdict count。
- Agent latency / tool failure / retrieval latency / eval pass rate。

---

## 15. 测试架构

```mermaid
flowchart TB
    E2E["E2E Tests<br/>Few"]
    Integration["Integration Tests"]
    Contract["Contract Tests"]
    AgentEval["Agent Evals"]
    Unit["Unit Tests<br/>Many"]

    E2E --> Integration
    Integration --> Contract
    Integration --> AgentEval
    Contract --> Unit
    AgentEval --> Unit
```

重点：

- Unit：业务规则、Judge 聚合、权限、Agent 路由。
- Integration：MySQL、Redis、RabbitMQ、MinIO、gRPC。
- Contract：Proto / Event Schema。
- Fuzz：Comparator、Parser、Decoder。
- E2E：提交判题主链路、Agent Tool 权限主链路。

---

## 16. CI / Git

Git Workflow：

```text
Issue
  ↓
Short-lived Branch
  ↓
Code + Tests
  ↓
Pull Request
  ↓
CI
  ↓
Review
  ↓
Squash Merge
  ↓
main
```

PR 至少执行：

```text
Go fmt / vet / lint / test / build
Python lint / type check / pytest
buf lint / buf breaking
Integration Test
Docker Build
Security Scan
```

---

## 17. 关键架构不变量

1. Service 不直接访问其他 Service 的数据库。
2. Agent 不直接访问业务数据库。
3. DB Change + MQ Event 可靠写入使用 Outbox。
4. RabbitMQ Consumer 必须幂等。
5. Judge 用户代码始终视为不可信。
6. LLM / Prompt 不是授权边界。
7. 核心 IO 必须传播 Context / Timeout。
8. Generated Code 不手工修改。
9. 架构级变更应通过 ADR 记录。
