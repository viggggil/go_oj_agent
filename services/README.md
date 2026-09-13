# Services

Go-Kratos services are organized here by business domain.

## 当前服务

- `user`：用户、认证令牌和基础角色信息，当前已完成 gRPC 运行时和核心用户 API。
- `problem`：题目和测试用例领域，当前已完成核心 Proto 契约与 Kratos gRPC 运行时骨架，业务和数据实现待按纵向功能切片补齐。
- `gateway`：外部 HTTP API 入口，当前已完成 Kratos HTTP 骨架、配置、middleware/client/service 扩展点和健康检查接口。
