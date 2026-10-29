# Metrics 指标字典

本文描述当前已实现的 Metrics 名称、单位、标签和数据语义。未登记的指标或标签不应直接加入生产代码。指标是运行诊断旁路数据，不能替代业务数据库、判题结果、Outbox 状态或 Agent Run 终态。

## 采集路径

Go 服务和 Judge Worker 使用 `pkg/observability` 的 OpenTelemetry MeterProvider；Agent 使用 `agent/app/observability.py` 的 MeterProvider 和安全属性策略。设置 `OTEL_EXPORTER_OTLP_ENDPOINT` 后，Metrics 通过 OTLP 发往 Collector；未设置时使用 no-op/无 reader 行为。Collector 在 `otel-collector:9464` 暴露 Prometheus exporter，Prometheus 每 15 秒抓取该 exporter 和 Collector 自身的 `:8888` 指标。Contest 还在 `contest-service:9105/metrics` 提供独立的 Prometheus exposition。

Collector 的 Resource 转换提供 `service_name` 和 `deployment_environment` 标签。Contest 手写 exposition 保持 `oj_contest_*` 名称。OTel 名称使用小写点分隔，Prometheus exporter 负责生成 `_total`、`_seconds_bucket` 等后缀，例如 `oj.judge.submission.total` 导出为 `oj_judge_submission_total`。

指标记录、属性规范化、导出失败、Collector/Prometheus 不可用都不能改变业务请求、数据库事务、RabbitMQ ACK/NACK、重试、租约或 Agent Run 终态。

## 指标目录

| 领域 | 指标 | 类型 | 单位 | 标签 |
| --- | --- | --- | --- | --- |
| HTTP/gRPC | `http.server.request.duration`、`rpc.server.duration`、`rpc.client.duration` | Histogram | `s` | 服务端或客户端 instrumentation 的稳定路由/RPC、状态 |
| Judge | `oj.judge.submission.total`、`oj.judge.result.total` | Counter | `1` | `language`、`outcome`、`verdict` |
| Judge | `oj.judge.result.publish.failure.total` | Counter | `1` | `outcome` |
| Judge | `oj.judge.queue.duration`、`oj.judge.compile.duration`、`oj.judge.execute.duration` | Histogram | `s` | `outcome`、`language`、`verdict` |
| Judge Outbox | `oj.judge.outbox.publish.total`、`oj.judge.outbox.retry.total`、`oj.judge.dlq.total` | Counter | `1` | `outcome` 或 `error_code` |
| Judge Worker | `oj.judge.worker.task.total`、`oj.judge.worker.retry.total` | Counter | `1` | `language`、`outcome` |
| Judge Worker | `oj.judge.worker.in_flight` | UpDownCounter | `1` | 无业务 ID 标签 |
| Judge Worker | `oj.judge.worker.task.duration`、`oj.judge.worker.sandbox.duration` | Histogram | `s` | `language`、`outcome` 或 `phase` |
| Judge Result | `oj.judge.result.consume.total` | Counter | `1` | `outcome` |
| Contest | `oj.contest.projection.total`、`oj.contest.projection.processed_event.total` | Counter | `1` | 投影 `outcome`（手写 exposition） |
| Contest | `oj.contest.projection.duration` | Histogram | `s` | 投影 `outcome` |
| Contest Cache | `oj.contest.cache.hit.total`、`miss.total`、`sql_fallback.total`、`rebuild.total`、`rebuild_failure.total` | Counter | `1` | 无动态业务标签 |
| Contest Cache Outbox | `oj.contest.cache.outbox.apply.total`、`retry.total`、`lease_failure.total`、`failure.total`、`observation_failure.total` | Counter | `1` | 无动态业务标签 |
| Contest Cache Outbox | `oj.contest.cache.outbox.backlog`、`pending`、`oldest_age_seconds`、`dead`、`observed_at_seconds` | Gauge | `1` 或 `s` | 无动态业务标签 |
| Contest Cache | `oj.contest.cache.outbox.delivery_age_seconds_total`、`rebuild_duration_seconds_total` | Counter（累计时长） | `s` | 无动态业务标签 |
| Agent Run | `agent.run.started`、`agent.run.terminal` | Counter | `1` | Run 终态仅使用受控 `status` |
| Agent Run | `agent.run.in_flight` | UpDownCounter | `1` | 无业务 ID标签 |
| Agent Run | `agent.run.duration`、`agent.run.first_event.duration` | Histogram | `s` | `status`（Run duration） |
| Agent Model | `agent.model.attempt`、`agent.model.retry` | Counter | `1` | `purpose`、受控 `model`、`outcome` |
| Agent Model | `agent.model.in_flight` | UpDownCounter | `1` | `purpose`、`model` |
| Agent Model | `agent.model.duration` | Histogram | `s` | `purpose`、`model`、`outcome` |
| Agent Tool | `agent.tool.total` | Counter | `1` | `purpose`、受控 `tool`、`outcome` |
| Agent Tool | `agent.tool.in_flight` | UpDownCounter | `1` | `purpose`、`tool` |
| Agent Tool | `agent.tool.duration` | Histogram | `s` | `purpose`、`tool`、`outcome` |
| Agent RAG | `agent.rag.total` | Counter | `1` | `purpose`、`outcome` |
| Agent RAG | `agent.rag.in_flight` | UpDownCounter | `1` | `purpose` |
| Agent RAG | `agent.rag.duration` | Histogram | `s` | `purpose`、`outcome` |
| Agent 后台任务 | `agent.job.total`、`agent.job.in_flight` | Counter / UpDownCounter | `1` | `purpose`、`status` |
| Agent 后台任务 | `agent.job.duration` | Histogram | `s` | `purpose`、`status` |
| 遥测自身 | `oj.telemetry.export.failure`、`oj.telemetry.dropped` | Counter | `1` | 受控 `error_code` |

