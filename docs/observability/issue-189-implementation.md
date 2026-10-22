# Issue #189 实施记录

## 范围

本阶段补齐 Agent Chat 的本地 OpenTelemetry 业务追踪。一次聊天的 Trace 从 Gateway/Agent HTTP 边界继续覆盖 Agent Run、模型 SSE Attempt、Tool、RAG 检索和 Agent 发起的业务 gRPC 调用；业务 Run 在 Agent 自有数据库中保存可空的 `trace_id` 关联。

本阶段不启用 LangGraph 自动内容追踪，不上传用户消息、Prompt、模型输出、Tool 参数/结果、RAG 正文或原始异常；LangSmith 脱敏导出由 Issue #188 单独维护。

## 已实现

- `agent_runs.trace_id`：新增 `000011_add_run_trace_id` migration，字段为可空 ASCII 32 位 Trace ID，并建立 `(trace_id, started_at, id)` 索引。历史 Run 保持 `NULL`。
- Agent Store / `RunRecord`：创建 Run 时保存当前有效 OTel Trace ID，读取和终态更新继续兼容旧数据。
- Agent Run Span：覆盖完整异步 SSE 消费和清理生命周期，记录受控的 `agent.purpose`、`run.id`、`run.status`。
- 模型 Span：每次 Responses SSE Attempt 使用 `agent.model.attempt`，Span 覆盖响应体消费、完成、失败和取消。
- Tool/RAG Span：Tool 执行使用 `agent.tool`，知识检索使用 `agent.rag.retrieve`；只记录受控状态和错误码。
- 业务 gRPC：Agent Client 使用 `agent.grpc.client` Span，并通过 W3C `traceparent` metadata 传播当前上下文。
- 日志与导出：沿用现有 JSON trace/span 关联；Span provider 关闭异常文本记录，观测失败不阻断业务。

## 数据和隐私约束

- `run_id` 可出现在 Trace 属性中用于运维关联，但不作为 Metrics 标签。
- Span 名称、属性和稳定错误码使用 allowlist；不记录请求正文、URL、凭据、Provider 错误正文和自由文本。
- Trace 不可用时，业务 Run 仍由 MySQL 终态事实驱动；无有效 Span Context 时写入 `NULL`。
- 业务成功仍以终态事务提交为准，HTTP 200、SSE 建连和模型完成都不提前决定成功。

## 验证方式

```bash
make agent-fmt-check
make agent-lint
make agent-type
make agent-test-unit
```

需要真实 Agent MySQL 的迁移和持久化验证时，使用项目现有的 `make test-integration`。本阶段只增加一条聚焦的 Trace ID 持久化集成测试，其他行为复用已有 Runtime、Tool、gRPC 和 LangSmith 隐私测试。

## 后续工作

管理员 Run 查询、固定 PromQL、Vue Observability 页面属于 PR3；Judge/Outbox/RabbitMQ/Worker 的持久化 Trace Context 属于后续异步链路阶段，不在本记录范围内。
