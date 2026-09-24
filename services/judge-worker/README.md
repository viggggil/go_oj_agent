# Judge Worker

当前目录是首版 Judge Worker 的可测试核心。它通过 `pkg/mq` 接收固定任务快照，
通过 `pkg/judge` 的 immutable manifest 使用题目限制，并把 Go 编译/执行委托给独立
的 `go-judge` gRPC 服务。

本 Issue 只接入 Worker 生命周期、Go Runner、Comparator 和 go-judge adapter；正式
RabbitMQ consumer、MinIO loader、ACK/NACK 和 retry topology 在后续 Issue 实现。

## 本地 go-judge

```bash
GO_JUDGE_TEST_TOKEN=local-development-go-judge-token \
  tests/gojudge/run.sh
```

go-judge 使用 `v1.12.3`，只在内部网络监听 gRPC，启用 bearer token；它不持有 MQ、
MinIO 或 MySQL 凭据。运行代码时，Worker 使用 manifest 顶层的题目级时间/内存限制，
编译使用 Worker 固定限制，编译产物留在 go-judge file store 并在任务结束删除。

