# 本地开发 Docker Compose

本目录提供当前认证后端链路所需的最小本地环境：

```text
HTTP Client -> gateway-service -> gRPC -> user-service -> MySQL / Redis
                              -> gRPC -> problem-service -> MySQL / Redis / MinIO
```

MySQL 保存用户、角色、题目、标签和测试点元数据；Redis 保存 Refresh Token
及题目详情缓存；MinIO 的 `problem-data` bucket 保存测试点正文。当前环境
启动 user-service、problem-service、judge-service、contest-service、judge-worker、go-judge、
RabbitMQ、gateway-service 和 Web。Contest 消费判题结果并维护本地 MySQL 排行榜，Consul 默认关闭。
Agent Service 是可选的 `agent` profile，不会默认启动。

Contest 消费配置位于 `configs/contest.yaml`：exchange=`oj.events`，queue=`contest.submission-judged`，
DLQ=`contest.results.dlq`，prefetch=16。RabbitMQ URL 由 `KRATOS_MESSAGING_URL` 注入，使用 Compose 的
RABBITMQ_USER/RABBITMQ_PASSWORD。MySQL 初始化挂载 Contest `000002_create_result_projection`；
已有数据卷须先显式执行该迁移，`make infra-up` 不会重新执行数据库初始化文件。

MinIO 和 `mc` 不再直接从 Quay 拉取。Compose 使用固定版本的官方 GitHub
Release 二进制构建本地镜像，并在 Docker build 阶段校验 SHA-256；这样 CI 不依赖
Quay 匿名拉取权限，也避免官方 Docker 镜像下线后导致集成环境无法启动。

## 启动

在仓库根目录执行：

```bash
make infra-up
```

服务默认地址：

| 服务 | 地址 | 用途 |
| --- | --- | --- |
| Gateway | `http://127.0.0.1:8080` | 外部 REST API |
| Web | `http://127.0.0.1:5173` | Vue 3 认证前端 |
| user-service | `127.0.0.1:9001` | 内部 gRPC，仅用于本地调试 |
| problem-service | `127.0.0.1:9002` | 内部 gRPC，仅用于本地调试 |
| MySQL | `127.0.0.1:3306` | `oj_user`、`oj_problem` 数据库 |
| Redis | `127.0.0.1:6379` | Refresh Token 和题目详情缓存 |
| MinIO | `http://127.0.0.1:9000` | 测试点对象存储 API |
| MinIO Console | `http://127.0.0.1:9003` | 本地对象存储管理界面 |

检查状态：

```bash
docker compose -f deploy/compose/compose.yaml ps
curl http://127.0.0.1:8080/healthz
```

浏览器访问 `http://127.0.0.1:5173` 可使用注册、登录和个人资料页面。Compose 构建时通过 `WEB_API_BASE_URL` 注入 Gateway 地址；如果修改了 `GATEWAY_HTTP_PORT`，应同步设置 `WEB_API_BASE_URL`，例如 `WEB_API_BASE_URL=http://127.0.0.1:18080 GATEWAY_HTTP_PORT=18080 make infra-up`。

停止服务但保留 MySQL、Redis 和 MinIO 数据：

```bash
make infra-down
```

需要重新执行初始化 migration 时，显式删除本地 Compose 数据卷：

```bash
docker compose -f deploy/compose/compose.yaml down --volumes
```

该命令会永久删除此 Compose 项目的本地 MySQL、Redis 和 MinIO 数据。

### 启动 Agent Chat（PR3）

Agent Chat 需要同时启动 Compose profile 和 Gateway 开关。开发 profile 使用显式的
`oj_agent` 数据库账户、合成 Fake runtime，不调用真实模型：

```bash
AGENT_ENABLED=true docker compose \
  --profile agent \
  -f deploy/compose/compose.yaml \
  up --build --detach --wait
```

