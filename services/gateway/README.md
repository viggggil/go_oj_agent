# Gateway Service

`gateway-service` 是外部 HTTP API 入口，负责 REST/SSE 接入、认证入口、请求上下文构造、统一响应和到内部 gRPC 服务的路由。

## 当前状态

当前阶段只完成 Gateway Kratos 项目骨架：

- `cmd/server`：服务入口和 Wire 注入。
- `internal/conf`：Gateway 配置契约。
- `internal/server`：HTTP Server 和 Consul Registrar。
- `internal/middleware`：认证与请求上下文 middleware 的扩展点。
- `internal/client`：User、Problem、Submission gRPC client 扩展点。
- `internal/service`：Auth、User、Problem、Submission HTTP service 扩展点。
- `configs/config.yaml`：本地默认配置。
- `/healthz`：健康检查接口。

后续 PR 会优先接入现有 `user-service` 的注册、登录、刷新令牌、当前用户和按 ID 查询用户接口。Problem 和 Submission 目录先保留扩展位置，不在当前阶段实现真实转发。

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

响应格式：

```json
{
  "data": {
    "status": "ok"
  },
  "request_id": "..."
}
```

## 边界

- Gateway 不直接访问业务数据库。
- Gateway 不校验密码、不签发 Refresh Token、不做用户资源最终授权。
- Gateway 后续通过 `pkg/auth` 验证 Access Token，并将可信身份转换为 `common.v1.RequestContext`。
- Gateway 到内部服务统一走 gRPC。
