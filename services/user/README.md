# User Service

`user-service` 负责用户账户、认证令牌和基础角色信息。

## 第一阶段接口

当前服务骨架面向 `api/user/v1/user.proto` 中的 5 个 RPC：

- `Register`
- `Login`
- `RefreshToken`
- `GetCurrentUser`
- `GetUser`

## 当前范围

当前阶段已完成 Proto 代码生成、gRPC handler、基础运行时和 Consul 服务注册：

- `cmd/user-service`：服务入口、wire 注入和启动装配。
- `internal/server/grpc.go`：构建 gRPC Server、应用 middleware、注册 service handler。
- `internal/server/server.go`：创建 Consul Registrar；服务注册和注销由 `kratos.App` 统一管理。
- `internal/conf`：环境变量配置加载和服务注册配置。
- `internal/biz`：领域模型、错误、校验逻辑和 usecase 依赖接口。
- `internal/data`：MySQL 用户仓储和 Redis Refresh Token 存储实现。
- `internal/service`：接收 proto request、做简单参数转换、调用 `biz.UserUsecase`、返回 proto response。
  当前已实现注册、登录、刷新令牌、查询当前用户和按 ID 查询用户，权限判断由 `biz` 统一控制。

## 运行时配置

user-service 启动时从环境变量读取配置。以下配置必须提供：

```bash
export USER_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/oj_user?parseTime=true'
export USER_ACCESS_TOKEN_KEY='replace-with-a-long-random-secret'
```

常用配置：

```text
USER_SERVICE_NAME=user-service
USER_SERVICE_GRPC_ADDR=:9001
USER_REDIS_ADDR=127.0.0.1:6379
USER_REDIS_PASSWORD=
USER_REDIS_DB=0
USER_REDIS_NAMESPACE=go_oj_agent:user
USER_ACCESS_TOKEN_TTL=15m
USER_REFRESH_TOKEN_TTL=168h
USER_CONSUL_ENABLED=true
USER_CONSUL_ADDR=127.0.0.1:8500
USER_CONSUL_SCHEME=http
USER_CONSUL_DATACENTER=
USER_CONSUL_TOKEN=
USER_CONSUL_SERVICE_ID=
```

`USER_CONSUL_ENABLED=false` 时不连接 Consul。启用时，`server.NewRegistrar` 使用 Kratos Consul contrib 注册器，并开启健康检查。`kratos.App.Run()` 在 gRPC Server 启动后执行注册，`kratos.App.Stop()` 负责注销服务。

## 后续实现顺序

1. 补齐 `GetCurrentUser` 和 `GetUser` 的认证上下文与查询业务。
2. 增加 gateway 对 user-service 的服务发现和鉴权中间件。
3. 根据部署环境补充 Consul 健康检查、服务发现和运行配置。

## 管理员 bootstrap

管理员用户通过一次性命令创建：

```bash
export USER_BOOTSTRAP_DSN='user:pass@tcp(127.0.0.1:3306)/oj_user?parseTime=true'
export USER_BOOTSTRAP_ADMIN_USERNAME='admin'
export USER_BOOTSTRAP_ADMIN_EMAIL='admin@example.com'
read -r -s USER_BOOTSTRAP_ADMIN_PASSWORD
export USER_BOOTSTRAP_ADMIN_PASSWORD
go run ./services/user/cmd/user-bootstrap
unset USER_BOOTSTRAP_ADMIN_PASSWORD
```

命令规则：

- 密码使用 bcrypt 哈希后写入数据库，不保存明文。
- 如果系统中已经存在 `admin` 角色用户，命令会输出结构化日志并跳过，不覆盖已有管理员。
- 初始管理员参数只从环境变量读取。

## 角色

MVP 阶段只使用两个角色：

- `user`：普通用户。
- `admin`：管理员。

管理员用户不通过普通注册接口直接创建，后续通过 bootstrap 命令或运维流程初始化。

## 认证策略

- Access Token 使用 JWT，签名算法为 HS256。
- JWT secret 只允许 `user-service` 和 `gateway` 持有。
- Access Token TTL 默认为 15 分钟。
- JWT payload 可以携带 `roles`，但资源最终授权仍由资源所属服务执行。
- Refresh Token 使用高熵不透明字符串，服务端保存 SHA-256 hash。
- Refresh Token 成功刷新时执行轮换，旧 token 会被标记为撤销。
- 密码使用 bcrypt 哈希，默认 cost 为 12。
- 密码策略保持简单：trim 后不能为空，最小 8 个字符，最大 72 bytes。
- username 和 email 都按大小写不敏感处理，注册前执行 trim + lowercase。

## 日志

- 服务和 bootstrap 命令使用 `slog` 输出 JSON 结构化日志。
- 普通运行日志输出到 stdout。
- 错误日志输出到 stderr。
- 后续由 Alloy 统一采集日志，不在业务代码中写入本地日志文件。
