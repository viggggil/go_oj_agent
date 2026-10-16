# API 接口定义文档（API Specification）

## 1. 目标

本项目使用两类 API：

```text
External API
REST + SSE
        ↓
Gateway

Internal API
gRPC + Protobuf
        ↓
Go Services / Agent Tools
```

异步业务通过 RabbitMQ Event Contract 交互。

本文件定义 v0 阶段的接口边界。最终字段以 `api/*/v1/*.proto` 和生成的 OpenAPI 为准。

Gateway 外部 HTTP 请求/响应的结构化契约记录在 `api/gateway/v1/gateway.proto`。Gateway 的 `service GatewayService` 使用 `google.api.http` 声明 REST 路由，由 `protoc-gen-go-http` 生成 `gateway_http.pb.go`，服务启动时通过 `RegisterGatewayServiceHTTPServer` 注册。Gateway 当前已接入认证、用户、Problem、Submission/Judge 和 Contest API。

---

## 2. 通用规范

### 2.1 Version

External：

```text
/api/v1/...
```

Protobuf：

```text
gateway.v1
user.v1
problem.v1
submission.v1
contest.v1
judge.v1
```

请求字段校验由 `validate.rules` 定义，Go 代码由 `validate-go` 生成。Gateway HTTP Server 通过 Kratos `validate.Validator()` middleware 统一调用请求对象的 `Validate()`；user-service 的 gRPC service 入口继续校验内部请求。user-service 的业务错误 reason 定义在 `api/user/v1/errors.proto`，错误码使用 Kratos v3 标准的 `errors.code` 注解声明，并由 `protoc-gen-go-errors/v3` 生成 `*kratos/errors.Error` 构造和匹配函数。

仓库只使用根目录的 `buf.yaml` 和 `buf.gen.yaml` 管理全部 Proto，包括 API 契约和各服务配置。常用生成命令如下：

```bash
make generate  # 使用 Buf 生成全部 Proto Go 代码，并安装固定版本 Kratos errors 插件
make validate  # 使用 protoc 生成 API 参数校验代码
make errors    # 使用 Kratos errors 插件生成错误代码
```

服务配置 Proto 使用服务专属的 Proto 包名以避免单一 Buf module 中的符号冲突，但 Go 包导入路径保持不变。Kratos 的 `errors/errors.proto` 作为生成期契约保存在仓库根目录，运行时仍依赖 `github.com/go-kratos/kratos/v3/errors`。

### 2.2 Content Type

普通 HTTP：

```text
application/json
```

测试用例文件上传：

```text
multipart/form-data
```

Streaming：

```text
text/event-stream
```

### 2.3 Authentication

External：

```http
Authorization: Bearer <access_token>
```

Internal gRPC 传播可信上下文：

```text
user_id
role
request_id
trace_id
service identity
```

外部客户端提交的同名 Header 不得直接被当作可信身份。

### 2.4 HTTP Response

成功响应直接编码 Proto response：

```json
{
  "status": "ok"
}
```

请求 ID：

```http
X-Request-ID: <request_id>
```

失败响应使用 Kratos 标准错误结构：

```json
{
  "code": 400,
  "reason": "GATEWAY_INVALID_ARGUMENT",
  "message": "request is invalid"
}
```

### 2.5 Error Codes

统一语义：

```text
INVALID_ARGUMENT
UNAUTHENTICATED
PERMISSION_DENIED
NOT_FOUND
ALREADY_EXISTS
CONFLICT
RESOURCE_EXHAUSTED
FAILED_PRECONDITION
INTERNAL
UNAVAILABLE
DEADLINE_EXCEEDED
```

user-service 错误 reason：

```text
USER_ERROR_REASON_INVALID_ARGUMENT
USER_ERROR_REASON_INVALID_CREDENTIAL
USER_ERROR_REASON_ALREADY_EXISTS
USER_ERROR_REASON_NOT_FOUND
USER_ERROR_REASON_ADMIN_ALREADY_EXISTS
USER_ERROR_REASON_INACTIVE
USER_ERROR_REASON_PERMISSION_DENIED
USER_ERROR_REASON_REFRESH_TOKEN_DENIED
```

---

## 3. External REST API

## 3.1 Auth

认证接口由 `GatewayService` 的 HTTP 生成代码注册，不需要 Bearer Token。用户接口通过 Kratos operation middleware 校验 Bearer Token，并由 Gateway 将 token claims 转换为 `common.v1.RequestContext` 后传递给 user-service；资源权限仍由 user-service 判断。

### POST `/api/v1/auth/register`

创建用户。

Request：

```json
{
  "username": "alice",
  "email": "alice@example.com",
  "password": "..."
}
```

Response：

```json
{
  "user": {
    "id": 1001,
    "username": "alice",
    "email": "alice@example.com",
    "status": "active",
    "roles": ["user"]
  }
}
```

当前 Gateway 已接入该接口，内部转发到 `user.v1.UserService/Register`。

## 3.2 Problem

所有 Problem HTTP 接口都要求 Bearer Token，Gateway 将认证结果转换为
`common.v1.RequestContext`，最终资源权限仍由 problem-service 校验。

```text
POST   /api/v1/problems
GET    /api/v1/problems
GET    /api/v1/problems/{problem_id}
PUT    /api/v1/problems/{problem_id}
DELETE /api/v1/problems/{problem_id}
GET    /api/v1/problems/{problem_id}/testcases
DELETE /api/v1/problems/{problem_id}/testcases/{testcase_id}
```

测试点文件通过以下接口上传：

```http
POST /api/v1/problems/{problem_id}/testcases/upload
Authorization: Bearer <access_token>
Content-Type: multipart/form-data
```

表单字段为 `case_no`、`input` 和 `output`。当 `case_no=7` 时，两个文件名
必须严格为 `7.in` 和 `7.out`。每个文件最大 16 MiB，整个 HTTP 请求最大
34 MiB。Gateway 只解析并转发文件，MinIO 和 MySQL 写入由 ProblemService
完成。

### POST `/api/v1/auth/login`

Request：

```json
{
  "account": "alice@example.com",
  "password": "..."
}
```

Response：

```json
{
  "access_token": "...",
  "refresh_token": "...",
  "expires_in": 3600
}
```

### POST `/api/v1/auth/refresh`

Request：

```json
{
  "refresh_token": "..."
}
```

Response：新的 Access Token 和轮换后的 Refresh Token。

### POST `/api/v1/auth/logout`

使用 Refresh Token 注销其所属的登录 Session。该接口不要求 `Authorization: Bearer <access_token>`，因此 Access Token 过期后仍可退出登录。

Request：

```json
{
  "refresh_token": "..."
}
```

Response：

```json
{
  "status": "ok"
}
```

user-service 会撤销同一 Session 下的全部 Refresh Token。对未知、过期或已撤销的 Refresh Token 幂等返回成功；内部依赖故障仍返回错误。Access Token 是无状态 JWT，退出登录不会使已签发的 Access Token 立即失效，它仍按短 TTL 自然过期。客户端收到成功响应后必须清除本地 Access Token、Refresh Token 和用户状态。

当前 Gateway 已接入注册、登录、刷新令牌和退出登录四个公开认证接口。参数校验使用 `api/gateway/v1/gateway.proto` 的 `validate.rules` 生成代码，认证业务规则、Refresh Token 策略和 Session 注销仍由 user-service 执行。

### 前端认证流程

`web/` 提供基于 Vue 3 + Pinia + Axios 的参考客户端：注册成功后进入登录页；登录成功后保存 Token 并获取当前用户；访问受保护接口时自动携带 Access Token；Access Token 返回 401 时通过 Refresh Token 单飞刷新并重放请求；刷新失败则清理状态并回到登录页；退出登录调用本接口后清理本地 Token 和用户状态。

---

## 3.2 User

### GET `/api/v1/users/me`

获取当前用户信息。

需要 `Authorization: Bearer <access_token>`。Gateway 使用 `pkg/auth` 验证 Access Token，并将 claims、request id 和 trace id 转换为 `common.v1.RequestContext`，内部对应 `user.v1.UserService/GetCurrentUser`。

Response：

```json
{
  "user": {
    "id": 1001,
    "username": "alice",
    "email": "alice@example.com",
    "status": "active",
    "roles": ["user"]
  }
}
```

### GET `/api/v1/users/{id}`

按用户 ID 获取用户信息。

需要 `Authorization: Bearer <access_token>`，内部对应 `user.v1.UserService/GetUser`。Gateway 只传递请求者上下文和目标用户 ID，是否允许访问由 user-service 最终判断。

```json
{
  "user": {
    "id": 1001,
    "username": "alice",
    "email": "alice@example.com",
    "status": "active",
    "roles": ["user"]
  }
}
```

---

## 3.3 Problem

### GET `/api/v1/problems`

Query：

```text
page
page_size
```

当前版本只实现分页。名称搜索、难度筛选和标签筛选在后续版本增加。

Response：

```json
{
  "items": [
    {
      "id": 1001,
      "title": "Two Sum",
      "slug": "two-sum",
      "difficulty": "PROBLEM_DIFFICULTY_EASY",
      "status": "PROBLEM_STATUS_NORMAL"
    }
  ],
  "page": {
    "page": 1,
    "page_size": 20,
    "total": 1
  }
}
```

列表项使用 `ProblemSummary`，不包含题面正文、标签详情或测试用例元信息。

### GET `/api/v1/problems/{problem_id}`

返回题面、限制、标签等公开信息。

普通用户接口不返回测试用例元信息、MinIO object key 或隐藏测试数据正文。

### POST `/api/v1/problems`

管理员创建题目。

### PATCH `/api/v1/problems/{problem_id}`

管理员更新题目。

### DELETE `/api/v1/problems/{problem_id}`

管理员归档题目。该操作将状态改为 `ARCHIVED`，不物理删除题目。

### POST `/api/v1/problems/{problem_id}/testcases`

管理员以 `multipart/form-data` 一次提交配对的 `.in` 和 `.out` 文件及
`case_no`。Gateway 读取文件后调用内部 `AddTestcase` RPC；
Problem Service 在同一次业务操作中完成 MinIO 上传、SHA-256 计算和 MySQL
元信息落库，不提供单独的上传完成确认 API。每个文件当前最大 16 MiB。

### GET `/api/v1/problems/{problem_id}/testcases`

管理员展示某题测试用例元信息时使用。内部对应的
`ListProblemTestcases` RPC 供管理员测试点管理使用，也保留给需要诊断元数据的
可信内部调用方。该列表默认只返回 `ACTIVE` 测试用例，按 `case_no` 查询；管理员
可以显式包含归档项。Judge Service 创建提交时必须使用 `GetJudgeProfile`。

