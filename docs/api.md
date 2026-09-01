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

---

## 2. 通用规范

### 2.1 Version

External：

```text
/api/v1/...
```

Protobuf：

```text
user.v1
problem.v1
submission.v1
contest.v1
judge.v1
```

### 2.2 Content Type

普通 HTTP：

```text
application/json
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

成功：

```json
{
  "data": {},
  "request_id": "..."
}
```

失败：

```json
{
  "code": "NOT_FOUND",
  "message": "resource not found",
  "request_id": "..."
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

---

## 3. External REST API

## 3.1 Auth

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
  "data": {
    "user_id": 1001,
    "username": "alice"
  },
  "request_id": "..."
}
```

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
  "data": {
    "access_token": "...",
    "refresh_token": "...",
    "expires_in": 3600
  },
  "request_id": "..."
}
```

### POST `/api/v1/auth/refresh`

Request：

```json
{
  "refresh_token": "..."
}
```

Response：新的 Access Token；是否轮换 Refresh Token 由实现策略决定。

---

## 3.2 User

### GET `/api/v1/users/me`

获取当前用户信息。

Response：

```json
{
  "data": {
    "id": 1001,
    "username": "alice",
    "email": "alice@example.com",
    "roles": ["user"]
  },
  "request_id": "..."
}
```

---

## 3.3 Problem

### GET `/api/v1/problems`

Query：

```text
page
page_size
keyword
difficulty
tag
```

Response：

```json
{
  "data": {
    "items": [],
    "page": 1,
    "page_size": 20,
    "total": 0
  },
  "request_id": "..."
}
```

### GET `/api/v1/problems/{problem_id}`

返回题面、限制、标签等公开信息。

普通用户接口不返回隐藏测试数据正文。

### POST `/api/v1/problems`

管理员创建题目。

### PATCH `/api/v1/problems/{problem_id}`

管理员更新题目。

---

## 3.4 Submission

### POST `/api/v1/submissions`

创建提交。

Request：

```json
{
  "problem_id": 1001,
  "language": "cpp",
  "source_code": "#include <bits/stdc++.h>..."
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

### GET `/api/v1/submissions/{submission_id}`

返回提交元数据与当前判题结果。

只有提交拥有者、授权角色或业务规则允许的用户可以读取私有源码。

### GET `/api/v1/submissions`

Query：

```text
page
page_size
problem_id
status
language
```

默认只查询当前用户有权访问的提交。

### GET `/api/v1/submissions/{submission_id}/events`

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

## 3.5 Contest（后续阶段）

### GET `/api/v1/contests`

比赛列表。

### GET `/api/v1/contests/{contest_id}`

比赛详情。

### POST `/api/v1/contests`

管理员创建比赛。

### POST `/api/v1/contests/{contest_id}/join`

参加比赛。

### GET `/api/v1/contests/{contest_id}/leaderboard`

获取排行榜。

实时视图可来自 Redis，最终结果以持久化数据为准。

---

## 3.6 Agent

### POST `/api/v1/agent/chat`

发起 Agent 会话，响应使用 SSE。

Request：

```json
{
  "conversation_id": "optional",
  "message": "为什么我的 submission 90001 一直 WA？",
  "context": {
    "submission_id": 90001
  }
}
```

SSE：

```text
event: tool_call
data: {"tool":"get_submission","arguments":{"submission_id":90001}}

event: tool_result
data: {"tool":"get_submission","ok":true}

event: token
data: {"text":"从你的判题结果来看..."}

event: done
data: {"conversation_id":"..."}
```

Agent API 不接受“绕过授权”的任意资源读取参数。

---

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
  rpc GetCurrentUser(GetCurrentUserRequest) returns (GetCurrentUserResponse);
  rpc GetUser(GetUserRequest) returns (GetUserResponse);
}
```

User Service 第一阶段需要实现以下接口：

### `Register`

创建用户账户。

- 校验用户名、邮箱和密码格式。
- 检查用户名和邮箱是否已经存在。
- 使用安全密码 Hash 保存密码，禁止保存明文密码。
- 创建用户后分配默认 `user` 角色。
- 返回新用户的基础资料，不返回 `password_hash`。

### `Login`

使用账号和密码登录。

- `account` 可以匹配用户名或邮箱。
- 校验密码 Hash。
- 返回短生命周期 Access Token。
- 返回 Refresh Token，Refresh Token 元数据优先保存到 Redis。
- 登录失败时不要泄露“用户不存在”或“密码错误”的具体差异。

