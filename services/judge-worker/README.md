# Judge Worker

当前目录是 Judge Worker 的可测试核心。它通过 `pkg/mq` 接收固定任务快照，从 MinIO
的 `submission-source` 加载源码，并从 `problem-data` 加载对应 `problem_id +
judge_revision` 的 immutable manifest 与测试点，最后把 Go 编译/执行委托给独立的
`go-judge` gRPC 服务。

Loader 严格校验源码 key 的语言后缀和 ULID、manifest identity、测试点对象 key、对象
size 与 SHA-256。存储读取失败是可重试系统错误；快照或完整性不匹配不可重试。正式
RabbitMQ consumer、ACK/NACK 和 retry topology 在后续 Issue 实现。

MinIO 连接通过 `storage.minio` 配置；环境变量可用 Kratos 的嵌套配置名覆盖，例如
`KRATOS_STORAGE_MINIO_ENDPOINT`、`KRATOS_STORAGE_MINIO_ACCESS_KEY` 和
`KRATOS_STORAGE_MINIO_SECRET_KEY`。

## 本地 go-judge

```bash
GO_JUDGE_TEST_TOKEN=local-development-go-judge-token \
  tests/gojudge/run.sh
```

go-judge 使用 `v1.12.3`，只在内部网络监听 gRPC，启用 bearer token；它不持有 MQ、
MinIO 或 MySQL 凭据。运行代码时，Worker 使用 manifest 顶层的题目级时间/内存限制，
编译使用 Worker 固定限制，编译产物留在 go-judge file store 并在任务结束删除。
