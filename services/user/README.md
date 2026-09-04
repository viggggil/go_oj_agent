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

本阶段完成 Proto 代码生成和 gRPC handler 的基础接入：

- `cmd/user-service`：服务入口、wire 注入和启动装配。
- `internal/server`：HTTP / gRPC Server 构建、middleware 应用和 service 注册的传输层入口。
- `internal/conf`：认证相关配置结构。
- `internal/biz`：领域模型、错误、校验逻辑和 usecase 依赖接口。
- `internal/data`：MySQL 用户仓储和 Redis Refresh Token 存储实现。
- `internal/service`：接收 proto request、做简单参数转换、调用 `biz.UserUsecase`、返回 proto response。

## 后续实现顺序

1. 在 `internal/server` 接入真实运行时启动和服务注册中心。
2. 补齐配置加载、数据库连接和 Redis 连接的 Wire 装配。

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
