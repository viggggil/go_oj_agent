# Metrics 契约（Issue #205）

本文是业务 Metrics 的命名、单位和标签字典。PR1 落地共享 Meter、属性 allowlist 和遥测自身的健康计数；PR2 接入 Judge 和异步判题链路指标；PR3 接入 Contest 投影与缓存指标；PR4 对齐 Agent Chat、模型、Tool/RAG 和后台任务指标。未在本文登记的指标和标签不得直接加入生产代码。

## 采集路径

Go 服务和 Worker 使用 `pkg/observability` 的 OTel MeterProvider，Agent 使用 `agent/app/metrics.py` 的属性策略。配置 `OTEL_EXPORTER_OTLP_ENDPOINT` 时，OTLP Metrics 发往 Collector；未配置时使用 SDK no-op/无 reader 行为。Collector 通过 Prometheus exporter 暴露指标，Prometheus 负责抓取和保存历史序列。Contest 另保留独立 `/metrics` 端口，由 Prometheus 直接抓取。

Metrics 是旁路数据：Meter 创建、属性规范化、导出失败和 Collector/Prometheus 不可用都不能改变业务请求、事务、Outbox、ACK/NACK、重试、租约或 Agent Run 终态。

## 命名、类型和单位

OTel instrument name 使用小写点分隔，不在代码中手写 Prometheus `_total` 后缀。Prometheus exporter 会按 Counter、Histogram 和 UpDownCounter 规则生成最终 exposition 名称。

| 领域 | OTel 名称 | 类型 | 单位 | 计划接入 |
| --- | --- | --- | --- | --- |
| HTTP/gRPC | `http.server.request.duration` | Histogram | `s` | 已有 OTel HTTP instrumentation |
| HTTP/gRPC | `rpc.server.duration`、`rpc.client.duration` | Histogram | `s` | 已有 OTel gRPC instrumentation |
| Judge | `oj.judge.submission.total`、`oj.judge.result.total` | Counter | `1` | PR2 |
| Judge | `oj.judge.result.publish.failure.total` | Counter | `1` | PR2 |
| Judge | `oj.judge.result.consume.total` | Counter | `1` | PR2 |
| Judge | `oj.judge.queue.duration`、`oj.judge.compile.duration`、`oj.judge.execute.duration` | Histogram | `s` | PR2 |
| Outbox/MQ | `oj.judge.outbox.publish.total`、`oj.judge.outbox.retry.total`、`oj.judge.dlq.total` | Counter | `1` | PR2 |
| Worker | `oj.judge.worker.task.total`、`oj.judge.worker.in_flight`、`oj.judge.worker.retry.total` | Counter / UpDownCounter | `1` | PR2 |
| Worker | `oj.judge.worker.task.duration`、`oj.judge.worker.sandbox.duration` | Histogram | `s` | PR2 |
| Contest | `oj.contest.projection.total` | Counter (`outcome=success/error`) | `1` | PR3 |
| Contest | `oj.contest.projection.processed_event.total` | Counter | `1` | PR3 |
| Contest | `oj.contest.projection.duration` | Histogram (`outcome=success/error`) | `s` | PR3 |
| Contest | `oj.contest.cache.hit.total`、`oj.contest.cache.miss.total`、`oj.contest.cache.sql_fallback.total` | Counter | `1` | PR3 |
| Contest | `oj.contest.cache.outbox.pending`、`oj.contest.cache.outbox.backlog`、`oj.contest.cache.outbox.oldest_age`、`oj.contest.cache.outbox.dead` | Gauge | `1` / `s` | PR3 |
| Contest | `oj.contest.cache.outbox.apply.total`、`oj.contest.cache.outbox.retry.total`、`oj.contest.cache.outbox.lease_failure.total` | Counter | `1` | PR3 |
| Contest | `oj.contest.cache.outbox.delivery_age`、`oj.contest.cache.rebuild.duration` | Counter (duration sum) | `s` | PR3 |
| Agent Run | `agent.run.started`、`agent.run.in_flight` | Counter / UpDownCounter（无业务 label） | `1` | PR4（Run 既有指标） |
| Agent Run | `agent.run.terminal`、`agent.run.duration` | Counter / Histogram (`status`) | `1` / `s` | PR4（Run 既有指标） |
| Agent Run | `agent.run.first_event.duration` | Histogram（无业务 label） | `s` | PR4（Run 既有指标） |
| Agent 模型 | `agent.model.attempt`、`agent.model.retry` | Counter (`purpose`, `model`, `outcome`) | `1` | PR4 |
| Agent 模型 | `agent.model.in_flight` | UpDownCounter (`purpose`, `model`) | `1` | PR4 |
| Agent 模型 | `agent.model.duration` | Histogram (`purpose`, `model`, `outcome`) | `s` | PR4 |
| Agent Tool | `agent.tool.total` | Counter (`purpose`, `tool`, `outcome`) | `1` | PR4 |
| Agent Tool | `agent.tool.in_flight` | UpDownCounter (`purpose`, `tool`) | `1` | PR4 |
| Agent Tool | `agent.tool.duration` | Histogram (`purpose`, `tool`, `outcome`) | `s` | PR4 |
| Agent RAG | `agent.rag.total` | Counter (`purpose`, `outcome`) | `1` | PR4 |
| Agent RAG | `agent.rag.in_flight` | UpDownCounter (`purpose`) | `1` | PR4 |
| Agent RAG | `agent.rag.duration` | Histogram (`purpose`, `outcome`) | `s` | PR4 |
| 后台任务 | `agent.job.total` | Counter (`purpose`, `status`) | `1` | PR4 |
| 后台任务 | `agent.job.in_flight` | UpDownCounter (`purpose`) | `1` | PR4 |
| 后台任务 | `agent.job.duration` | Histogram (`purpose`, `status`) | `s` | PR4 |
| 遥测自身 | `oj.telemetry.export.failure`、`oj.telemetry.dropped` | Counter | `1` | PR1 基础设施 |

