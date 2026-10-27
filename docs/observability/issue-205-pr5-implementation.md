# Issue #205 PR5：Prometheus 抓取、Grafana 看板与告警

## 交付内容

- Prometheus 抓取 Collector Prometheus exporter、Collector 自身 8888 指标和 Contest 独立 `/metrics` 端口；OTel Collector 将 Resource 属性转换为稳定的 `service_name`/`deployment_environment` 标签。
- 增加 recording rules：HTTP 请求/错误速率、判题提交速率、判题排队 P95、Agent 模型错误比例；增加 scrape target、判题排队、Contest Cache Outbox 陈旧积压和 Agent 模型错误告警。
- Grafana provisioning 改为目录加载，提供系统/判题、Contest 投影与缓存、Agent Metrics、Collector 健康四套看板。面板显示 `noValue`/无样本状态，未把缺失数据渲染为零。
- Gateway 增加 `GET /api/v1/admin/observability/metrics?window=15m|1h|24h`。查询语句、窗口、响应大小、序列数量和点数均由服务端固定限制；不接受任意 PromQL、数据源 URL 或 Grafana URL。
- 平台管理员查询系统、Judge、Contest、Agent 和 Collector 摘要；`agent_admin` 只查询 Agent 与 Collector 摘要，不返回 Contest/system 指标，也不返回 Grafana 链接。普通用户仍由 Gateway 鉴权/授权拒绝。
- 监控工作区新增指标总览页面：展示 available/no-data/stale/unavailable/not-configured；轮询在页面隐藏时暂停，旧查询 abort，固定时间窗由后端校验。

## 可靠性与安全

Metrics 仍是业务旁路。Prometheus、Collector、Grafana 停止或查询超时不会改变提交、判题、Agent Run、Outbox、ACK、重试或后台租约。Prometheus 返回的未知服务标签折叠为 `other`，不会把业务 ID、查询语句、上游错误正文或任意 URL返回给前端。Grafana 链接仅由平台管理员使用服务端环境配置生成。

## 验证

- `go test ./services/gateway/internal/server ./services/gateway/internal/client`
- `npm run build`
- `npm test`
- `git diff --check`
- Prometheus、Collector、Compose、Grafana provisioning YAML 解析通过；五个 dashboard JSON 解析通过。
- 监控 E2E 用例已补充角色范围、无样本和固定查询场景；当前执行环境缺少 Chromium 依赖 `libnspr4.so`，浏览器未能启动，需在 CI 或带 Playwright 系统依赖的环境执行。

## 已知边界

本 PR 不配置 Alertmanager 外发通知，不引入基础设施 exporter，不允许用户自定义 PromQL。生产环境应在现有 SSO/网络边界后保护 Prometheus、Grafana、Collector 和 metrics 端口。