### DELETE `/api/v1/problems/{problem_id}/testcases/{testcase_id}`

管理员归档测试用例。该操作不物理删除 MySQL 元信息或 MinIO 对象，保证
历史判题仍可按照 `judge_revision` 复现。新增或归档测试点成功时，Problem
Service 必须先发布包含全部有效测试点的新 immutable revision，再切换题目的
active revision；发布失败时保留旧 revision，并使本次变更失败。

---

## 3.4 Submission

### POST `/api/v1/submissions`

创建提交。

Request：

```json
{
  "problem_id": 1001,
  "language": "cpp",
  "source_code": "#include <bits/stdc++.h>...",
  "idempotency_key": "550e8400-e29b-41d4-a716-446655440000"
}
```

Response：

```json
{
  "data": {
    "submission_id": 90001,
    "status": "QUEUED"
  },
  "request_id": "..."
}
```

建议返回：

```http
202 Accepted
```

Judge Service 接收 `source_code` 后获取题目的 active `judge_revision`，把源码
上传为 MinIO 不可变对象，再在一个事务中创建 Submission 和
`judge.requested` Outbox。数据库与 MQ 不保存源码正文。
如果事务失败，已上传但未被引用的源码对象由 GC 在安全保留期后清理。

普通提交的 `contest_id` 为 `NULL`。比赛提交必须使用 Contest 专用路由，不能由普通
提交接口伪造比赛上下文。

### GET `/api/v1/submissions/{submission_id}`

返回提交元数据与当前判题结果。

只有提交拥有者、管理员或明确授权的可信内部调用方可以读取；响应不返回源码正文、
源码对象 key 或 MinIO 凭据。对普通用户，资源不存在与跨用户读取统一返回 NotFound，
避免泄露 Submission 是否存在。

### GET `/api/v1/submissions`

Query：

```text
page
page_size
problem_id
status
language
user_id（仅管理员或可信内部调用方可指定其他用户）
```

默认只查询当前用户有权访问的提交，按 `created_at DESC, id DESC` 稳定排序。
最近提交直接使用该接口的第一页和较小的 `page_size`，不提供单独的
`ListRecentSubmissions`。

### POST `/api/v1/submissions/{submission_id}/rejudge`

管理员因题目数据变化发起重判。Judge Service 在一个事务中把原 Submission
标记为 `INVALIDATED`、写入 `submission.invalidated` Outbox，并为原用户创建
一个使用当前 active `judge_revision` 的新 Submission 和 `judge.requested`
Outbox。新提交复用原源码对象；不保存 parent/root/origin 关系。

原结果即使是 AC 也立即失效。Contest Service 消费
`submission.invalidated` 撤销旧结果，之后以新 Submission 的
`submission.judged` 为准。

Create 和 Rejudge 的 `idempotency_key` 均为必填 UUID。同一调用身份、操作类型
和 key 的重复请求必须返回首次响应，不得重复创建 Submission；管理员需要
再次主动重判时使用新的 key。

### GET `/api/v1/submissions/{submission_id}/events`

Gateway 第一版以短轮询 `judge-service/GetJudgeResult` 实现 SSE，不直接消费 RabbitMQ。
连接建立后发送当前 `submission.snapshot`，状态发生变化时发送
`submission.updated`；进入 `DONE`、`CANCELLED` 或 `INVALIDATED` 后发送最终事件并
关闭连接。默认轮询间隔为 `500ms`，最大连接时长为 `2m`，由 Gateway 配置控制。

下游调用失败时发送一次 `submission.error` 后关闭；客户端断开或请求超时必须停止
轮询并释放 ticker。该接口不承诺 exactly-once 事件投递，客户端应按 submission 状态
处理重复快照。

SSE 判题状态流。

示例：

```text
event: status
data: {"status":"COMPILING"}

event: progress
data: {"current_case":3,"total_cases":15}

event: done
data: {"status":"AC","time_ms":32,"memory_kb":4096}
```

---

## 3.5 Contest

### GET `/api/v1/contests`

比赛列表。Gateway 透传分页参数 `page`、`page_size` 和状态筛选，Contest Service 负责比赛可见性和归档状态判断。

### GET `/api/v1/contests/{contest_id}`

比赛详情。Gateway 仅转发请求，比赛是否存在以及归档比赛可见性由 Contest Service 判断。

### POST `/api/v1/contests`

管理员创建比赛。Gateway 从认证上下文透传用户身份，管理员权限和比赛字段校验由 Contest Service 执行。

### PUT `/api/v1/contests/{contest_id}`

更新比赛。`contest_id` 以路径参数为准，只有仍处于 DRAFT 状态的比赛允许更新。

### DELETE `/api/v1/contests/{contest_id}`

归档比赛。Gateway 不直接删除数据，归档权限和状态由 Contest Service 执行。

### POST `/api/v1/contests/{contest_id}/join`

参加比赛。用户身份来自 JWT/Auth Context，不接受请求体中的 `user_id`。仅允许在比赛
开始前加入；重复请求保持幂等并返回已有报名时间。比赛开始后、已结束或已归档的比赛
拒绝报名。

### POST `/api/v1/contests/{contest_id}/problems/{problem_id}/submissions`

创建比赛提交。Gateway 从认证上下文取得用户身份，Contest Service 校验比赛状态、
时间窗口、报名关系和题目归属后，调用 Judge Service 创建带 `contest_id` 的
Submission。比赛提交与普通提交共用 MinIO、Transactional Outbox、RabbitMQ 和
Judge Worker；其 `judge_revision` 在创建/投递 `judge.requested` 时读取题目的当前
active revision，提交后保持 immutable。

### GET `/api/v1/contests/{contest_id}/leaderboard`

比赛排行榜。Gateway 透传分页参数并返回 Contest Service 计算的排名、罚时和每道题结果；不访问 Judge Service、Submission 数据库或 Redis。

---

## 3.6 Agent

### 服务健康探针

Agent Service 的探针不经过 Gateway，供容器编排使用：

```text
GET /healthz
GET /readyz
```

`/healthz` 只检查进程路由，成功响应为 `{"service":"agent-service","status":"ok"}`。
`/readyz` 连接 Agent 自有 `oj_agent` Schema 并执行 `SELECT 1`；成功返回 `200` 和
`{"service":"agent-service","status":"ready","checks":{"database":"ready"}}`，
数据库未配置或不可用时返回 `503`，`database` 分别为 `not_configured` 或 `unavailable`。
探针不会创建表，也不会
访问其他服务的数据库。

### Chat 接入状态（PR3）

PR3 已接通 `Gateway -> Agent Service` 的受保护 SSE 链路。业务客户端只能访问 Gateway
的外部地址；开发 Compose 中 Agent 不映射宿主机端口，只挂载 Gateway 的公钥。
Gateway 始终注册受保护的路由，`clients.agent.enabled=false` 时返回 503；Agent 只有
在 `AGENT_CHAT_ENABLED=true` 并配置数据库、公钥与运行时后才注册内部 Chat 路由。Agent
未启用时返回 404，不保留可匿名调用的临时入口。

外部请求先由 Gateway 的现有 Access Token middleware 认证。Gateway 从已认证的请求上下文
签发短期 RS256 委托 JWT（固定 `iss=go-oj-gateway`、`aud=agent-service`、
`sub=gateway-service`、`rpc=HTTP POST /api/v1/agent/chat`、`kid`），只把用户 ID、角色、
请求 ID 和唯一 token ID 转给 Agent。Agent 只信任该 JWT，不信任请求体或伪造的身份 Header；
委托 token 的有效期不超过 60 秒。直接访问 Agent 时也必须具有合法的内部委托；
替换 `X-User-ID` 等 Header 或携带外部用户 JWT 都不能建立 Chat Run。
Gateway 在建流前校验请求 ID 为 1–128 个字符、角色最多 32 项且每项为 1–64 个字符；
异常身份字段返回 400，不伪装成上游故障。两端均只接受合法 UTF-8 身份文本。

### POST `/api/v1/agent/chat`

通过 Gateway 发起一次 Agent 会话，成功响应为 UTF-8 SSE。请求使用
`Content-Type: application/json`，不接受压缩编码；请求体默认上限 256 KiB，Gateway 和
Agent 都会检查该限制。请求字段如下：

```json
{
  "conversation_id": "363f7d21-b923-41ee-8702-71b2a1280239",
  "agent_key": "learning_assistant",
  "skill_key": "algorithm_explain",
  "message": "为什么我的 submission 90001 一直 WA？",
  "context": {
    "submission_id": 90001,
    "problem_id": 42,
    "language": "cpp"
  }
}
```

`message` 必须是 1–32,000 个字符且不能全为空白；新会话可省略 `conversation_id`，
继续会话时必须提供 UUID；
PR5 新增可选 `agent_key`、`skill_key`，格式为 `[a-z][a-z0-9_]{1,63}`。数据库模式省略
Agent 时使用部署默认 key。数据库真实模型模式省略 Skill 时，固定 Agent 绑定的可用技能
UUID、名称、适用说明和渲染文本，交给模型在本次回答中自行参考；缺少 Context 的技能不参与，
若全部缺少则返回 `AGENT_PROMPT_CONTEXT_REQUIRED`。默认 Skill 排在参考集合首位，
运行快照的 skill_key/skill_id 是兼容默认绑定标识，不代表模型实际采用的技能。
显式 Skill 仍只选择绑定集合中的 key，沿用单技能说明/权限/预算行为；Fake 模式沿用默认 Skill。
普通聊天页面只发送 Agent，不提供 Skill 选择。Demo 模式只接受省略或 `demo`。
管理员/test Agent 仅允许可信角色 `admin`、`system_admin` 或 `agent_admin`，不可访问、归档、停用、
依赖停用、测试到期与不存在统一返回 404。不同 Agent 不可共享会话，返回 409。
Prompt 所需 Context 变量缺失返回 400 `AGENT_PROMPT_CONTEXT_REQUIRED`，不固定预取业务数据。
`context` 只接受正数 `submission_id`/`problem_id` 和 1–32 个字符的 `language`。顶层和
`context` 都拒绝额外字段，调用方不能从请求体指定 `user_id`、`role` 或工具权限。

Gateway 在建立上游流前可能返回以下稳定错误：