延迟统一使用秒。需要毫秒展示时由 Grafana/PromQL 换算，不能在不同服务中混用单位。跨实例 P95/P99 必须从合并后的 Histogram 计算，不能平均各实例的分位数。

## Resource 与标签

每个服务通过 OTel Resource 设置：

- `service.name`：固定部署服务名，如 `gateway-service`、`judge-service`、`judge-worker`、`contest-service`、`agent-service`。
- `deployment.environment`：部署环境，默认 `development`，由 `OTEL_DEPLOYMENT_ENVIRONMENT` 或 Agent 配置提供。

Metrics 属性使用显式 allowlist。Go 的 `observability.Attributes` 和 Agent 的 `safe_metric_attributes` 会排序、规范化长度，并按 key 使用受控值集合；动态路由段、未知模型/工具/错误码、ULID、UUID 和业务 ID 统一归一为 `other`，未知 key 直接丢弃。新增服务、路由、工具、模型或错误码必须先登记到 Go/Python 两侧集合并补充负向测试。

允许的 key：

```text
service environment route rpc status outcome verdict language purpose
model tool error_code usage_source phase
```

`service` 和 `environment` 优先使用 Resource 属性，业务指标不应重复添加为 label。`route` 只接受已登记的规范化路由模板，`rpc` 只接受已登记的稳定方法名。`status`、`outcome`、`verdict`、`purpose`、`language`、`usage_source`、`phase` 使用固定枚举；`model`、`tool`、`error_code` 只能使用已登记的受控值，未知值为 `other`。Agent Tool 的允许值包括 `retrieve_knowledge`；后台 `status` 额外允许 `lease_lost`。

禁止将以下字段放入 Metrics label：

```text
user_id run_id trace_id span_id conversation_id submission_id
problem_id contest_id request_id idempotency_key source_code prompt
query payload input output URL token credential exception_message
```

这些值如确需排障，只能进入既有受控 Trace 属性、业务数据库安全投影或日志关联字段，并遵守各自隐私边界。Metrics 不记录源码、用户请求正文、模型输出、Tool 参数/结果、RAG 文本、数据集样本、凭据或原始异常。

## 状态与计数口径

