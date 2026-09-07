# Pkg

Shared Go packages live here.

## 当前公共包

- `auth`：跨服务共享的 Access Token Claims 与 HS256 JWT 校验能力。Gateway 使用它验证外部 `Authorization: Bearer`，user-service 使用同一套 Claims 与校验语义保持签发端和验证端一致。

## 边界约定

- 公共包不能依赖任何 `services/*/internal` 包。
- 公共包只放跨服务稳定复用的基础能力，不放具体业务用例。
- 认证公共包只提供 Access Token 校验能力；Access Token 签发、Refresh Token 生命周期和密码哈希仍属于 user-service 内部安全层。