| HTTP | reason / code | 语义 |
| --- | --- | --- |
| 400 | `GATEWAY_AGENT_INVALID_ARGUMENT` 或 `GATEWAY_AGENT_REJECTED`；内部 `AGENT_INVALID_ARGUMENT` | JSON 或字段校验失败 |
| 401 | `GATEWAY_UNAUTHENTICATED` | 外部 Access Token 无效 |
| 404 | `GATEWAY_AGENT_REJECTED` / `AGENT_CONVERSATION_NOT_FOUND`、`AGENT_CONFIGURATION_NOT_FOUND` | 会话无权限或 Agent 配置不可用 |
| 409 | `GATEWAY_AGENT_REJECTED` / `AGENT_ACTIVE_RUN_CONFLICT`、`AGENT_CONVERSATION_AGENT_CONFLICT` | 同会话有活动 Run 或跨 Agent 续聊 |
| 413 | `GATEWAY_AGENT_REQUEST_TOO_LARGE` 或 `GATEWAY_AGENT_REJECTED`；内部 `AGENT_REQUEST_TOO_LARGE` | 请求体超过上限 |
| 415 | `GATEWAY_AGENT_UNSUPPORTED_MEDIA_TYPE` 或 `GATEWAY_AGENT_REJECTED`；内部 `AGENT_UNSUPPORTED_MEDIA_TYPE` | 非 JSON 或使用压缩编码 |
| 502/504 | `GATEWAY_AGENT_UNAVAILABLE` / `GATEWAY_AGENT_TIMEOUT` | Gateway 无法建立受保护的上游流 |
| 503 | `GATEWAY_AGENT_DISABLED`、`GATEWAY_AGENT_CAPACITY` 或 `GATEWAY_AGENT_REJECTED`；内部 `AGENT_CAPACITY_EXCEEDED` / `AGENT_UNAVAILABLE` | 功能关闭、并发槽耗尽或 Agent 不可用 |

错误响应只返回稳定 code，不回显请求体、JWT、Prompt、数据库连接串或 Provider 异常。
外部 Gateway 错误沿用 Kratos 的数值 `code`、字符串 `reason` 和 `message`；表中 `AGENT_*`
错误是内部 Agent 的字符串 `code`，Gateway 只按允许的 HTTP 状态映射，不透传上游正文。
上游不在允许映射表中的状态（例如 500、重定向或异常 401）会被 Gateway 统一转成
`502 GATEWAY_AGENT_INVALID_RESPONSE`。

成功响应包含以下 Header：

```http
Content-Type: text/event-stream; charset=utf-8
Cache-Control: no-cache, no-transform
X-Accel-Buffering: no
X-Agent-Run-ID: <canonical UUID>
X-Agent-Conversation-ID: <canonical UUID>
```

每个 SSE 帧的 `event` 与 JSON `type` 一致，`data` 是完整的事件 envelope：

```text
event: thinking
data: {"type":"thinking","run_id":"...","conversation_id":"...","sequence":1,"data":{"text":"正在整理信息"}}

event: token
data: {"type":"token","run_id":"...","conversation_id":"...","sequence":2,"data":{"text":"【演示回答】尚未接入真实模型。"}}

event: done
data: {"type":"done","run_id":"...","conversation_id":"...","sequence":3,"data":{}}
```

当前运行时只生成 `thinking`、`token`、`done`、`error` 四类事件；`thinking` 是短状态，
不表示思维链，也不暴露工具内部数据。`error` 的 `data` 只有稳定错误码，`done` 的
`data` 必须为空，并表示答案及 `COMPLETED` 状态已经持久化。事件序号在一个 Run 内严格
递增，Gateway 会校验 UTF-8、UUID、事件类型、公开错误码、帧大小和序号后再转发；中间
帧可能是 SSE 注释形式的 `: heartbeat`。不提供 SSE 续传或 `Last-Event-ID` 语义。

客户端断开、Gateway 总时限/空闲时限、上游 EOF 或写入失败都会取消 Run 或发送稳定的
`error` 事件；没有收到 `done` 的 EOF 不能视为成功。一次接受请求创建新 Run，不按
`request_id` 幂等，调用方不能自动重试 Chat POST。普通用户（包括管理员身份）只能访问
本人会话；跨用户管理查询属于后续控制面。

### PR6 真实 Responses 模型

外部 Chat 请求和 SSE envelope 不变，Gateway 仍验证 Access JWT、签发可信委托并校验/转发有界 SSE。新增模型错误只使用已有 error 事件的稳定 code：`AGENT_MODEL_AUTH_FAILED`、`AGENT_MODEL_QUOTA`、`AGENT_MODEL_RATE_LIMIT`、`AGENT_MODEL_UNAVAILABLE`、`AGENT_MODEL_TIMEOUT`、`AGENT_MODEL_INVALID_RESPONSE`、`AGENT_MODEL_REJECTED`、`AGENT_MODEL_INPUT_LIMIT`、`AGENT_MODEL_OUTPUT_LIMIT`、`AGENT_MODEL_CALL_LIMIT`、`AGENT_TOOL_CALL_LIMIT`、`AGENT_MODEL_BLOCKED`、`AGENT_MODEL_UNSUPPORTED_OUTPUT`、`AGENT_MODEL_ENDPOINT_DENIED`、`AGENT_CREDENTIAL_UNAVAILABLE`。不透传 Provider HTTP 错误正文。

模型配置与选定 Skill 不兼容时，建流前返回 409 `AGENT_CONFIGURATION_UNSUPPORTED`；凭据引用不存在或撤销时返回 503。Gateway 使用已有状态映射，不透传内部错误正文。建流后的模型失败由 SSE error 表示，即使已有部分 token，也不能当成成功答案；done 仍表示最终持久化成功。

内部 Provider 接口使用 Responses：POST `<base_url>/responses`，Bearer 凭据，`stream=true`、`store=false`。Issue #162 PR2 在 input 中按 user/assistant 角色加入预算内已完成的会话问答，最后追加本轮 user 消息；instructions 仅保留安全前缀和解析后的配置提示，不混入历史，不使用 previous_response_id；direct 模式不传 tools。只消费 output_text.delta 和经校验的 completed 终态，推理/工具/拒绝不作为回答转发。每个 Run 固定一个 Provider/Model；配置需要管理员手动替换，没有自动切换 API。

同会话续聊自动取本人最近最多 20 个成功完整轮次（候选最多 64,000 字符），再为本轮必需输入和输出预留预算，按 UTF-8 字节上界与逐消息开销裁剪最旧整轮。失败、取消、中断、RUNNING 和残缺消息不进入模型输入。历史被裁剪不会新增 SSE 事件；必需输入本身超限仍返回已有 AGENT_MODEL_INPUT_LIMIT。新对话没有历史。外部 Chat 请求、响应、错误码、Gateway 委托和 SSE envelope 不变，无需前端传递历史正文或供应商会话 ID。

本阶段没有公开凭据读写 API。`app.credential_cli` 仅提供本机 create/get/list/revoke 和 init-keyring，读取 Key 使用 600 文件或 stdin；查询不会返回原始 Key。`app.config_cli` 扩展 provider kind 和可选的 bootstrap credential-id；仍不能把 actor-id 当作可信管理员角色。

### Issue #162 PR3：只读工具循环

Skill 的 execution_mode 支持 direct 与 react；页面“回答方式”可选择直接回答或读取业务信息后回答。
react 使用 Agent/Skill 工具白名单与 Registry 已启用、角色允许的只读工具交集，模型不能扩大权限。
Responses 请求携带 function schema、parallel_tool_calls=false；校验 completed 中的 function_call 后，
通过现有带 Agent 委托身份的 gRPC 执行工具，再按 call_id 回传 function_call_output。
只回传校验后的 name/call_id/arguments，不重放供应商 reasoning、ID、状态或原始错误。
工具结果仅进入本次模型输入，SSE 不新增工具参数/结果事件；允许模型最终答案引用已授权业务证据。
工具失败回传稳定 Tool 错误，拥有数据的 Go 服务仍校验授权；未读取成功不得声称已获取证据。

每次模型尝试（含重试）累计消耗模型次数、输入和输出预算；工具批次累计限制 max_tool_calls。
参数为最多 8 KiB 的 JSON 对象，拒绝重复键、非有限数字及重复 call_id；批次先校验再执行，
越权/未知工具不执行。工具结果受字节、模型窗口与累计输入上限限制，循环没有自动恢复。
新增稳定 SSE 错误 AGENT_TOOL_CALL_LIMIT；Gateway 明确允许既有模型错误码，仍拒绝任意上游错误。
取消/断连/Run deadline 传播到当前 HTTP 或 gRPC/工具等待；成功保存后才发送 done。
REST/SSE envelope、Proto、数据库表均兼容；更新时应一起发布 Gateway 和 Agent 错误白名单。

### 其他用户 API

`GET /api/v1/users/{user_id}` 仍是 User API，与 Agent Chat 无关。内部对应
`user.v1.UserService/GetUser`；请求用户不是目标用户本人时，必须携带 `admin` 角色上下文，
否则返回权限拒绝。

---


### 管理员 Agent Control Plane API

PR7 提供 Agent、Prompt、Skill 的管理 HTTP API 和聊天目录，全部经 Gateway。两端均校验
管理员角色 `admin`、`system_admin`、`agent_admin`，普通用户的管理请求返回 403。
内部 RS256 委托绑定 `HTTP <method> <具体路径>`，含资源 key/UUID；Chat、查询或其他资源的
委托不能用于本次写操作。Agent 只接受 Gateway 签名身份，不信任正文 actor_id/role 或身份 Header。
管理开关为 `AGENT_ADMIN_ENABLED`，默认关闭；数据库和 Gateway 公钥必须配置。
管理可独立于 Chat 启用，不要求模型 Key 可解密或启动 Runtime；关闭时返回 503。

对 `agents`、`prompts`、`skills` 三类资源分别提供下列操作，花括号表示明确的资源路由：

```text
GET  /api/v1/admin/agent/{agents|prompts|skills}
POST /api/v1/admin/agent/{agents|prompts|skills}
GET  /api/v1/admin/agent/{agents|prompts|skills}/{key}
PUT  /api/v1/admin/agent/{agents|prompts|skills}/{key}
GET  /api/v1/admin/agent/{agents|prompts|skills}/{key}/versions
GET  /api/v1/admin/agent/{agents|prompts|skills}/{key}/versions/{uuid}
GET  /api/v1/admin/agent/{agents|prompts|skills}/versions/{uuid}
POST /api/v1/admin/agent/{agents|prompts|skills}/{key}/archive
POST /api/v1/admin/agent/{agents|prompts|skills}/{key}/restore
POST /api/v1/admin/agent/{agents|prompts|skills}/{key}/disable
POST /api/v1/admin/agent/{agents|prompts|skills}/{key}/enable
GET  /api/v1/admin/agent/model-options
GET  /api/v1/admin/agent/model-options/{uuid}
GET  /api/v1/admin/agent/tools
GET  /api/v1/agent/agents
GET  /api/v1/agent/agents/{agent_key}/skills
```