- Counter 只在业务事实已经发生后增加；事务回滚、消息未收到 Publisher Confirm、任务未 ACK 或 Run 终态未提交不得记录成功。
- `oj.judge.submission.total` 只计算新创建/重判操作，不重复统计幂等重放；`oj.judge.result.total` 只在 Worker 收到结果事件的 Publisher Confirm 后增加；未确认的结果发布尝试记入 `oj.judge.result.publish.failure.total`，不代表 Broker 未接受（Confirm 可能丢失）；Judge Result consumer 只在处理事务成功后记为消费成功，重复事件仍按成功处理次数统计。
- `oj.judge.outbox.publish.total` 在 Outbox 状态持久化为 published 或失败/重试状态后计数；Outbox Dead 只在 dead 状态持久化成功后计数。Worker 对不可解析任务执行 `Reject(false)` 后的 DLQ 计数表示客户端成功提交 reject 操作；RabbitMQ consumer reject 没有 Publisher Confirm，因此不等同于 DLQ 队列持久化确认。RabbitMQ confirm、ACK/NACK 的业务顺序不受 Metrics 影响。
- Worker `in_flight` 覆盖任务开始处理到处理/发布/ACK 路径退出；进程重启后归零。Queue duration 使用当前任务 Envelope 的 `occurred_at` 到 Worker 开始处理的时间；重试消息使用重试消息自身入队时间。
- Contest projection 只在本地投影事务返回后记录；成功包括已处理事件、幂等重复和被旧版本保护逻辑忽略的事件。缓存 Outbox pending/oldest/dead 是每 15 秒独立查询快照，携带 `observed_at_seconds`；查询失败保留上次值并增加 observation failure，不能当成新鲜的 0。`backlog` 为 pending + dead，`pending` 与 `oldest_age_seconds` 只计算 pending；dead 是当前 dead 行数，retry 与 lease failure 是进程累计计数器。
- `completed`、`failed`、`cancelled`、`timeout`、`retrying` 等状态必须使用固定枚举。正常判题 verdict（AC/WA/CE/TLE 等）与基础设施失败分开统计。
- UpDownCounter 表示当前实例观察到的在途数量；进程重启归零，不代表历史总量。
- Contest 独立 `/metrics` 中的进程级 Counter 在进程重启时归零；短暂停机可能使最后一次事务之后、进程退出之前的业务计数未被 scrape 到。它们用于运行诊断，不是账务或审计事实。
- Agent Model/Tool/RAG Counter 在每次操作退出后记录；成功、错误、超时和取消分别使用固定 `outcome`。模型重试计数只在下一次 Provider attempt 确实开始时增加。Memory 按逻辑任务终态计数；其内部重试在确定安排下一次处理后单独记录一次 `status=retrying`。RAG ingestion 则每次领取的 attempt 只计一个终态；仅在 lease-fenced finish 已将失败任务持久化为 PENDING 后，以 `status=retrying` 记录，不再额外重复增加。`lease_lost` 表示失租 Worker 未能持久化任务完成状态；重领和退避仍由原存储逻辑控制。Agent 在途数只描述当前进程观察到的操作，进程重启后归零。
- Gauge/ObservableGauge 必须带 `observed_at` 或等价新鲜度语义；查询失败保留 unknown/stale，不写入 0。
- 指标采样不依赖 Trace 是否采样；Trace 缺失不能造成 Metrics 缺失。

## 遥测自身健康

`oj.telemetry.export.failure` 和 `oj.telemetry.dropped` 使用 `error_code` 标签记录受控原因，例如 `otel_export_failed`、`queue_full`、`collector_unavailable`。这些计数器也属于旁路指标，不得在记录失败时触发业务重试或阻塞请求。业务服务未配置 Collector 时，不应把 no-op provider 误报为业务故障；看板需要结合 Collector/Prometheus target 状态区分“未启用采集”和“采集异常”。

## 验证要求

新增指标时必须：

1. 在本文登记名称、类型、单位、标签和基数预算。
2. 使用共享属性 helper，并补充非法 key、UUID/业务 ID、过长值和秘密标记的负向测试。
3. 验证内存/手动 exporter 能收集到预期 instrument；Exporter、Collector 或 Prometheus 不可用时业务测试仍通过。
4. 更新对应 PR 的实现记录和 Grafana/PromQL 文档；不要直接开放任意 PromQL 或把业务 ID作为 label。
