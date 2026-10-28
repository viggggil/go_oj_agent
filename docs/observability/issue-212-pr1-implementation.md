# Issue #212 PR1：监控总览前端

## 交付内容

- 将 `/monitoring` 首页从入口卡片改为项目风格统一的运行总览，不依赖打开 Grafana 才能查看当前摘要。
- 复用现有 Gateway 固定 Metrics API，支持 `15m`、`1h`、`24h` 时间范围、手动刷新和页面隐藏时暂停轮询。
- 增加全局健康状态、请求/错误/Judge/Agent KPI、Prometheus 指标趋势图、业务运行状态、采集目标和服务指标接入状态。
- 保留 Agent 运行诊断与判题链路入口；Grafana 仅作为可选外部排障链接。
- 对尚未具备后端查询的 API 延迟分位数、Agent 成功率、TTFT、Token 用量和活跃告警显示明确的“待接入”说明，不用零值或绿色状态替代缺失数据。
- `admin`/`system_admin` 展示系统范围，`agent_admin` 只展示 Agent 范围；服务列表明确区分“有指标样本”和“健康探测未接入”。

## 现有后端边界

本 PR 不增加 REST、Prometheus 查询或服务健康探测接口。首页只消费 `/api/v1/admin/observability/metrics` 返回的固定序列和 scrape target；服务健康列表不能据此推断业务健康。API 延迟 P50/P95/P99、Agent 成功率、Token 用量、活跃告警和独立服务探测在 Issue #212 PR2/PR3 补充。

## 验证

- `npm run build`
- `npm test`
- `git diff --check`
- 监控 E2E 用例已更新首页 KPI、趋势、服务接入状态和 Agent/Judge 入口断言；当前环境缺少 Chromium 运行库 `libnspr4.so`，浏览器用例需在 CI 或安装 Playwright 系统依赖的环境执行。