所有请求需外部 Bearer Access Token。写操作必须显式携带 canonical UUID 格式的
`X-Request-ID`，作为同一次操作重试的幂等编号；缺失/格式错误返回 400。同一操作者、
操作/资源、预期 UUID、正文与同编号重试返回原操作结果；同编号不同操作/内容/操作者返回
409 `AGENT_CONFIGURATION_REQUEST_CONFLICT`。401 刷新重放也必须保留原编号；Chat POST
不适用这套幂等约定。回放旧结果可能已经归档，客户端需重新读取 current。

创建正文为 `{"content":{"name":"算法基础规则","text":"用中文解释算法","variables":[]}}`。
省略 key 时，后端由资源类型、可信操作者和请求 UUID 生成稳定标识，重试返回同一资源；
名称允许重复，修改名称不改变 key。返回 configuration.key 是后续 URL/Chat 引用的标识。
兼容旧客户端显式 key 创建，既有 key 和历史 UUID 不改写。
PUT 正文为 `{"expected_id":"<当前 UUID>","content":{...完整内容...}}`。归档/启停正文为
`{"expected_id":"<当前 UUID>"}`，恢复为 `{"expected_id":"<当前 UUID>","source_id":"<历史 UUID>"}`。
不接受正文 actor_id/role/kind/request_id 或未知字段。保存结果统一为
`{"configuration":{"id":"...","kind":"prompt","key":"...","archived":false,"disabled":false,
"created_at":"...","created_by":7,"archived_at":null,"archived_by":null,"content":{...}}}`。
创建、替换与操作成功均返回 200，响应 `Cache-Control: no-store`。

列表支持 `page`（默认 1，最大 501）、`page_size`（默认 20，最大 100）、`search`（name 或 key 子串，
最多 64 字符）和 `state=active|archived|disabled|all`。active 表示当前未归档，仍包含停用
记录；disabled 筛选当前未归档且停用的记录。响应为 `{"items":[...摘要...],"page":{"page":1,
"page_size":20,"total":...}}`，列表不返回完整 Prompt/正文。资源按 created_at/id 降序，
历史版本按版本 created_at/id 降序；total/items 在同一数据库快照中查询。详情无分页参数，
允许读取归档记录；历史 UUID 必须匹配路径 kind/key。拒绝重复/未知查询参数。
历史列表、模型选项与 Tools 不支持状态筛选，非 active 的 state 返回 400。
PR8 增加按 kind/UUID 查询历史详情的只读入口（无需先查 key）；UUID 必须为规范小写格式，
kind 不匹配返回 404，详情禁止查询参数。与 key/UUID 详情使用相同配置 DTO、管理员权限及具体路径委托。

创建已有 key（含归档）或 stale expected_id 返回 409 `AGENT_CONFIGURATION_STALE`，不会
自动覆盖。输入/引用/工具不合法返回 400，未实现执行模式/非允许模型返回 409
`AGENT_CONFIGURATION_UNSUPPORTED`，不存在返回 404，数据库/凭据依赖不可用返回 503。
Gateway 只返回允许的稳定错误码，不透传异常正文。请求体默认最多 256 KiB，JSON 响应最多
1 MiB，均不接受压缩请求；管理请求使用普通 HTTP deadline，不占用 Chat 的长时间预算。

Agent/Prompt/Skill content 使用严格配置 Schema。Prompt 为 name/text/variables，变量仅
language/problem_id/submission_id。新 Skill 为 name/description/text/execution_mode/allowed_tools，
text 是 1–32,000 字符的任务说明，description 最多 512 字符；文本默认是普通正文，花括号不展开。
Skill 不设置预算时继承 Agent 和部署上限。兼容旧 Skill 的 prompt_id/budget；text 与 prompt_id
必须二选一。前端编辑旧 Skill 时复制历史 Prompt 正文为内置文本，保留 variables（包括空列表，
用于旧模板字面括号转义），保存生成新版本并移除旧 Prompt 绑定/Skill 预算；旧版本继续有效。
内置 variables 仅用于兼容迁移，声明时沿用 Prompt 模板校验。
Agent 为 name/prompt_id/skill_ids/default_skill_id/model_profile_id/allowed_tools/budget/
visibility/is_test/test_expires_at/knowledge_scope。RAG PR2 起知识范围支持分组 UUID；运行模式支持 direct/react。
test Agent 必须管理员可见、到期时间带时区且在未来。程序安全前缀不可编辑。

真实模式的模型选项是只读分页接口，返回已有 Responses Model Profile 的 UUID/key/model、可选生成
参数及上下文/输出上限，只允许部署 `AGENT_ADMIN_PROVIDER_KEY`（默认 su8）下的可用模型。
保存 Agent 时在事务中复核模型与 Provider，不能靠直接提交 UUID 绕过；不返回 Provider
正文、Credential ID、Key/密文。开发环境自动准备 deepseek-v4-flash、deepseek-v4-pro、deepseek-v4.1-flash，目录来自数据库，不在前端写死。
Provider/模型/Key 的管理仍使用本机 CLI，没有新建 Provider/凭据 HTTP 写接口。
`GET model-options/{uuid}` 用于显示已有的历史模型绑定，额外返回 archived/disabled/available
布尔值；available 表示可用于新绑定，归档版本返回 false，但仍显示保留的版本信息。
该接口只允许部署指定 Provider 的 Responses Profile，不属于该范围或 UUID 不存在返回 404。
禁用或部署网络策略不允许时仍可查元数据，available=false；响应严格禁止 Provider/凭据字段。

Tools 仅返回 Registry 的只读元数据和输入输出 Schema，不提供调用/统计/编辑接口。
普通 Agent 目录只返回 key/name/is_default，绑定 Skill 返回 key/name/description/execution_mode/default
及 default_skill_key。权限/可运行条件在 SQL 分页前过滤，普通用户看不到 test/admin Agent，
不可访问 Skill 子目录统一 404；目录不返回 Prompt、模型、Provider 或预算。Chat 每次重新
校验权限。Chat 关闭、demo 配置或 Runtime disabled 时目录为空，不将数据库配置伪装为可运行。
数据库聊天目录独立于管理开关；Chat 和管理均关闭时目录返回 503。

Agent/Prompt/Skill/模型配置不采用草稿发布；创建完整校验后立即生效，修改生成新 UUID，
原记录归档，删除只归档。替换携带预期当前编号及请求编号，实现并发检查和幂等重放。
旧配置恢复通过复制生成新编号；共享 Prompt 替换不自动修改已有 Agent 或旧版 Skill；Skill 替换不自动重绑 Agent。

归档用于版本历史，不抹除已有绑定；已有 Agent 可继续使用被归档的 Prompt/Skill 具体
UUID。停用是资源级操作，会阻止后续 Run 使用其任一版本；已开始 Run 保留配置快照。
Agent 自身归档也会阻止新的 Run。恢复不自动启用已停用资源。

以下旧目标列表中超出上述 PR7 接口的功能（Tools 详情/统计、RAG、Eval、观测等）仍为后续
设计；PR8 已接管理页面、配置版本绑定和顶栏/工作区路由，PR9 已接 Agent/Skill 目录选择和
Gateway 流式对话。Chat 不自动重试，401 刷新后由用户再次发送；停止、新对话、切换 Agent
和离开页面会中止流，只有合法 done 才表示完成。多轮上下文仍未实现。

Prompt：

```text
GET  /api/v1/admin/agent/prompts
POST /api/v1/admin/agent/prompts
PUT  /api/v1/admin/agent/prompts/{prompt_key}
GET  /api/v1/admin/agent/prompts/{prompt_key}/versions
POST /api/v1/admin/agent/prompts/{prompt_key}/archive
POST /api/v1/admin/agent/prompts/{prompt_key}/restore
```

Prompt 编辑只能修改安全前缀之外的模板内容、变量说明和适用 Skill。服务端必须校验
变量白名单、敏感信息、Tool 名称和禁止指令；不能通过 Prompt 授予 RPC 权限或删除
安全前缀。

Tool Catalog：

```text
GET  /api/v1/admin/agent/tools
GET  /api/v1/admin/agent/tools/{tool_name}
GET  /api/v1/admin/agent/tools/{tool_name}/metrics
```

Tool Catalog 返回描述、输入输出 Schema、来源 RPC、敏感级别、允许角色、side effect、
超时、调用统计和当前启用策略。RPC 方法、目标服务、Tool 实现和最终授权代码只读展示。
Tool 是否对某个 Skill 可用，通过 Skill 的 Tool allowlist 管理。

Skill：

```text
GET  /api/v1/admin/agent/skills
POST /api/v1/admin/agent/skills
GET  /api/v1/admin/agent/skills/{skill_key}
PUT  /api/v1/admin/agent/skills/{skill_key}
POST /api/v1/admin/agent/skills/{skill_key}/archive
POST /api/v1/admin/agent/skills/{skill_key}/restore
```

Skill 版本可以修改描述、Prompt 绑定、Tool allowlist、执行模式、输出 Schema、Tool
次数、计划步数和模型预算。不能执行任意 Python，也不能引用代码 Registry 中不存在
的 Tool。

知识库：

```text
GET  /api/v1/admin/agent/knowledge/documents
POST /api/v1/admin/agent/knowledge/documents
GET  /api/v1/admin/agent/knowledge/documents/{document_id}
PUT  /api/v1/admin/agent/knowledge/documents/{document_id}
POST /api/v1/admin/agent/knowledge/documents/{document_id}/versions
POST /api/v1/admin/agent/knowledge/documents/{document_id}/preview-chunks
POST /api/v1/admin/agent/knowledge/documents/{document_id}/index
POST /api/v1/admin/agent/knowledge/documents/{document_id}/publish
POST /api/v1/admin/agent/knowledge/documents/{document_id}/archive
```

知识库 API 管理文档元数据、版本、切分、索引状态、发布和归档。文档原文存 MinIO，
向量存 Qdrant；检索结果必须带文档版本和引用位置。

Eval 与观测：