### `RefreshToken`

使用 Refresh Token 获取新的 Access Token。

- 校验 Refresh Token 是否存在、未过期、未撤销。
- 必要时轮换 Refresh Token。
- 轮换时撤销旧的 Refresh Token，避免重复使用。
- 不允许通过 Refresh Token 直接改变用户身份或角色。

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
- Gateway 调用 `Register`、`Login`、`RefreshToken` 和 `GetCurrentUser` 支持外部认证流程。
- 其他服务按业务需要通过 `GetUser` 获取最少用户资料。
- Agent Tool 通过 `GetCurrentUser` 或受控的 `GetUser` 获取当前用户上下文。
- User Service 负责最终的用户资源权限校验，Gateway 和 Agent 不负责替代该校验。

---

## 4.2 ProblemService

```protobuf
syntax = "proto3";

package problem.v1;

service ProblemService {
  rpc GetProblem(GetProblemRequest) returns (GetProblemReply);
  rpc ListProblems(ListProblemsRequest) returns (ListProblemsReply);
  rpc SearchProblems(SearchProblemsRequest) returns (SearchProblemsReply);
}
```

Agent Tool 映射：

```text
get_problem      -> GetProblem
search_problems  -> SearchProblems
recommend_problem -> Search/List + Agent strategy
```

---

## 4.3 SubmissionService

```protobuf
syntax = "proto3";

package submission.v1;

service SubmissionService {
  rpc CreateSubmission(CreateSubmissionRequest) returns (CreateSubmissionReply);
  rpc GetSubmission(GetSubmissionRequest) returns (GetSubmissionReply);
  rpc ListSubmissions(ListSubmissionsRequest) returns (ListSubmissionsReply);
  rpc ListRecentSubmissions(ListRecentSubmissionsRequest)
      returns (ListRecentSubmissionsReply);
  rpc GetJudgeResult(GetJudgeResultRequest) returns (GetJudgeResultReply);
}
```

Agent Tool 映射：

```text
get_submission           -> GetSubmission
get_judge_result         -> GetJudgeResult
list_recent_submissions  -> ListRecentSubmissions
```

Authorization 必须由 Submission Service 执行，而不是 Agent 判断。

---

## 4.4 ContestService

```protobuf
syntax = "proto3";

package contest.v1;

service ContestService {
  rpc GetContest(GetContestRequest) returns (GetContestReply);
  rpc ListContests(ListContestsRequest) returns (ListContestsReply);
  rpc GetLeaderboard(GetLeaderboardRequest) returns (GetLeaderboardReply);
}
```

Contest 可以消费 `submission.judged` 更新排行榜视图，避免直接查询 Submission 数据库。

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
Submission Service
  ↓ synchronous RPC
Judge Worker
```

---

## 5. RabbitMQ Event API

## 5.1 Event Envelope

所有事件使用稳定 Envelope：

```json
{
  "event_id": "uuid",
  "event_type": "judge.requested",
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

## 5.2 `judge.requested`

Producer：

```text
submission-service / outbox-relay
```

Consumer：

```text
judge-scheduler
```

Payload：

```json
{
  "submission_id": 90001,
  "problem_id": 1001,
  "language": "cpp",
  "testcase_version": 3
}
```

---

## 5.3 `judge.task.<language>`

Producer：

```text
judge-scheduler
```

Consumer：

```text
judge-worker
```

Routing Keys：

```text
judge.task.cpp
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
  "testcase_version": 3
}
```

源码和测试数据不必全部塞入 MQ；Worker 可以按安全约束从对象存储或受控数据源获取。

---

## 5.4 `judge.completed`

Producer：

```text
judge-worker
```

Consumer：

```text
submission-service
```

Payload：

```json
{
  "submission_id": 90001,
  "verdict": "AC",
  "time_ms": 32,
  "memory_kb": 4096,
  "case_count": 15
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
  "reason": "SANDBOX_UNAVAILABLE",
  "retryable": true
}
```

---

## 5.6 `submission.judged`

Producer：

```text
submission-service
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
  "user_id": 1001,
  "problem_id": 1001,
  "verdict": "AC",
  "judged_at": "RFC3339 timestamp"
}
```

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

- Create Submission（可选 Idempotency-Key）。
- Outbox Relay。
- Judge Result Consumer。
- Contest Event Consumer。
- Agent 中产生写操作的未来 Tool。

MQ Consumer 以 `event_id` 或业务唯一约束实现去重。

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
