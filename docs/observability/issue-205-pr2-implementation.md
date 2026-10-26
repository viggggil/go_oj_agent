# Issue #205 PR2：Judge 与异步链路 Metrics

## 实现范围

- 复用已有 OTel HTTP/gRPC instrumentation，暴露 Gateway HTTP、Judge/Problem gRPC 的请求延迟与状态序列。
- 新增共享 Judge metrics：新提交/重判、队列等待、编译/执行耗时、Worker 任务/在途数、Worker sandbox 耗时、Outbox publish/retry/dead、Result publish/consume。
- Worker task outcome 使用固定 `success`、`error`、`retrying`；正常 AC/WA/CE/TLE 等 verdict 与基础设施失败分别统计。
- `oj.judge.result.total` 仅表示 Publisher Confirm 成功的结果；未确认的发布尝试进入独立 failure counter，不把 confirm 不确定性算成已产生结果。
- PR2 暂不导出 `oj.judge.outbox.pending` 和 `oj.judge.outbox.oldest_age`：这两个值需要从持久化 Outbox 快照采集，并定义查询失败时的 stale/unknown 语义；后续与 Prometheus 看板及告警在 PR5 一起落地，避免把单次 relay batch 当成队列总量。
- Metrics 只在新业务事实或 Outbox 状态持久化后计数；Result 成功以 Publisher Confirm 为准，消费成功以 Judge 事务处理返回成功为准。Worker 对坏消息的 DLQ 计数仅表示客户端提交 Reject，不宣称 broker 已确认落入 DLQ。
- 所有 label 通过 Go/Python 共享策略约束，动态/未知值归一为 `other`。

## 异步可靠性边界

本 PR 不改数据库事务、Outbox claim/lease、Publisher Confirm、RabbitMQ ACK/NACK、retry queue、DLQ 路由、结果去重或提交终态保护。指标记录不参与任何业务条件判断；confirm 失败或 Outbox 状态写入失败时不记成功。

## 验证

- `go test ./...`：通过。
- `make fmt-check`、`make vet`、`make build`：通过。
- Agent 非集成测试：232 passed，55 deselected；Ruff、format check、mypy：通过。
- Web Vitest：4 个文件、11 项测试通过；生产构建与 Prettier：通过。
- `docker compose -f deploy/compose/compose.yaml -f deploy/compose/observability.yaml config --quiet`：通过。
- Async reliability review：通过。Outbox DB+事件事务、confirm 后标记 published、失败退避/dead、worker retry confirm 后 ACK、结果事务去重与 ACK 顺序均未更改；Metrics 仅在既有状态/协议动作后记录。RabbitMQ consumer Reject 无 broker confirm，DLQ 指标按客户端提交 reject 命令统计，文档已注明。

PR CI 尚待 GitHub Actions 完成。
