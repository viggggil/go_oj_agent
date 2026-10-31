# Trace 能力现状

本文记录当前仓库已经实现并可在本地观测栈中使用的分布式 Trace 能力。Trace 是诊断数据，不能替代提交、判题结果、Agent Run 或消息状态等业务事实。

## 采集与导出

Go 服务和 Agent 都使用 OpenTelemetry SDK，采用 W3C Trace Context（`traceparent` / `tracestate`）传播上下文，并通过 OTLP gRPC 将 Span 发往 OpenTelemetry Collector，再由 Collector 导出到 Tempo。

各进程通过 `OTEL_EXPORTER_OTLP_ENDPOINT` 控制导出。未配置 endpoint 时仍创建本地 Trace Provider，但不向外部发送 Span；观测后端不可用不会阻塞业务请求、判题消息处理或 Agent Run 终态。Go 端使用批量导出和有限超时，Agent 端使用有界批处理队列。

Gateway 会删除公网请求携带的 `traceparent` 和 `tracestate`，然后为请求建立可信的服务端根 Trace，避免外部调用方直接指定内部 Trace 根。内部 HTTP、gRPC 和 Agent 调用会在此上下文上继续创建子 Span。

## 已覆盖的链路

### Gateway 与同步 RPC

- Gateway HTTP 请求使用 `otelhttp` 建立 `gateway.http` Server Span。
- Gateway 到 Agent 的 HTTP 调用使用 `otelhttp` Client Transport，Trace Context 会注入请求元数据。
- Gateway 到 User 的 gRPC 调用使用 `otelgrpc` Client Handler。
- Judge、Problem 的 gRPC Server 使用 `otelgrpc` Server Handler。
- Judge 到 Problem 的 gRPC Client 使用 `otelgrpc` Client Handler，因此可以看到 Gateway → Judge → Problem 的同步调用关系。

### Judge 分布式判题链路

当前 Judge Trace 覆盖一次提交从入口到结果处理的主要阶段：

1. Judge 创建提交时的 `judge.submission.create` Span。
2. Judge Outbox 持久化和发布时的 `judge.outbox.publish` Span。
3. RabbitMQ 中的判题任务由 Judge Worker 提取上下文，并创建 `judge.worker.task` Span。
4. Worker 发布完成、失败或重试结果时创建 `judge.worker.result.publish` 或 `judge.worker.retry.publish` Span。
5. Judge 消费结果并创建 `judge.result.consume`、`judge.result.apply` 及结果事务 Span。
6. Contest 消费判题结果并创建 `contest.result.consume`、`contest.projection.apply` 和事务 Span。
7. Contest Cache Outbox 应用排行榜缓存时创建 `contest.cache_outbox.apply` Span。

异步边界同时使用两种传播方式：

- RabbitMQ Header 携带 W3C `traceparent` / `tracestate`。
- 稳定事件 Envelope 保存可选的 `traceparent`、`tracestate`、`trace_id` 和 `causation_id`，用于 Outbox 持久化、重试和混合版本消费者。

缺失或格式错误的传播字段不会拒绝消息；消费者会从新的 Trace 开始处理。Outbox 记录会在业务事务中保存上下文，重试发布时继续使用原有关联信息。

Span 会附带受控的提交、题目、比赛、事件、路由键、判题尝试次数和重试次数等属性，用于定位阶段和耗时。不会把源码、Prompt、用户输入、凭据或任意异常正文写入 Span。

### Agent Chat 与后台任务

Agent Chat 的本地 Trace 包括：

- `agent.run`：一次聊天 Run 的生命周期、完成/失败/取消状态和 Run ID。每条用户消息对应一个独立 Run Trace；会话级历史通过 Agent 业务记录查看。
- `agent.context.load`：加载历史会话上下文的阶段和历史轮数。
- `agent.step`：ReAct 推理轮次、工具执行和最终回答整理阶段，通过 `agent.step`、`agent.phase` 表示业务顺序。
- `agent.model.attempt`：模型名称、调用序号、输入/输出 Token、用量来源和成功/错误/超时/取消结果。
- `agent.tool`：受控工具名称及工具状态；`agent.rag.retrieve` 会记录结果数量和截断状态。
- `agent.rag.retrieve`：RAG 检索操作。
- `agent.grpc.client`：Agent 调用 User、Problem、Contest、Judge 等业务 gRPC 的客户端 Span。

Trace 详情按 `ParentSpanID` 构建树形时间线，底层 RPC Span 默认收纳在工具阶段下；短耗时 Span 不会被丢弃，只在界面中折叠。Run、模型、工具和 RAG 阶段的错误日志可以按 Trace ID 与 Span ID 查询。