```text
GET  /api/v1/admin/agent/evals/datasets
POST /api/v1/admin/agent/evals/runs
GET  /api/v1/admin/agent/evals/runs/{run_id}
GET  /api/v1/admin/agent/evals/runs/{run_id}/results
GET  /api/v1/admin/agent/evals/runs/{run_id}/results/{result_id}
GET  /api/v1/admin/agent/metrics
GET  /api/v1/admin/agent/runs
GET  /api/v1/admin/agent/runs/{run_id}
GET  /api/v1/admin/agent/audits
```

当前已接入的 Eval 数据集和程序化评测接口：

```text
GET  /api/v1/admin/agent/evals/datasets
POST /api/v1/admin/agent/evals/datasets
GET  /api/v1/admin/agent/evals/datasets/{dataset_id}
GET  /api/v1/admin/agent/evals/datasets/{dataset_id}/cases
GET  /api/v1/admin/agent/evals/runs
POST /api/v1/admin/agent/evals/runs
GET  /api/v1/admin/agent/evals/runs/{run_id}
POST /api/v1/admin/agent/evals/runs/{run_id}/cancel
GET  /api/v1/admin/agent/evals/runs/{run_id}/results
GET  /api/v1/admin/agent/evals/runs/{run_id}/results/{result_id}
```

上传正文是受限 JSON 请求，包含 `name`、`description` 和 UTF-8 JSONL `content`。Agent
服务逐行校验 Case，只接受 `user`/`assistant` 对话并拒绝重复 ID、system/tool 伪造消息和
超限内容；原始 JSONL 以不可变对象写入 Agent 专用 MinIO，MySQL 保存哈希、元数据和
校验后的 Case 投影。重复内容哈希返回已有数据集，不重复创建。上述接口均要求管理员角色，
普通用户由 Gateway 和 Agent 双重拒绝。

运行创建请求包含 `dataset_id`（UUID）、`agent_key`、`mode=programmatic`、
`repeat_count`（默认 1，范围 1–3）和 `concurrency`（默认 1，范围 1–8）。
只支持程序化模式，未实现的 manual/programmatic_review 返回 400。
服务在创建时固定 Agent/Prompt/Skill/Model/Provider/知识版本；每个 Case 的 user turn
通过普通 Runtime 按真实生成历史执行，assistant turn 仅保留为参考。
fixture.principal 必须是部署 `AGENT_EVAL_PRINCIPALS` 中配置的专用账号别名，执行角色固定 user，
未知别名只使该 Case 失败（AGENT_EVAL_PRINCIPAL_INVALID），不能继承创建者的管理员角色。
评测会话在数据库标为 purpose=eval，普通历史列表、详情、Run 查询及聊天续聊仅允许 purpose=chat；
即使用评测账号本身的普通 JWT，也不能读取或继续 Eval 会话。

运行状态为 `QUEUED`、`RUNNING`、`COMPLETED`、`FAILED` 或 `CANCELLED`。
COMPLETED 表示调度执行完成，质量结论看每条 checks.pass 以及 hard_failures/error_count。
取消使用 `POST /runs/{uuid}/cancel?version={正整数}` 与规范 UUID `X-Request-ID`，
JSON 正文为 `{}`。version 不匹配或已终态返回 409 AGENT_EVAL_VERSION_CONFLICT；同一取消 request ID
即使 version 已过期仍幂等返回当前状态。取消停止活跃 SSE/模型流并将排队 Case 记为 CANCELLED。
单实例服务重启时遗留运行标记 FAILED，保留已保存结果，本阶段不自动续跑。
Run 冻结后，配置替换/停用不改变该批次的固定内容；新建 Run 会重新校验停用状态。
凭据撤销、业务 RPC 权限与 RAG 当前授权仍由原 Runtime 在实际调用时检查，不因快照豁免。

Run 列表使用 page/page_size（最多 100），可选 status，按 created_at/id 倒序；
结果列表同样分页，可选 status=COMPLETED/FAILED/CANCELLED，按 case_id/attempt 稳定排序，
未知 Run 返回 404。列表只返回基础元数据、512 字符 `answer_preview` 和 checks 的 pass/hard_fail，
避免完整回答与检查在一页聚合后超出 Gateway 的 1 MiB 响应上限。
`GET /runs/{run_id}/results/{result_id}` 返回 `{result}`，包含各轮回答（至多 32000 字符）、
完整程序化检查与有界模型/工具/知识摘要；未知结果或 run/result 不属于同一批次返回 404。
不保存 Provider 原始响应、内部推理、工具原文和凭据。
required_facts/forbidden_claims/must_not_leak 是对实际回答的大小写不敏感字面检查，
required_tools/forbidden_tools 使用实际调用名称，required_evidence 使用实际来源题号/标题/章节。

同一管理员和同一正文使用相同 `X-Request-ID` 创建只返回已有运行，正文或创建者不同返回
409 AGENT_EVAL_REQUEST_CONFLICT；重试不重新解析活动配置。达到部署准入上限返回
503 AGENT_EVAL_UNAVAILABLE。创建响应 `{run,duplicate}`，运行详情/取消响应 `{run}`，
列表响应 `{items,page:{page,page_size,total}}`。公开 run.config_snapshot 只含配置 UUID/hash、
知识范围/版本、执行模式和预算，不含 Prompt/Skill 正文、Provider 地址或凭据配置。
运行列表的快照进一步将 knowledge_versions 列表替换为 knowledge_version_count；
完整版本清单通过单条运行详情读取，保证一页多条大知识范围的运行仍有界。

Eval 运行必须记录配置版本快照。运行详情默认返回脱敏的意图、Skill、Tool 调用、
延迟、Token、证据引用、答案状态和评分；源码、凭据、完整 Prompt 和原始敏感 Tool
Result 不直接返回浏览器。

所有管理员 API 都必须执行 RBAC、输入校验、乐观并发控制、审计记录和稳定分页。
实施这些 HTTP 接口时，需要同步补充 Gateway Proto/OpenAPI、Python Client、管理员
前端和权限负向测试。

## 4. Internal gRPC API

Proto 目录：

```text
api/
├── common/v1/
├── user/v1/
├── problem/v1/
├── submission/v1/
├── contest/v1/
└── judge/v1/
```

---

## 4.1 UserService

```protobuf
syntax = "proto3";

package user.v1;

service UserService {
  rpc Register(RegisterRequest) returns (RegisterResponse);
  rpc Login(LoginRequest) returns (LoginResponse);
  rpc RefreshToken(RefreshTokenRequest) returns (RefreshTokenResponse);
  rpc Logout(LogoutRequest) returns (LogoutResponse);
  rpc GetCurrentUser(GetCurrentUserRequest) returns (GetCurrentUserResponse);
  rpc GetUser(GetUserRequest) returns (GetUserResponse);
}
```

User Service 第一阶段需要实现以下接口：

### `Register`

创建用户账户。

- 参数校验优先使用 `validate/validate.proto` 注解生成的 Go 校验代码。
- 校验用户名、邮箱和密码格式。
- username 注册前执行 trim + lowercase，按大小写不敏感处理。
- email 注册前执行 trim + lowercase，按大小写不敏感处理。
- 密码策略保持简单：trim 后不能为空，最小 8 个字符，最大 72 bytes。
- 检查用户名和邮箱是否已经存在。
- 使用 bcrypt 保存密码 Hash，默认 cost 为 12，禁止保存明文密码。
- 创建用户后分配默认 `user` 角色。
- 返回新用户的基础资料，不返回 `password_hash`。
- 当前已接入 user-service 的 Proto handler 和 MySQL Repository。

### `Login`

使用账号和密码登录。

- `account` 可以匹配用户名或邮箱。
- 校验密码 Hash。
- 返回短生命周期 Access Token，默认 TTL 为 15 分钟。
- Access Token 使用 JWT，签名算法为 HS256，payload 可以携带 `roles`。
- HS256 secret 只允许 `user-service` 和 `gateway` 持有。
- Access Token Claims 与 HS256 校验语义由公共 Go 包 `pkg/auth` 维护，Gateway 后续复用该 verifier 生成可信请求上下文。
- 返回 Refresh Token，Refresh Token 使用高熵不透明字符串，服务端保存 SHA-256 hash。
- 登录失败时不要泄露“用户不存在”或“密码错误”的具体差异。
- 当前已接入账号查询、bcrypt 密码校验和 `TokenIssuer` 调用。
- 当前已接入 HS256 JWT 签发和 Redis Refresh Token 保存。

### `RefreshToken`

使用 Refresh Token 获取新的 Access Token。

- 校验 Refresh Token 是否存在、未过期、未撤销。
- 每次成功刷新时轮换 Refresh Token。
- 轮换时撤销旧的 Refresh Token，避免重复使用。
- Redis 中只保存 Refresh Token 的 SHA-256 hash 对应记录，不保存原文。
- 不允许通过 Refresh Token 直接改变用户身份或角色。

### `Logout`

使用 Refresh Token 注销登录 Session。

- 对 Refresh Token 计算 SHA-256 hash 后读取服务端记录，不存储或记录 Token 原文。
- 撤销同一 Session 下的全部 Refresh Token，而不只撤销请求中携带的 Token。
- 对已撤销、已过期或未知 Token 保持幂等成功。
- Redis 或其他内部依赖故障仍返回内部错误，不伪装成注销成功。
- 无状态 Access Token 不进入 Redis denylist，继续依赖短 TTL 到期。

### 管理员 bootstrap

管理员账号不通过普通注册接口创建，使用一次性 bootstrap 命令初始化。

- 从环境变量读取数据库 DSN、管理员用户名、邮箱和密码。
- 密码使用 bcrypt 哈希后写入数据库。
- 如果系统中已经存在 `admin` 角色用户，命令幂等跳过，不覆盖或增发管理员。
- bootstrap 命令输出 JSON 结构化日志，普通日志走 stdout，错误日志走 stderr。

### `GetCurrentUser`

获取当前认证用户的基础资料。

- 从受信任的内部 `RequestContext.user_id` 获取用户身份。
- 不能直接信任外部客户端传入的同名 Header。
- 返回用户基本信息和角色列表。
- 用于 Gateway 和其他受控内部调用。

### `GetUser`

按用户 ID 获取最小必要的用户资料。

- 最终权限校验由 User Service 执行。
- 只返回调用方业务所需的最少字段。
- 不返回密码 Hash、Refresh Token 或其他认证敏感数据。
- Agent Tool 只能通过该 gRPC 契约获取被授权的用户上下文。

接口用途：

