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

Gateway 外部 HTTP 请求/响应的结构化契约记录在 `api/gateway/v1/gateway.proto`。Gateway 的 `service GatewayService` 使用 `google.api.http` 声明 REST 路由，由 `protoc-gen-go-http` 生成 `gateway_http.pb.go`，服务启动时通过 `RegisterGatewayServiceHTTPServer` 注册。当前阶段先定义认证与用户相关 HTTP DTO，Problem、Submission 等后续模块接入时继续扩展。

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

### GET `/api/v1/users/{user_id}`

获取指定用户信息。

内部对应 `user.v1.UserService/GetUser`。当请求用户不是目标用户本人时，必须携带 `admin` 角色上下文，否则返回权限拒绝。

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

当前 `CreateSubmission`、`GetSubmission` 和 `ListSubmissions` 已在
judge-service 中实现。调用身份只来自经过 RS256 校验的内部 Principal；请求体不携带
也不能覆盖用户身份。Create 会在上传 MinIO 源码前检查已完成的幂等记录，并在一个
MySQL 事务内提交 Submission、`judge.requested` Outbox 和幂等响应。Get 对非 owner
返回与不存在资源相同的 NotFound；List 对普通用户强制使用当前 `actor_id`，管理员
才可指定其他 `user_id`。列表按 `created_at DESC, id DESC` 排序，`page_size` 上限为
100。

本阶段只开放内部 gRPC；上文 `/api/v1/submissions` REST 路由仍由后续 Gateway
接入实现。

Agent Tool 映射：

```text
get_submission           -> GetSubmission
get_judge_result         -> GetJudgeResult
list_recent_submissions  -> ListSubmissions(page=1, bounded page_size)
```

Authorization 必须由 Judge Service 执行，而不是 Agent 判断。

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
  "source_size_bytes": 1234
}
```

`judge_revision` 引用 Problem Service 已完整发布的不可变测试集 revision。首版没有 priority 字段，所有任务都使用普通优先级。源码正文存 MinIO，消息只携带不可变对象 key 与 hash。

---

## 5.3 `judge.task.<language>`

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
  "source_size_bytes": 1234
}
```

Worker 使用受限的服务凭据按对象引用读取源码，并根据 `judge_revision` 读取该 revision 的 `manifest.json` 和全部测试点。同一 Submission 不得读取其他 revision，也不得把源码或测试数据正文塞入 MQ。

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
  "user_id": 1001,
  "problem_id": 1001,
  "verdict": "AC",
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
  "invalidated_at": "RFC3339 timestamp"
}
```

Consumer 必须按 `event_id` 幂等；旧提交一经作废，不得再接受 Worker 的迟到
结果。新 Submission 之后按正常 `submission.judged` 流程进入 Contest。

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