Agent 的后台任务各自建立任务 Trace，当前任务用途包括：

- `memory`：会话记忆生成与持久化。
- `eval`：评测运行及评测尝试。
- `review`：后台 Review 任务。
- `rag_ingestion`：RAG 文档摄取任务。

后台任务会记录受控的 `agent.purpose`、任务状态、尝试次数和稳定错误码。手动 Review 会脱离原始请求上下文，作为独立根 Trace，避免把长时间后台任务错误地挂在已经结束的请求下。

### 结构化日志关联

Gateway、Judge、Judge Worker、Contest 和 Agent 的 JSON 日志从当前 OTel 上下文读取 `trace_id` 与 `span_id`。日志采集到 Loki 后，可以使用 Trace ID 查询同一条链路的相关日志。Trace ID、Span ID、Run ID 和 submission ID 是日志内容字段，不作为 Loki 高基数索引标签。

## 当前查询入口

### Agent Run 诊断

监控页面的 Agent 运行诊断调用：

- `GET /api/v1/admin/agent/observability/runs`
- `GET /api/v1/admin/agent/observability/runs/{run_id}`

返回 Run ID、状态、开始时间、耗时、受控错误码、工具步骤数、RAG 次数和可空 `trace_id`。Run 有本地 Trace ID 时，监控页面直接进入站内 Span 详情并按同一 Trace ID 查询 Loki 日志。`tempo_url` 暂保留用于旧客户端兼容。

响应不会返回聊天正文、完整 Prompt、工具参数/结果、RAG 正文、原始异常或凭据。历史 Run 没有本地 Trace 时，`trace_id` 为 `null`。

### Judge Trace

平台管理员可以使用以下站内查询接口：

```text
GET /api/v1/admin/observability/judge/traces?window=15m|1h|24h&submission_id=<submission_id>
```

Gateway 通过服务端固定配置 `OBSERVABILITY_TEMPO_URL` 查询 Tempo，并只返回固定服务范围的 Trace 摘要；提交编号会先经 Judge 校验，不接受客户端提供的 Tempo 地址或任意 TraceQL。单条 Trace 使用 `GET /api/v1/admin/observability/traces/{trace_id}` 返回 Span 树和受控属性。`agent_admin` 只能查询 Agent/RAG 服务范围。

### 日志关联

日志中心支持使用 Trace ID 查询 Loki：

```text
GET /api/v1/admin/observability/logs?trace_id=<32位小写十六进制TraceID>
```

Gateway 使用固定 LogQL 模板查询，限制时间窗、服务范围、返回条数和返回字段。Trace 详情页按 Trace ID 展示关联日志和 Span ID。

## 权限与脱敏

- `admin`、`system_admin` 可以查询系统范围的 Trace 入口和日志。
- `agent_admin` 只能访问 Agent 运行诊断、Agent/RAG 日志范围和 Agent 相关告警。
- 普通用户不能访问管理员监控接口。
- 后端不接受任意 PromQL、LogQL、TraceQL、Tempo URL 或 Loki URL。
- Span、日志和 Agent Run 诊断只保留低基数服务属性、业务关联 ID 和稳定错误码；敏感正文默认不进入观测数据。

## 当前边界

- Trace 查询通过 Gateway 的固定 Tempo 代理完成，当前返回有限数量的 Span JSON 投影，不提供任意 TraceQL 或实时 tail。
- Judge 入口按提交属性搜索，Trace 是否存在仍取决于采样、导出和 Tempo 保留期。
- 采样、Tempo 保留和 Collector 导出状态由观测栈决定，业务 API 不保证每个请求都有可查 Trace。
- 默认业务 Compose 不启动观测后端，需要执行 `make observability-up` 才能使用本地 Collector、Tempo、Prometheus、Loki 和 Grafana。
- Problem Service 和 RAG Worker 当前不在 Alloy 的日志采集 allowlist 中，因此它们可能有 Trace，但不能保证在日志中心按 Trace ID 查到对应日志。
- 当前没有 Alertmanager 外发通知，也没有实时 Trace tail、通用 Trace 导出或跨租户 Trace 管理能力。

## 相关文档

- [Observability 系统设计](../observability/design.md)
- [Metrics 指标字典](../observability/metrics.md)
- [Loki 日志架构与运行约束](../observability/loki.md)
- [HTTP API 契约](../api.md)
- [本地 Compose 配置](../../deploy/compose/README.md)
