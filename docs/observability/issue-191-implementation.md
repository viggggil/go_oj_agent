# Issue #191 实施记录

## 范围

为 Agent 脱离 HTTP 请求执行的 Memory、Eval/模型评审和 RAG 文档 ingestion 增加本地 OpenTelemetry 任务 Span。任务各自创建本地 Trace，不持久化上游 trace context，不建立伪造的 HTTP 父子关系，也不接入 LangSmith 导出。

## 已实现

- Memory worker 为每个实际队列 job 创建独立根 Span `agent.memory.job`，每次重试另建 attempt Span，并追踪摘要生成和持久化阶段；记录完成/重试/失败/取消状态及稳定错误码。
- Eval Runner 创建 `agent.eval.run`、并发 `agent.eval.case.attempt` 和 `agent.eval.review` Span；Run/Case/Review 记录受控状态和 attempt。
- RAG worker 独立初始化 OTel exporter；标准观测 Compose overlay 向其注入 Collector 地址。每个已领取任务创建独立根 Span `agent.rag.ingest`，分别追踪对象读取、切分、批量 embedding/upsert 和终态持久化；确认租约仍有效后才记录完成。
- Span 不记录用户/会话身份、Eval 样本、Prompt/输出、RAG 内容、对象键或异常正文；错误码通过固定 allowlist；LangSmith 导出保持关闭。
- 取消、重试、持久化和租约代码路径未改变，OTel span 只围绕既有处理流程。

## 验证

```bash
make agent-fmt-check
make agent-lint
make agent-type
make agent-test-unit
```

本地内存 exporter 检查覆盖 HTTP 父上下文隔离、并发任务用途与根/子 Span 关系。当前本地缺少真实 RAG MySQL/MinIO/Qdrant 连接时集成用例会跳过；PR CI 的 RAG/Agent Docker workflow 负责验证真实环境。

本地环境的 Makefile 调用 `uv`。若用户 shell profile 配置的临时 uv 目录失效，可直接使用 `agent/.venv/bin/ruff` 与 `agent/.venv/bin/mypy` 执行同一检查。
