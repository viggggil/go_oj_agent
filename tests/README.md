# Tests

Integration, contract, and e2e test suites live here. The judge e2e flow is in
`tests/e2e/judge_flow_test.go` and runs through RabbitMQ, judge-worker,
go-judge, MinIO, judge-service, and the submission API.

## 认证集成测试

集成测试通过独立 Docker Compose project 启动 MySQL、Redis、MinIO、user-service、problem-service 和 gateway-service：

```bash
make test-integration
```

测试使用单独的数据卷和 `18080/19001/13306/16379` 宿主机端口，结束时自动删除本次测试的容器、网络与数据卷，不影响 `make infra-up` 启动的开发环境。可通过以下变量覆盖端口：

```text
AUTH_TEST_GATEWAY_HTTP_PORT
AUTH_TEST_USER_GRPC_PORT
AUTH_TEST_MYSQL_PORT
AUTH_TEST_REDIS_PORT
PROBLEM_TEST_GRPC_PORT
PROBLEM_TEST_MINIO_PORT
PROBLEM_TEST_MINIO_CONSOLE_PORT
```

覆盖范围：

- 健康检查、注册和重复注册。
- 错误密码登录。
- 缺失或无效 Access Token。
- 登录和获取当前用户。
- Access Token 真实过期。
- Refresh Token 轮换、新 Access Token 访问和旧 Refresh Token 拒绝。
- Problem Repository 对真实 MySQL 的创建、读取、更新和归档。
- MinIO 对象上传、读取元信息和清理。
- Redis Problem 详情缓存命中与失效。
- Gateway 创建题目、multipart 测试点上传、查询和归档链路。

直接运行 `go test ./...` 时，如果没有设置 `AUTH_INTEGRATION_BASE_URL`，该测试会跳过，以保持单元测试不依赖 Docker。