- Gateway 获取用户信息。
- Gateway 调用 `Register`、`Login`、`RefreshToken`、`Logout` 和 `GetCurrentUser` 支持外部认证流程。
- 其他服务按业务需要通过 `GetUser` 获取最少用户资料。
- Agent Tool 通过 `GetCurrentUser` 或受控的 `GetUser` 获取当前用户上下文。
- User Service 负责最终的用户资源权限校验，Gateway 和 Agent 不负责替代该校验。

---

## 4.2 ProblemService

```protobuf
syntax = "proto3";

package problem.v1;

service ProblemService {
  rpc CreateProblem(CreateProblemRequest) returns (CreateProblemResponse);
  rpc UpdateProblem(UpdateProblemRequest) returns (UpdateProblemResponse);
  rpc ArchiveProblem(ArchiveProblemRequest) returns (ArchiveProblemResponse);
  rpc GetProblem(GetProblemRequest) returns (GetProblemResponse);
  rpc ListProblems(ListProblemsRequest) returns (ListProblemsResponse);
  rpc AddTestcase(AddTestcaseRequest) returns (AddTestcaseResponse);
  rpc ArchiveTestcase(ArchiveTestcaseRequest) returns (ArchiveTestcaseResponse);
  rpc ListProblemTestcases(ListProblemTestcasesRequest)
      returns (ListProblemTestcasesResponse);
  rpc GetJudgeProfile(GetJudgeProfileRequest)
      returns (GetJudgeProfileResponse);
}
```

`AddTestcase` 的内部请求携带配对文件名和文件 bytes。`.in/.out` 后缀、文件
大小、题目 ID、版本和序号由生成的校验代码检查；Problem Service 负责生成
MinIO object key，数据库只保存当前编辑态的 object key、hash、大小、序号和状态。

`ListProblemTestcases` 是管理员页面与受信内部调用方的诊断能力。具体授权仍在
Problem Service 内完成，不能信任外部调用者伪造角色；Judge Service 创建提交
不得使用该列表临时拼装测试集。

`GetJudgeProfile` 仅接受通过 RS256 服务间认证的 `judge-service` 调用方，返回
题目 ID、状态、时间/内存限制和当前 `active_judge_revision`。题目归档、没有
有效测试点或尚无已发布 revision 时返回 `FailedPrecondition`。Judge Service
不得通过 `ListProblemTestcases` 临时拼装一次判题。

新增或归档测试点时，Problem Service 先将完整快照写入 MinIO 的不可变
`problem-{problem_id}/judge-revisions/{judge_revision}/` 前缀，最后才在一个
MySQL 事务中提交测试点最新状态并切换 `active_judge_revision`。MySQL 不保存
revision 历史；历史 manifest 与 `.in/.out` 文件只保存在 MinIO。
manifest 顶层保存该题目 revision 的一份 `time_limit_ms` 和
`memory_limit_kb`，同一 revision 下所有测试点共享这组限制。管理员修改任一
限制时，即使测试点正文未变化，也必须发布新 revision 后再切换 active 指针。
manifest 格式为：

```json
{
  "manifest_version": 1,
  "problem_id": 1001,
  "judge_revision": "01K5C6Y7N8P9Q0R1S2T3V4W5X6",
  "time_limit_ms": 1000,
  "memory_limit_kb": 65536,
  "testcases": [
    {
      "case_no": 1,
      "input": {"object_key": ".../1.in", "sha256": "...", "size_bytes": 4},
      "output": {"object_key": ".../1.out", "sha256": "...", "size_bytes": 2}
    }
  ]
}
```

归档最后一个有效测试点时不生成空 revision，而是清空 active 指针并保留旧
MinIO 快照，因此新 Submission 会被拒绝，历史 Submission 仍可复现。

Agent Tool 映射：

```text
get_problem      -> GetProblem
search_problems  -> 后续扩展 ListProblems 筛选能力
recommend_problem -> 后续使用 List + Agent strategy
```

---

## 4.3 SubmissionService

```protobuf
syntax = "proto3";

package submission.v1;

service SubmissionService {
  rpc CreateSubmission(CreateSubmissionRequest) returns (CreateSubmissionResponse);
  rpc GetSubmission(GetSubmissionRequest) returns (GetSubmissionResponse);
  rpc ListSubmissions(ListSubmissionsRequest) returns (ListSubmissionsResponse);
  rpc GetJudgeResult(GetJudgeResultRequest) returns (GetJudgeResultResponse);
  rpc RejudgeSubmission(RejudgeSubmissionRequest)
      returns (RejudgeSubmissionResponse);
}
```

当前五个 Submission RPC 均已在 judge-service 中实现。调用身份只来自经过 RS256
校验的内部 Principal；请求体不携带也不能覆盖用户身份。Create 会在上传 MinIO
源码前检查已完成的幂等记录，并在一个 MySQL 事务内提交 Submission、
`judge.requested` Outbox 和幂等响应。Get 和 GetJudgeResult 对非 owner 返回与不存在
资源相同的 NotFound；List 对普通用户强制使用当前 `actor_id`，管理员才可指定其他
`user_id`。列表按 `created_at DESC, id DESC` 排序，`page_size` 上限为 100。
GetJudgeResult 在判题尚未完成时返回当前状态和空的 Case Results。Rejudge 仅允许
管理员调用，在一个事务内作废旧 Submission、写入 `submission.invalidated`，创建
复用源码对象且固定当前 revision 的新 QUEUED Submission，并写入新的
`judge.requested` Outbox；相同幂等键会重放首次响应。

本阶段只开放内部 gRPC；上文 `/api/v1/submissions` REST 路由仍由后续 Gateway
接入实现。

Agent Tool 映射：

```text
get_submission           -> GetSubmission
get_judge_result         -> GetJudgeResult
list_recent_submissions  -> ListSubmissions(page=1, bounded page_size)
```

Authorization 必须由 Judge Service 执行，而不是 Agent 判断。

### Agent PR4 只读 Tool 与内部身份

PR4 在不改变外部 Chat/SSE 契约的前提下，增加 Agent Service 到 Problem/Judge Service
的只读 gRPC Client。首批映射如下：

```text
get_problem            -> /problem.v1.ProblemService/GetProblem
list_problems          -> /problem.v1.ProblemService/ListProblems
get_submission         -> /submission.v1.SubmissionService/GetSubmission
get_submission_source  -> /submission.v1.SubmissionService/GetSubmissionSource
get_judge_result       -> /submission.v1.SubmissionService/GetJudgeResult
list_submissions       -> /submission.v1.SubmissionService/ListSubmissions
```

Tool 按请求和已发布 allowlist 选择，不构成固定的提交预取链路。算法解释、通用知识问答
和学习计划可以不调用这些 Tool。PR4 不开放 CreateSubmission、RejudgeSubmission、
测试数据或 Judge Worker 内部 RPC。

Agent 使用独立 RSA 私钥为每次调用签发短期 JWT。Token 的 `iss`、`aud`、`sub`、`actor_id`、
`actor_roles`、`request_id`、`jti` 和 `rpc` 必须通过校验，其中 `rpc` 是实际 gRPC
FullMethod。Problem/Judge 的 Agent caller 配置还必须提供只读 RPC allowlist；有效 Agent
签名也不能调用 allowlist 之外的写方法。Agent 不挂载 Gateway 私钥，业务 owner 和角色
授权仍由目标 Go Service 执行。

服务配置中的 Agent caller 应设置 `require_method_allowlist: true`；该开关与 issuer、subject
分离，因此身份名称轮换时仍会对缺少 allowlist 的配置 fail-closed。Gateway、Judge 等历史
caller 可以保留空 allowlist 以兼容既有调用范围。

Tool 结果拥有输入/输出 Schema、deadline、分页上限和响应体积上限。源码、题面和判题
结果超过边界时返回明确的截断证据或稳定错误；依赖不可用、权限不足、资源不存在和
超时映射为脱敏 Tool 错误。判题尚未完成时透传 queued/compiling/running 等实际状态，
不推断不存在的 Verdict。取消沿 Python gRPC 调用传播，Agent 不自动重试这些读取。

---

## 4.4 ContestService

```protobuf
syntax = "proto3";

package contest.v1;

service ContestService {
  rpc CreateContest(CreateContestRequest) returns (CreateContestReply);
  rpc GetContest(GetContestRequest) returns (GetContestReply);
  rpc ListContests(ListContestsRequest) returns (ListContestsReply);
  rpc UpdateContest(UpdateContestRequest) returns (UpdateContestReply);
  rpc ArchiveContest(ArchiveContestRequest) returns (ArchiveContestReply);
  rpc GetLeaderboard(GetLeaderboardRequest) returns (GetLeaderboardReply);
  rpc JoinContest(JoinContestRequest) returns (JoinContestReply);
  rpc CreateContestSubmission(CreateContestSubmissionRequest)
      returns (CreateContestSubmissionReply);
}
```

`GetLeaderboard` 已实现，校验身份和比赛可见性后优先读取经过结构和新鲜度校验的 Redis 代次；
缓存缺失、损坏、过旧或 Redis 故障时读取 Contest 自有 `contest_problem_results` 聚合结果。
按 solved_count DESC、penalty_seconds ASC、user_id ASC 排序，分页参数与比赛列表一致（1 起始，最大 100）。
保留 `rank=1,user_id=2,score=3,penalty=4,accepted_count=5`；新增 `problems=6,solved_count=7,penalty_seconds=8`。
ACM 下 score/accepted_count 等于 solved_count，penalty 等于 penalty_seconds（秒）。每道题返回
`problem_id/solved/wrong_attempts/accepted_at`，未 AC 的 accepted_at 为空。无本地结果的用户不进入榜单。
Gateway 已提供比赛列表、详情、创建、更新、归档、报名、提交和排行榜路由；业务权限、比赛状态和排行榜计算仍由 Contest Service 负责。

`UpdateContest` 是完整替换操作。管理员必须把最近一次 `GetContest` 返回的
`updated_at` 原样放入 `ContestUpdate.expected_updated_at`；服务端在比赛行锁内
重新检查状态和 UTC 时间，并在令牌过期时返回 `ABORTED`，避免并发管理员静默覆盖。
生命周期展示与业务预校验读取 Repository 的数据库 UTC 时钟；比赛时间规范化为 DATETIME(3) 的毫秒精度。
报名、归档和编辑在拿锁后再次读取数据库 UTC 时间；报名只允许 start_at 前。比赛提交采用半开区间 `[start_at,end_at)`，重复报名
按 `(contest_id,user_id)` 主键保持幂等。Redis 排行榜题目结构由比赛配置签名保护，题目
集合变化后旧代次不会继续命中。