Prometheus 规则和 Grafana 看板主要使用这些序列计算请求速率、错误速率、延迟分位数、判题排队、Agent 成功率/TTFT、后台任务失败和采集器健康。跨实例 P95/P99 必须从合并后的 histogram bucket 计算，不能平均各实例分位数。

## Resource 与标签策略

每个服务设置以下 Resource：

- `service.name`：`gateway-service`、`judge-service`、`problem-service`、`judge-worker`、`contest-service`、`agent-service` 等固定服务名。
- `deployment.environment`：由 `OTEL_DEPLOYMENT_ENVIRONMENT` 或 Agent 环境配置提供，常用值为 `development`、`test`、`staging`、`production`。

业务属性使用显式 allowlist。允许的 key 为：

```text
service environment route rpc status outcome verdict language purpose
model tool error_code usage_source phase
```

受控值包括固定服务名、路由模板、RPC 名、Run/任务状态、判题 verdict、编程语言、Agent purpose、模型别名、Tool 名、错误码、Token 来源和判题 phase。未知值归一为 `other`，未知 key 丢弃；值长度、格式和 UUID/ULID/数字业务 ID 也经过限制。

以下值禁止作为 Metrics label：

```text
user_id run_id trace_id span_id conversation_id submission_id
problem_id contest_id request_id idempotency_key source_code prompt
query payload input output URL token credential exception_message
```

Metrics 不记录源码、对话、Prompt、模型输出、Tool 参数或结果、RAG 文本、评测样本、凭据和原始异常。需要排障的关联 ID 使用 Trace 属性、安全 Run 投影或受控日志字段。

## 计数语义

- Counter 只在业务事实或 Outbox 状态已经持久化后增加；事务回滚、未确认发布和未 ACK 的任务不能记为成功。
- Judge `result.total` 表示结果发布已收到 Publisher Confirm；Confirm 失败进入独立 failure counter。Worker 的 DLQ counter 表示已提交 `Reject(false)`，不等同于 Broker 持久化确认。
- Worker `in_flight` 覆盖任务开始到处理、结果发布和 ACK 路径退出；进程重启后归零。队列等待从消息事件时间到 Worker 开始时间计算。
- Contest 投影成功包括已提交的幂等重复和旧版本保护结果。Cache Outbox Gauge 是定期快照；查询失败保留旧值并更新时间语义，不写入虚假的零。
- Agent 模型、Tool 和 RAG 指标在一次操作退出后记录 `success`、`error`、`timeout` 或 `cancelled`。重试只有在下一次 attempt 或后台重试已经安排时计数。
- Agent 后台任务使用 `completed`、`failed`、`retrying`、`cancelled`、`lease_lost`；失租表示任务完成状态无法通过租约 fencing 持久化。
- UpDownCounter 表示当前进程观察到的在途数量，不代表历史累计量。进程重启会归零。
- 指标采集不依赖 Trace 是否采样；Trace 缺失不能造成业务 Metrics 缺失。

## 查询和可用性

Gateway 只允许 `15m`、`1h`、`24h` 三个时间窗，使用服务端登记的 PromQL 模板，限制序列、点数、响应大小和查询超时。系统管理员可查看系统/Judge/Contest/Agent/Collector 序列；`agent_admin` 只能查看 Agent 和 Collector 序列。空序列返回 `no_data`，目标抓取异常返回 `stale`，未配置 Prometheus 返回 `not_configured`，上游请求失败返回 `unavailable`。

Prometheus 当前保留 7 天；Contest、Go/Agent OTel Metrics 和 Prometheus 自身指标的进程重启可能造成短暂采集缺口。Prometheus 历史数据不作为业务事实回填，容量由部署卷和主机资源另行约束。

相关配置和页面见 [Observability 系统设计](design.md)；观测 API 契约见 [`docs/api.md`](../api.md)。
