# Judge Worker

当前目录是 Judge Worker 的可测试核心。它通过 `pkg/mq` 接收固定任务快照，从 MinIO
的 `submission-source` 加载源码，并从 `problem-data` 加载对应 `problem_id +
judge_revision` 的 immutable manifest 与测试点，最后把 Go 编译/执行委托给独立的
`go-judge` gRPC 服务。

Loader 严格校验源码 key 的语言后缀和 ULID、manifest identity、测试点对象 key、对象
size 与 SHA-256。存储读取失败是可重试系统错误；快照或完整性不匹配不可重试。
RabbitMQ consumer 使用 manual ACK、固定并发和 prefetch；结果与 retry task 都使用
Publisher Confirm，确认成功后才 ACK 原任务。Worker 采用 at-least-once 语义，任务
可能在连接故障后重复执行，结果消费者依靠 event idempotency 和 submission terminal
state 去重。

## RabbitMQ topology

默认拓扑为：

```text
judge.task.go -> judge-worker
                     | retryable
                     v
                judge.retry.go -- TTL/DLX --> judge.task.go
                     |
                malformed reject(false) -> judge.dlq
```

retry task 会递增 `attempt`，生成新的 `event_id`，保留 `trace_id`，并把原任务事件
作为 `causation_id`。达到 `worker.retry.max_retries`，或 retry delay 会超过
`judge_deadline_at` 时，Worker 发布不可重试的 `judge.failed`。

收到 SIGTERM 后先取消 RabbitMQ consumer，停止 intake，等待已接收 delivery；在
`worker.shutdown_timeout` 内完成必要的判题和已确认结果发布后再关闭 channel/connection。
超时未完成的 delivery 不会被提前 ACK，连接关闭后由 RabbitMQ 重投。go-judge 编译
产物在每条执行路径通过 defer 删除，包括运行错误、超时和非法 runner 响应。

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

## 配置和测试

`worker.concurrency` 同时控制 worker 数量和 RabbitMQ prefetch。常用配置：

```yaml
worker:
  concurrency: 4
  task_timeout: 60s
  shutdown_timeout: 30s
  retry:
    max_retries: 3
    delay: 5s
```

运行单元和组件测试：

```bash
go test ./services/judge-worker/...
```

需要 RabbitMQ、MinIO 和 go-judge 的真实环境时，使用 `services/judge-worker/integration`
和 `tests/e2e` 中的测试；未配置依赖时测试会自动跳过。
