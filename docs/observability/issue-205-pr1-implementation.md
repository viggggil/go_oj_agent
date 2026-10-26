# Issue #205 PR1：Metrics 契约与采集基础

## 已完成

- 新增 [Metrics 契约](metrics.md)，固定 OTel 名称、单位、类型、Resource 属性、标签 allowlist、隐私边界和状态口径。
- `pkg/observability` 新增共享 Meter 名称、低基数 `Attributes` helper，以及 `oj.telemetry.export.failure` / `oj.telemetry.dropped` 两个遥测健康 Counter。
- Go 属性 helper 会排序 key、限制值长度、拒绝 UUID、ULID 和数字业务 ID，并按 key 校验受控服务、路由、RPC、状态、模型、工具和错误码集合；非法值归一为 `other`，未知 label key 直接丢弃。
- Agent 新增 `safe_metric_attributes`，与 Go 侧共享同一 allowlist、受控值集合和归一化策略；不允许 run/submission/user 等 ID 进入 label。
- Go 手动 Reader 测试和 Agent 隐私负向测试覆盖 instrument 创建、低基数属性、UUID/数字 ID、秘密 key 和导出计数。

## 兼容与边界

本 PR 不在 Judge、Worker、Contest 或 Agent 业务路径新增领域指标，也不修改判题状态机、Outbox、RabbitMQ、ACK/NACK、租约或 Agent 终态。现有 Go `Init` 和 Agent `configure` 的 no-op 行为保持不变；没有 OTLP endpoint 时不会阻塞业务。

Prometheus 抓取目标、Contest `/metrics` 纳入 scrape、Grafana 看板、告警和领域指标分别在 Issue #205 后续 PR 实现。PR1 的两个 telemetry health Counter 是共享基础设施 API，后续 exporter/业务适配器按固定错误码记录。

## 验证

```text
go test ./pkg/observability
agent/.venv/bin/python -m pytest tests/test_metrics.py tests/test_background_observability.py -q
agent/.venv/bin/ruff check app/metrics.py app/observability.py tests/test_metrics.py
agent/.venv/bin/ruff format --check app/metrics.py app/observability.py tests/test_metrics.py
```
