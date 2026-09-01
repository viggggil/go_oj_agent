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

本阶段只建立可编译、可测试的服务骨架：

- `cmd/user-service`：服务入口占位。
- `internal/conf`：认证相关配置结构。
- `internal/biz`：领域模型、错误、校验逻辑和 usecase 依赖接口。
- `internal/data`：后续 MySQL / Redis 数据实现的占位接口。
- `internal/service`：后续 gRPC handler 的方法映射占位。

## 后续实现顺序

1. 接入 MySQL，实现用户、角色和用户角色关系持久化。
2. 接入 JWT，实现 Access Token 签发与校验。
3. 接入 Redis，实现 Refresh Token 存储、轮换和撤销。
4. 生成 protobuf Go 代码后，将 `internal/service` 绑定到真实 gRPC handler。

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
- Refresh Token 后续实现轮换。
- 密码使用 bcrypt 哈希，默认 cost 为 12。
- 密码策略保持简单：trim 后不能为空，最小 8 个字符，最大 72 bytes。
- username 和 email 都按大小写不敏感处理，注册前执行 trim + lowercase。