也可以执行 `AGENT_ENABLED=true COMPOSE_PROFILES=agent make infra-up`，同时启用网关和
可选服务。Agent 只在 Compose 网络内监听 `8000`，
不映射宿主机端口；外部客户端始终通过 Gateway `8080` 访问 `POST /api/v1/agent/chat`。
Agent 容器只挂载 `agent-auth-public` 中的 Gateway 公钥，以及单独复制出的
`agent-auth-private` Agent 私钥卷；Gateway 私钥留在 `auth-keys`，不会进入 Agent 容器。
PR4 的业务 Tool 默认关闭。设置 `AGENT_BUSINESS_TOOLS_ENABLED=true` 后，Agent 使用该
独立私钥调用 Problem/Judge 的只读 gRPC RPC；目标服务通过 `agent-public.pem` 和 RPC
allowlist 验证调用。Agent 不会直接挂载业务数据库。

首次使用空 MySQL 数据卷时，Compose 会执行 Agent runtime migration 和本地开发账户
bootstrap。已有数据卷不会重新执行 SQL；部署者应先应用
`migrations/agent/000001_create_agent_runtime.up.sql`，并按需执行
`deploy/compose/agent-user.sql` 创建仅有 `oj_agent` 表读写权限的账户。不要为了应用迁移
删除已有开发数据卷。`AGENT_DATABASE_URL` 可覆盖默认 DSN，但必须仍使用
`mysql+asyncmy` 和 `oj_agent` Schema。

PR3/PR4 的 Fake runtime 只用于开发和集成测试；生产环境禁止 `AGENT_RUNTIME_MODE=fake`
或 `langgraph_fake`。PR4 的只读 Tools 不需要真实模型 API Key；真实模型 Provider、
知识库和管理员控制面仍未交付。停止可选服务时使用：

```bash
make infra-down
# 或显式：
docker compose --profile agent -f deploy/compose/compose.yaml down
```

## 配置

Compose 会自动读取仓库根目录或 `--env-file` 指定的环境文件。可用变量记录在 `deploy/compose/.env.example`。所有默认密码和 JWT 密钥仅供本机开发，不能用于共享、测试平台或生产环境。

`deploy/compose/configs/` 中的配置通过 Kratos 配置源使用 `${KEY}` 占位符读取容器环境变量。它们是 Compose 专用配置，不替代服务自身的本地默认配置。

使用指定环境文件：

```bash
docker compose \
  --env-file deploy/compose/.env.example \
  -f deploy/compose/compose.yaml \
  up --build --detach
```

`ACCESS_TOKEN_TTL=5s` 可用于后续真实过期集成测试。Gateway 和 user-service 必须使用相同的 `AUTH_ACCESS_TOKEN_KEY`、issuer 与 audience。

## 数据初始化

MySQL 官方镜像仅在数据目录为空时执行 `/docker-entrypoint-initdb.d`。Compose 按以下顺序挂载现有 migration：

1. 创建项目 schemas。
2. 创建 `oj_user.users`。
3. 创建 `oj_user.roles`。
4. 创建 `oj_user.user_roles`。
5. 写入 `user` 和 `admin` 默认角色。
6. 创建 `oj_problem.problems`、`tags`、`problem_tags` 和 `testcases`。

`minio-init` 在 MinIO 健康后幂等创建 `problem-data` bucket。

Compose 不复制另一套数据库结构，schema 的唯一来源仍是 `migrations/`。

Agent PR2 将 `oj_agent` Schema 和 `migrations/agent/000001_create_agent_runtime.up.sql`
加入 MySQL 空数据卷初始化，建立会话、消息和 Run 表。已有数据卷不会重新执行初始化，
需由部署者显式应用 Agent up SQL。PR3 已提供可选 Agent profile、独立公钥卷和 Gateway Chat 路由。不要为迁移已有数据库而删除当前开发数据卷。

## 排障

查看服务日志：

```bash
docker compose -f deploy/compose/compose.yaml logs user-service gateway-service
```

若 MinIO 镜像构建失败，请检查 runner 是否能访问 `github.com` 和公开的
`alpine:3.23` 基础镜像；构建阶段会下载并校验固定版本的 MinIO server 与 `mc`。

若宿主机端口已被占用，可通过环境变量覆盖，例如：

```bash
GATEWAY_HTTP_PORT=18080 MYSQL_PORT=13306 make infra-up
```
