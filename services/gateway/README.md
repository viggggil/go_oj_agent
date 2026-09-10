# Gateway Service

`gateway-service` 是外部 HTTP API 入口，负责 REST/SSE 接入、认证入口、请求上下文构造和到内部 gRPC 服务的转发。

## 当前状态

当前阶段已完成 Gateway Kratos 项目骨架，并接入 user-service 的认证和用户 HTTP API：

- `cmd/server`：服务入口和 Wire 注入。
- `internal/conf`：Gateway 配置契约。
- `internal/server`：HTTP Server、Proto 生成 API 注册和 Consul Registrar。
- `internal/middleware`：Bearer Token 校验、认证 claims 和请求上下文构造。
- `internal/client`：User gRPC client 已接入，Problem、Submission 先保留扩展点。
- `internal/service`：GatewayService 实现由 Proto 生成的 HTTP 接口，Auth、当前用户和按 ID 查询转发到 user-service，Problem、Submission 先保留扩展点。
- `configs/config.yaml`：本地默认配置。
- `/healthz`：健康检查接口。
- `POST /api/v1/auth/register`：转发到 `user.v1.UserService/Register`。
- `POST /api/v1/auth/login`：转发到 `user.v1.UserService/Login`。
- `POST /api/v1/auth/refresh`：转发到 `user.v1.UserService/RefreshToken`。
- `GET /api/v1/users/me`：需要 Bearer Token，转发到 `user.v1.UserService/GetCurrentUser`。
- `GET /api/v1/users/{id}`：需要 Bearer Token，转发到 `user.v1.UserService/GetUser`。

Problem 和 Submission 目录先保留扩展位置，不在当前阶段实现真实转发。

## 目录结构

```text
services/gateway/
├── cmd/server/
├── configs/
└── internal/
    ├── conf/
    ├── server/
    ├── service/
    ├── client/
    └── middleware/
```

## 启动

```bash
go run ./services/gateway/cmd/server -conf services/gateway/configs/config.yaml
```

健康检查：

```bash
curl http://127.0.0.1:8080/healthz
```

HTTP API 由 `api/gateway/v1/gateway.proto` 中的 `service GatewayService` 和 `google.api.http` 定义，通过 `protoc-gen-go-http` 生成 `gateway_http.pb.go`，并使用 `RegisterGatewayServiceHTTPServer` 注册到 Kratos HTTP Server。请求参数校验使用同一 Proto 生成的 `Validate()`。

```bash
curl -X POST http://127.0.0.1:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"account":"alice@example.com","password":"correct1"}'
```

受保护用户接口需要 Access Token：

```bash
curl http://127.0.0.1:8080/api/v1/users/me \
  -H "Authorization: Bearer ${ACCESS_TOKEN}"

curl http://127.0.0.1:8080/api/v1/users/1001 \
  -H "Authorization: Bearer ${ACCESS_TOKEN}"
```

成功响应使用生成的 Proto response 直接编码；请求 ID 通过 `X-Request-ID` 响应头返回。错误由 Kratos 标准 error encoder 编码：

```json
{
  "code": 400,
  "reason": "GATEWAY_INVALID_ARGUMENT",
  "message": "..."
}
```

## 边界

- Gateway 不直接访问业务数据库。
- Gateway 不校验密码、不签发 Refresh Token、不做用户资源最终授权。
- Gateway 公开认证接口只做 HTTP DTO 校验、请求转换和 gRPC 转发。
- Gateway 使用公共包 `pkg/auth` 验证 Access Token，将 claims 转换为 `common.v1.RequestContext`，并传递给 user-service。
- Gateway 使用 Kratos `middleware.Middleware` 和 `server.Use` 按 operation 保护用户接口，不使用原生 `HandleFunc` 或 `khttp.Filter` 实现业务路由和认证。
- Gateway 不复制 user-service 的资源授权规则；用户本人或管理员权限由 user-service 最终判断。
- Gateway 到内部服务统一走 gRPC。
