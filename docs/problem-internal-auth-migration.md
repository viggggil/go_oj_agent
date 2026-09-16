# Problem Service 身份迁移说明

PR #76 已经让 Gateway→Problem 的 gRPC 调用携带短期 RS256 内部 JWT。PR #77 增加了签名 Principal 与旧 `RequestContext` 的一致性守卫。

下一步删除 `api/problem/v1/problem.proto` 中请求消息的 `RequestContext` 字段时，必须按以下顺序执行：

1. 为 service 层增加 `PrincipalFromContext` 适配，将 JWT claims 转换成 biz 使用的内部 Principal。
2. 将 `requireAdmin`、测试用例读取权限和题目读写权限改为只读取 Principal。
3. 从所有 Problem request message 删除 field 1，并 `reserved 1; reserved "context";`。
4. 重新生成 `problem.pb.go`、校验代码和 Gateway/Problem client 代码。
5. 更新所有 service、biz、Gateway 和集成测试，不再构造请求体身份。
6. 启用 Problem Service 的强制内部认证配置；没有有效 JWT 的直连请求必须返回 `Unauthenticated`。

在这一步完成之前，旧字段只能作为临时兼容字段，不能作为权限来源。只要请求经过内部认证中间件，JWT 中的 `actor_id/actor_roles` 与旧字段不一致就必须拒绝。

## 验收场景

- 合法 Gateway JWT + 一致旧 context：成功。
- 合法 Gateway JWT + 伪造管理员 context：`Unauthenticated`。
- 无内部 JWT 直连 Problem：`Unauthenticated`。
- 错误 audience、issuer、kid、签名或 RPC：`Unauthenticated`。