内部 `CreateSubmissionRequest` 新增 `contest_start_at=7` 和 `contest_end_at=8`；
普通公开提交端点不能填写比赛字段，Gateway 仅通过 Contest Service 发起比赛提交。
Contest 在兼容的共享比赛行锁内校验报名、题目和数据库时间，再转发稳定区间；Judge 最终 INSERT 用数据库 `UTC_TIMESTAMP(3)`
作为 `created_at/submitted_at` 并检查 `[start_at,end_at)`。跨 RPC、源码上传和
幂等锁等待期间越过截止会回滚提交、Outbox 和幂等预留，已成功请求的重放不重复插入。
部署时先停止比赛写入，再同时更新 Judge 和 Contest，避免旧 Contest 缺少新区间字段；
普通题库提交不受该字段要求影响。Go、Gateway 透传契约已更新；Python/Agent 当前无比赛写入调用。

---

## 4.5 Judge

Judge 主链路优先使用 RabbitMQ，不通过同步 RPC 分发每个任务。

可保留少量内部管理 RPC，例如：

```protobuf
syntax = "proto3";

package judge.v1;

service JudgeAdminService {
  rpc GetWorkerStatus(GetWorkerStatusRequest) returns (GetWorkerStatusReply);
}
```

不要将判题主链路重新耦合成：

```text
Judge Service
  ↓ synchronous RPC
Judge Worker
```

---

## 5. Judge Async Contract

## 5.1 Event Envelope

所有事件使用稳定 Envelope：

```json
{
  "event_id": "uuid",
  "event_type": "judge.completed",
  "event_version": 1,
  "occurred_at": "2026-08-27T00:00:00Z",
  "trace_id": "...",
  "data": {}
}
```

要求：

- `event_id` 全局唯一。
- `event_version` 显式版本化。
- Event Schema 不直接复用 ORM Entity。
- Consumer 必须支持重复投递。
- Breaking Change 需要升级版本或兼容迁移。

---

## 5.2 Outbox Intent：`judge.requested`

Writer：

```text
judge-service / CreateSubmission transaction
```

该记录是 `judge-service` 内部持久化的调度意图，不是 RabbitMQ 公共事件。Outbox Relay 读取后完成任务规范化与路由，并直接发布 `judge.task.<language>`；它不作为需要独立消费的 RabbitMQ 事件暴露给另一个服务。

Payload：

```json
{
  "submission_id": 90001,
  "problem_id": 1001,
  "language": "cpp",
  "judge_revision": "01K5C6Y7N8P9Q0R1S2T3V4W5X6",
  "source_object_key": "sources/01K5C6Y7N8P9Q0R1S2T3V4W5X6/source.cpp",
  "source_sha256": "...",
  "source_size_bytes": 1234,
  "judge_deadline_at": "2026-08-27T00:05:00Z"
}
```

`judge_revision` 引用 Problem Service 已完整发布的不可变测试集 revision。首版没有 priority 字段，所有任务都使用普通优先级。源码正文存 MinIO，消息只携带不可变对象 key 与 hash。

---

## 5.3 `judge.task`

Producer：

```text
judge-service / outbox-relay
```

Consumer：

```text
judge-worker
```

Routing Keys：

```text
judge.task.cpp
judge.task.c
judge.task.go
judge.task.python
judge.task.java
```

Payload 至少包括：

```json
{
  "submission_id": 90001,
  "problem_id": 1001,
  "language": "cpp",
  "judge_revision": "01K5C6Y7N8P9Q0R1S2T3V4W5X6",
  "source_object_key": "sources/01K5C6Y7N8P9Q0R1S2T3V4W5X6/source.cpp",
  "source_sha256": "...",
  "source_size_bytes": 1234,
  "judge_deadline_at": "2026-08-27T00:05:00Z"
}
```

公开消息的 `event_type` 固定为 `judge.task`，具体语言只通过 routing key 区分。
`judge.requested` 不得作为 RabbitMQ 公共事件类型。公共 Envelope 与 Judge 消息
数据结构以 `pkg/mq` 为唯一代码契约，业务体统一放在 `data` 字段。

Worker 使用受限的服务凭据按对象引用读取源码，并根据 `judge_revision` 读取该 revision 的 `manifest.json` 和全部测试点。同一 Submission 不得读取其他 revision，也不得把源码或测试数据正文塞入 MQ。
源码必须匹配 `sources/{ulid}/source.{language-extension}`；manifest key 由 Worker 根据
`problem_id + judge_revision` 构造，测试点 key 必须精确匹配该 revision 下的
`testcases/{case_no}.in|out`。Worker 在执行前校验所有对象的 `size_bytes` 和 SHA-256；
对象完整性或 revision 不一致不可重试，MinIO 暂时不可用则允许消息层重试。

---

## 5.4 `judge.completed`

Producer：

```text
judge-worker
```

Consumer：

```text
judge-service
```

Payload：

```json
{
  "submission_id": 90001,
  "judge_revision": "01K5C6Y7N8P9Q0R1S2T3V4W5X6",
  "verdict": "AC",
  "time_ms": 32,
  "memory_kb": 4096,
  "case_results": [
    {"case_no": 1, "verdict": "AC", "time_ms": 12, "memory_kb": 4096}
  ]
}
```

Worker 必须成功发布结果并收到 Publisher Confirm 后，再 ACK 原 Judge Task。

---

## 5.5 `judge.failed`

表示系统级判题失败，而不是用户代码的 WA/TLE 等正常 Verdict。

Payload：

```json
{
  "submission_id": 90001,
  "judge_revision": "01K5C6Y7N8P9Q0R1S2T3V4W5X6",
  "reason": "SANDBOX_UNAVAILABLE",
  "retryable": true
}
```

`retryable=true` 时，任务进入对应语言的延迟 Retry Queue；最多重试 3 次，
耗尽后进入 DLQ。Judge Service 消费 DLQ 并把仍未终止的 Submission 收敛为
`DONE/SYSTEM_ERROR`。RabbitMQ 暂时不可用时不立即产生 SYSTEM_ERROR，而是由
Outbox、Publisher Confirm、未 ACK 重投和总 deadline 共同恢复或最终收敛。

---

## 5.6 `submission.judged`

共享 `pkg/mq.SubmissionJudged`，Envelope event_version=1，routing key=`submission.judged`。
仅 `contest_id != NULL` 的 Submission 发布；最终基础设施失败 verdict=`SYSTEM_ERROR`，不计错误次数。
Contest 队列 `contest.submission-judged` 使用 durable、manual ACK、bounded prefetch、DLQ。
成功事务提交后 ACK，协议错误 Reject(false)，存储或本地归属失败 NACK(requeue=true)。
同 ID 的修正必须提高 judged_at；陈旧/等版本消息不会覆盖最新事实。作废优先且永久生效。

Producer：

```text
judge-service
```

Potential Consumers：

```text
contest-service
notification / SSE fan-out
analytics extension
```

Payload：

```json
{
  "submission_id": 90001,
  "contest_id": 2001,
  "user_id": 1001,
  "problem_id": 1001,
  "verdict": "AC",
  "submitted_at": "RFC3339 timestamp",
  "judged_at": "RFC3339 timestamp"
}
```

---

## 5.7 `submission.invalidated`

管理员重判事务产生，供 Contest、排行榜和统计视图撤销旧结果：

```json
{
  "submission_id": 90001,
  "user_id": 1001,
  "problem_id": 1001,
  "previous_verdict": "AC",
  "contest_id": 2001,
  "submitted_at": "RFC3339 timestamp",
  "invalidated_at": "RFC3339 timestamp"
}
```

Consumer 必须按 `event_id` 幂等；旧提交一经作废，不得再接受 Worker 的迟到
结果。新 Submission 之后按正常 `submission.judged` 流程进入 Contest。
比赛事件必须包含 contest_id/submitted_at，普通作废事件仍允许缺省 contest_id=0 并由 Contest 忽略。
比赛重判的新 Submission 保留原提交时间用于 ACM 计分，保持 ADR-0001 的新 ID 语义。

---

## 6. Proto 兼容规则

必须：

- Package 版本化。
- 已使用 Field Number 不得复用。
- 删除字段使用 `reserved`。
- CI 执行 `buf lint`。
- CI 执行 `buf breaking`。
- Generated Code 不手工修改。
- 修改 Proto 时同时检查 Go Server、Go Client、Python Client 和 Agent Tool。

示例：

```protobuf
message Submission {
  int64 id = 1;
  int64 user_id = 2;
  int64 problem_id = 3;
  string language = 4;
  string status = 5;

  reserved 6;
}
```

---

## 7. Pagination

列表 API 统一使用：

```text
page
page_size
```

v0 阶段保持简单。

要求：

- `page_size` 有最大值。
- 默认排序必须稳定。
- 大规模数据后可以演进为 cursor pagination。

---

## 8. Idempotency

以下接口/消费者需要重点考虑幂等：

- Create Submission 和 Rejudge Submission（必填 `idempotency_key`）。
- Outbox Relay。
- Judge Result Consumer。
- Contest Event Consumer。
- Agent 中产生写操作的未来 Tool。

MQ Consumer 以 `event_id` 或业务唯一约束实现去重。

Judge Result Consumer 必须在同一个事务内完成 `processed_events` 去重、
Submission 状态与 Case Result 写入，以及 `submission.judged` Outbox 写入。
结果中的 `submission_id` 和 `judge_revision` 必须同时匹配；迟到、重复或已
作废 Submission 的结果不得覆盖终态。

---

## 9. Timeout / Retry

gRPC Client：

- 必须传播 `context.Context`。
- 必须设置合理 Deadline。
- 不对所有错误无脑重试。
- 写请求重试前必须确认幂等性。

RabbitMQ：

- Retry 有上限。
- 使用 Backoff。
- 不可恢复消息进入 DLQ。

---

## 10. API 变更检查清单

修改 API 前检查：

- [ ] 是否属于正确 Service。
- [ ] 是否泄露其他 Service 的内部数据模型。
- [ ] 是否需要新 Proto / Event Version。
- [ ] 是否影响 Go / Python 客户端。
- [ ] 是否需要权限校验。
- [ ] 是否需要 Contract Test。
- [ ] 是否更新 OpenAPI / Proto Docs。

### 本人 Agent 会话（Issue #167）

