# Issue #212 PR3：告警摘要、Trace 入口与运行文档

## 交付内容

- 新增 Gateway `GET /api/v1/admin/observability/alerts`，读取固定 Prometheus alerts 接口。
- 仅返回固定告警字段和稳定错误码，过滤未知告警、原始 annotation、业务 ID、地址和 TraceQL。
- 平台管理员查看系统告警，`agent_admin` 只查看 Agent 模型错误率告警；普通用户无权访问。
- 监控首页展示告警状态和受控摘要，并保留 Agent 诊断、Judge 链路入口；Loki 日志和 Alertmanager 外发通知继续留在后续 Issue。

## 状态语义

`not_configured` 表示没有配置 Prometheus，`unavailable` 表示查询失败，`available` 表示查询成功（包括空告警数组）。告警的 `trace_available` 当前为 `false`，因为 Prometheus 告警本身不携带可验证 Trace 关联；用户应通过固定模块入口按 Run 或 submission 查询受控 Trace。

## 排查流程

1. Metrics 为 `no_data` 时，检查时间范围和对应业务流量；空序列不表示零错误或服务健康。
2. 服务为 `not_configured` 时，表示当前没有独立探针；TCP `up` 只说明固定端口可连接，Agent `/readyz` 表示健康端点响应。
3. Metrics 为 `stale` 时，检查 Prometheus Targets 和最近抓取时间，再看 Collector 或服务 exporter 日志。
4. Alerts 为 `not_configured` 或 `unavailable` 时，检查 Gateway 的 Prometheus 配置和 Prometheus `/api/v1/alerts`；查询故障不等于“无告警”。
5. 对业务问题进入 Agent 或 Judge 页面，使用 Run/submission 详情打开现有受控 Trace 搜索；告警标签不提供任意 TraceQL 或跳转地址。

## 验证

- Gateway 告警 allowlist、权限、查询参数拒绝和字段脱敏测试
- Web build、unit test 和监控 E2E mock
- API、Metrics、架构和运行文档同步
