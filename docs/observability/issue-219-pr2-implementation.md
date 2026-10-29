# Issue #219 PR2：统一日志字段、Trace 关联和脱敏边界

PR2 让 Go 服务和 Agent 的 JSON 日志使用相同的基础字段。Go 服务通过共享 `pkg/observability.NewJSONLogger` 注入固定的 `service_name`，并从当前 OpenTelemetry context 派生 `trace_id`、`span_id`；没有有效 Trace 时不生成占位 ID。Judge Worker 和 Contest 将该 logger 设置为进程默认 logger，因此后台消费者、Relay 和判题阶段通过已有 context 自动关联日志。Gateway 的 Kratos logger 也使用同一 handler。

Agent formatter 将历史 `service` 字段改为 `service_name`，继续只输出 allowlist 中的 `error_code`、`error_type`、`run_id` 和 `request_id`，不输出 `exc_info`、stack trace 或未登记的 LogRecord extra。请求正文、模型输出、Tool/RAG 内容和凭据不进入普通日志。日志中业务 ID 仍然是 JSON 字段而不是 Loki label；后续查询 API 负责权限过滤和字段投影。

本 PR 不修改业务状态机、Trace 传播协议或日志查询 API。错误码的完整 allowlist 和各业务阶段的细粒度日志改造与 PR3/后续字段补齐一起完成；本 PR 的不变量是所有新建 logger 都保留 `service_name`，有效 OTel context 始终使用服务端派生的 Trace ID。