`GET /api/v1/agent/conversations?page=1`：登录用户的历史会话，固定每页 20 条，
`page` 为 1–10000，按 `updated_at DESC, id DESC` 排序。
返回 `{items:[{id,agent_key,title,updated_at}],page:{page,page_size,total}}`。
新会话标题取首条消息前 80 字；旧空标题读取首条用户消息摘要。

`GET /api/v1/agent/conversations/{canonical_uuid}/messages?before={message_id}`：
只读取本人会话，每次返回最多四条完整 user/assistant 消息，按 ID 升序；
`before` 为正整数，省略时读取最新消息。返回
`{conversation:{id,agent_key,title},items:[{id,role,content,status,error_code}],next_before}`。
`next_before` 非空时可继续向前分页。失败、停止的用户消息保留稳定 Run 状态/错误码，
不返回未完成助手文本、工具原文、模型摘要或配置快照。未找到与越权均返回
`404 AGENT_CONVERSATION_NOT_FOUND`，管理员也不能读取其他用户会话。
重复/未知查询参数和 GET 请求体返回 400。响应 `Cache-Control: no-store`。

两条只读接口均经 Gateway 用户鉴权、具体路径委托与 Agent owner 校验，无需管理员角色。
恢复会话后聊天仍使用原 Agent；选择其他 Agent 会开始新对话。Chat POST 不自动重放。

### 自动 Skill 参考（Issue #169）

Run 快照新增 `skill_selection`（automatic/explicit）、`skill_references`，仅 Agent 自有库保存；
不增加 REST/SSE 字段，不向用户暴露技能正文或原始配置。自动模式的工具范围为可用 react
技能工具并集 ∩ Agent 白名单 ∩ 系统启用的只读工具；最终业务权限仍由 Go 服务检查。
预算保守取 Agent、部署上限与参与参考技能的兼容旧预算最小值。一次模型请求同时参考技能，
没有独立分类请求；超过输入预算明确失败，不静默丢弃说明。模型采用哪个说明是回答行为，
当前不生成可靠的 selected_skill 审计字段。


## Agent RAG 管理接口与分组绑定

前缀 `/api/v1/admin/agent/rag`。Gateway 和 Agent 均只允许 `admin`、`system_admin`、`agent_admin`；
Gateway 签署绑定具体方法/路径的短期委托。普通用户没有知识列表/原文/下载接口，RAG API 不属于聊天 SSE。
`AGENT_ADMIN_ENABLED` 与 `AGENT_RAG_ENABLED` 均须开启。PR2 允许 `knowledge_scope` 绑定 0–20 个不重复且已存在的知识分组 UUID；空数组表示不访问任何知识库。
分组复用 knowledge_base，每份文档属于一个分组，Agent 不逐篇绑定；分组改名不改变 UUID。
PR3 再接入模型工具和 Run 固定版本清单，不能把管理员测试搜索开放给普通用户。

| 方法与相对路径 | 请求 | 响应 |
| --- | --- | --- |
| GET `/knowledge-bases` | page/page_size | items + page |
| POST `/knowledge-bases` | name | knowledge_base |
| GET `/knowledge-bases/{uuid}` | 无查询参数 | knowledge_base |
| POST `/knowledge-bases/{uuid}/rename` | name、expected_name | knowledge_base；名称不符返回 409 |
| GET `/documents` | page/page_size，可选 base_id UUID | items + page |
| POST `/documents` | 下述 Upload | document（初始 PENDING） |
| GET `/documents/{uuid}` | 无查询参数 | document 含当前版本 content、索引状态及 active_version |
| GET `/documents/{uuid}/versions` | page/page_size，不接受 base_id | items + page；不可变版本元数据、索引状态，含 active_version，不含正文/对象键 |
| POST `/preview` | content、page（默认 1） | chunks + page；每页 20 个片段，含 number/text/section/start/end，不保存文档 |
| POST `/documents/{uuid}/enable` | expected_version UUID | result：document_id/version_id/action |
| POST `/documents/{uuid}/archive` | 同上 | 同上，立即排除后续检索 |
| POST `/documents/{uuid}/retry` | 同上，仅 FAILED 允许 | 同上，重新排队 |
| POST `/search` | base_ids（1–20 个 UUID）、query（1–400 字符）、top_k（1–10，默认 5）、可选 problem_id | matches |

上述 POST（包括 preview/search）须携带规范 UUID `X-Request-ID`。上传为受限 JSON 文本（兼容现有 Gateway JSON 代理），
并非任意文件路径/下载 URL：`base_id,title,filename,content,kind(editorial/lecture/platform),problem_id?,source_url?,tags?`。
替换需同时提供 `document_id` 和 `expected_version`，不符返回 409。仅 UTF-8 Markdown/TXT，正文非空、
无 NUL、最多 120000 字节；整个 JSON 上限 262144 字节。名称/标题最多 128 字符，标签最多 20 个、每个最多 64 字符。
索引完成后 active_version 切换但新文档仍需管理员 enable；已有启用文档更新后自动使用完整就绪新版本。
启停要求 current_version 一致，归档不物理删除文档。列表 page≥1、page_size≤50，有稳定顺序。

文档返回 id（版本 UUID）、document_id、base_id、enabled、active_version、标题/类型/题号/来源/标签/文件名、
hash/大小、模型 revision、splitter 版本、创建者/时间、state/chunk_count/error_code；仅详情额外含 content，
不返回 MinIO object key 或凭据。检索测试仅管理员可用，匹配项含 score、内部文档/版本/知识库 UUID、标题、
problem_id、number、text、section、start/end（原文 Unicode 字符偏移，非字节）。最多 2000 个启用版本，超出返回 AGENT_UNAVAILABLE，不部分检索或回退全库；
Qdrant 前置过滤版本，返回后重新检查 MySQL 发布/启用状态。PR2 仍不对普通模型开放此管理员测试接口。

错误沿用单字段 `{code}`：400 AGENT_INVALID_ARGUMENT；403 AGENT_FORBIDDEN；404 AGENT_RAG_NOT_FOUND；
409 AGENT_RAG_STALE / AGENT_RAG_NOT_READY / AGENT_RAG_REQUEST_CONFLICT；413/415 沿用请求错误；
503 AGENT_RAG_DEPENDENCY_UNAVAILABLE / AGENT_UNAVAILABLE / AGENT_MANAGEMENT_DISABLED。依赖错误脱敏，
不返回第三方原始正文。本阶段扩展管理员 REST 与 Agent knowledge_scope 校验，默认空范围兼容已有配置；Go Gateway allowlist/响应校验、Python 服务及 Web 客户端同步更新，不改变 gRPC/SSE，不新增 MQ 事件。

管理员页面位于 `/agent/rag`，保留 Agent 左侧导航，仅管理角色可见；支持分组创建/改名、文档上传与版本编辑、状态刷新/失败重试、启用/归档、切分预览和当前分组检索。正文以纯文本显示，不执行上传 Markdown/HTML。文件在浏览器按 UTF-8 校验后作为受限 JSON 上传。Agent 编辑页按组多选；普通用户直接进入管理 URL 会返回对话页，后端仍独立拒绝管理 API。

### RAG PR3：内部模型检索与管理员证据

`retrieve_knowledge` 是内部只读工具，不是普通用户可调用的 REST 搜索接口。绑定分组且开启 RAG 的真实模型 Agent 自动获得该工具，仍消耗全局工具/模型预算。输入为 `query`（1–400 字符、非空白）、可空 `problem_id`（正 int64）、`top_k`（1–5，默认 3），拒绝其他字段；分组、版本和身份均取可信 Run 状态。默认每 Run 最多 4 次查询（`AGENT_RAG_MAX_QUERIES_PER_RUN` 可设 1–8）、`AGENT_RAG_MIN_SCORE=0.35`，匹配正文及元数据总计至多 24KB 并服从全局结果上限。

内部结果包含 `matches`、`truncated`、不可信资料提示；匹配项为现有检索字段加程序生成的 `source_id`。失败沿用 ToolResult 的 `ok=false/error_code`：依赖不可用 `TOOL_DEPENDENCY_UNAVAILABLE`、超时 `TOOL_TIMEOUT`、授权拒绝 `TOOL_PERMISSION_DENIED`、查询超限 `TOOL_KNOWLEDGE_QUERY_LIMIT`；成功空匹配明确表示没有可用证据，不能把故障伪装成空匹配。结果只返回模型；普通 SSE 的字段和事件不变，不返回检索原文、证据对象、对象键或下载链接。Provider 流事件预算耗尽返回既有 `AGENT_MODEL_OUTPUT_LIMIT`，不再误报响应格式错误。

新增管理员 **GET `/api/v1/admin/agent/rag/runs/{uuid}/evidence`**，不接受查询参数/正文，只允许 canonical UUID。双层管理员鉴权、具体 GET 路径委托、Cache-Control:no-store；普通用户 403、未知 Run 404。响应：

```json
{"evidence":{"run_id":"UUID","status":"COMPLETED","agent_key":"rag_agent","scope":["组UUID"],"versions":["版本UUID"],"attempts":[{"call_id":"call_1","ok":true,"error_code":null,"sources":[{"source_id":"UUID","base_id":"UUID","document_id":"UUID","version_id":"UUID","problem_id":22,"title":"单调队列","number":0,"section":"证明","start":0,"end":120,"score":0.8}]}]}}
```

attempts 最多 32，每项来源最多 5，仅记录实际送回模型的来源位置。运行中可查询固定清单，检索记录在终态持久化后出现；不返回完整 config_snapshot/model_summary、Provider 配置、query 或片段。无检索的旧 Run 兼容返回空清单和空 attempts。RAG 页支持输入 Run UUID 或管理员当前聊天的“查看本轮知识来源”链接。

新增兼容性 gRPC：`contest.v1.ContestService/CheckKnowledgeAccess`，请求 `problem_ids` 为 1–100 个不重复正 int64，返回 `allowed_problem_ids` 子集，不返回比赛列表/身份。需可信内部主体；Contest 从自身数据库排除所有 `status != archived AND end_at > UTC_TIMESTAMP(3)` 的关联题目，管理员同样不豁免。该结果只确认比赛限制，Agent 还必须通过 Problem `BatchGetProblems` 确认题目 normal/可见；未知、归档及不可访问题目均排除。Python bindings、Go generated server/client、Agent RPC 白名单与部署配置同步更新；Gateway 不公开此 RPC。无新增事件/表/列，升级先部署业务 RPC 与白名单并重启加载配置，再部署 Agent；旧服务返回 Unimplemented 时检索失败关闭。
