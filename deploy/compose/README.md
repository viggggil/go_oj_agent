# 认证链路 Docker Compose

本目录提供当前认证后端链路所需的最小本地环境：

```text
HTTP Client -> gateway-service -> gRPC -> user-service -> MySQL / Redis
```

MySQL 保存用户和角色，Redis 保存 Refresh Token 元数据。当前环境不启动 RabbitMQ、MinIO、Consul 或尚未实现的业务服务。

## 启动

在仓库根目录执行：

```bash
make infra-up
```

服务默认地址：

| 服务 | 地址 | 用途 |
| --- | --- | --- |
| Gateway | `http://127.0.0.1:8080` | 外部 REST API |
| user-service | `127.0.0.1:9001` | 内部 gRPC，仅用于本地调试 |
| MySQL | `127.0.0.1:3306` | `oj_user` 数据库 |
| Redis | `127.0.0.1:6379` | Refresh Token 元数据 |

检查状态：

```bash
docker compose -f deploy/compose/compose.yaml ps
curl http://127.0.0.1:8080/healthz
```

停止服务但保留 MySQL 和 Redis 数据：

```bash
make infra-down
```

需要重新执行初始化 migration 时，显式删除本地 Compose 数据卷：

```bash
docker compose -f deploy/compose/compose.yaml down --volumes
```

该命令会永久删除此 Compose 项目的本地 MySQL 和 Redis 数据。

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

Compose 不复制另一套数据库结构，schema 的唯一来源仍是 `migrations/`。

## 排障

查看服务日志：

```bash
docker compose -f deploy/compose/compose.yaml logs user-service gateway-service
```

若宿主机端口已被占用，可通过环境变量覆盖，例如：

```bash
GATEWAY_HTTP_PORT=18080 MYSQL_PORT=13306 make infra-up
```
