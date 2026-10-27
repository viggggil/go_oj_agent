# Issue #205 PR6：可观测性端到端检查与容量基线

## 交付内容

- `scripts/observability_check.sh` 校验 Compose、Collector、Prometheus、Grafana provisioning 的 YAML/JSON 结构；在可用环境中调用 `promtool` 和 `docker compose config` 做语法检查。
- `scripts/observability_smoke.sh` 检查 Collector、Prometheus、Grafana 健康端点、已 provision 的系统看板、固定 recording rule，以及 Gateway 指标 API 的可达性。Gateway 返回 401/403 也视为端点已到达，因为 smoke 不伪造管理员凭据。
- Makefile 提供 `make observability-check` 和 `make observability-smoke`；CI 在依赖安装后运行配置检查。
- 本文记录真实容量测量方法和结果模板。没有运行栈或没有产生业务流量时，指标填写 `N/A`，不得把空样本写成零。

## 本地验证

```bash
make observability-check
make observability-up
make observability-smoke
```

`observability-up` 启动的是可选 overlay，业务服务不依赖其健康状态。smoke 只证明端点和查询接口可达，不代表已经完成真实提交、判题或 Agent 聊天；要验证业务流量，应在测试环境执行一次受控流程后再采集样本。

完成受控提交、判题、Contest 投影和 Agent 聊天流程后，可要求固定查询必须有样本：

```bash
OBSERVABILITY_SMOKE_REQUIRE_SAMPLES=true make observability-smoke
```

该模式会拒绝 Prometheus 空结果，适合在集成环境运行；它仍不替代业务流程本身的成功、失败、重试、取消和 Collector 停机恢复断言，这些流程应由现有集成测试或值班演练记录请求 ID、终态和对应时间窗。

## 容量基线模板

在固定机器规格、服务副本数、抓取周期和测试流量下记录：

| 项目 | 命令/来源 | 结果 |
| --- | --- | --- |
| Prometheus scrape interval | `deploy/compose/prometheus.yaml` | 15s |
| Prometheus retention | `deploy/compose/observability.yaml` | 7d |
| 时间范围与流量 | 测试报告（提交/聊天请求数） | N/A |
| 活跃序列数 | `count({__name__=~"oj_.*|agent_.*|http_.*"})` | N/A |
| Prometheus 内存/CPU | `docker stats prometheus`，记录稳定窗口 | N/A |
| Collector 内存/CPU | `docker stats otel-collector`，记录稳定窗口 | N/A |
| Metrics API 延迟 | `curl -w '%{time_total}'`，重复 20 次取 P95 | N/A |
| exporter drop/failure | `oj_telemetry_dropped_total`、`oj_telemetry_export_failure_total` | N/A |

建议先保存基线，再逐步增加副本和请求量；序列数异常增长时优先检查标签是否引入业务 ID、ULID 或未知值。生产环境应根据实际磁盘、内存和保留期重新测量，本文默认值不是容量承诺。
