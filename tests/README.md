# Tests

Integration, contract, and e2e test suites live here. The judge e2e flow is in
`tests/e2e/judge_flow_test.go` and runs through RabbitMQ, judge-worker,
go-judge, MinIO, judge-service, and the submission API.

比赛 E2E 位于 `tests/e2e/contest_flow_test.go`，通过真实 Gateway -> Contest -> Judge ->
RabbitMQ -> Worker -> Contest Consumer 验证 WA -> AC、1200 秒罚时和赛后新 ID 重判。
RabbitMQ/MySQL 组件测试位于 `services/contest/internal/server/result_consumer_integration_test.go`，
覆盖重复/乱序、陈旧版本、结果修正、SYSTEM_ERROR、先到/后到作废、重启和未 ACK 重投、DLQ 与归属失败回滚。
这些测试已纳入 `make test-integration`，不提供环境变量时在 `go test ./...` 中跳过。
手动运行需要 `CONTEST_TEST_MYSQL_DSN`、`JUDGE_TEST_RABBITMQ_URL`，E2E 还需
`CONTEST_TEST_GRPC_ENDPOINT` 和已有 Gateway/Judge/MinIO/签名密钥测试配置。

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
CONTEST_TEST_GRPC_PORT
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

## Agent Service PR1/PR2/PR3 集成测试

Agent 的集成测试使用 `tests/agent/compose.yaml` 启动一套独立的 MySQL 和 Agent
Service；PR3 另外构建 Gateway（仓库根构建上下文）。数据库使用 `oj_agent` Schema 和合成凭据；
测试结束会删除本次 project 的容器、网络和数据卷，不会连接开发 Compose 环境。

```bash
make agent-test-integration
```

也可以直接运行并覆盖 `uv`、宿主机端口：

```bash
UV=/path/to/uv AGENT_TEST_MYSQL_PORT=23306 AGENT_TEST_HTTP_PORT=28000 \
  ./tests/agent/run.sh
```

测试验证真实 MySQL 连接、错误凭据返回 `unavailable`、连接池释放，以及容器的
`/healthz` 和 `/readyz` HTTP 契约。PR1 不需要模型 API Key；模型 Provider 和运行时质量
评估会在后续真实模型接入/质量工程 PR 中加入。

PR2 使用同一套 Compose 应用真实 Agent migration，增加 owner 隔离、稳定最近历史、
10 个并发请求仅接受一个 Run、数据库唯一索引、初始/完成事务回滚、终态保护、重启
中断、deadline 释放活动关联、Fake Service 会话恢复和空数据库 down/up 验证。down
测试只运行在本次临时测试库，不能对开发或生产数据库设置测试 DSN。

PR3 的 Agent 专用 Compose 另外启动实际 Gateway 二进制和 Agent Service，临时生成 RSA
密钥，验证完整的外部 Access Token -> Gateway 内部委托 JWT -> Agent 验证 -> MySQL
链路。测试覆盖两轮会话、Run 和消息持久化、owner 隔离、直接 Agent 访问/伪造 Header
拒绝和请求体上限；Go/Python 单元测试另行覆盖 SSE 帧校验、超时和客户端断连取消。
这些测试不需要真实模型 API Key。

```bash
make agent-test-integration
```

该命令会清理临时密钥、容器、网络和数据卷。完整后端集成测试 `make test-integration`
也会在独立项目中启用 `agent` profile，并通过 `AGENT_INTEGRATION_BASE_URL` 和
`AGENT_TEST_MYSQL_DSN` 运行 Go 集成用例；清理只作用于本次测试项目，不会删除开发
Compose 数据卷。
