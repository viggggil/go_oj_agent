# Issue #205 PR3：Contest 投影、缓存与 Outbox Metrics

## 实现范围

- Contest `/metrics` 输出对齐 `metrics.md` 的稳定 `oj_contest_*` Prometheus 名称、HELP/TYPE、秒单位和有限 `outcome=success|error` 标签。
- 记录 Contest 投影事务总量与耗时直方图；成功包含已提交的幂等重复和旧版本忽略结果。另独立计数在本地事务提交的唯一 processed event ID。
- 将原 expvar 缓存命中/未命中/错误、SQL 回源、重建、Outbox 应用/重试/故障和事务重试计数映射为规范 Prometheus 指标。
- 增加 Contest Cache Outbox pending、pending+dead backlog、oldest pending age、dead 数 Gauge，以及 observation timestamp；查询失败保留上次观测并增加 observation failure。
- `/debug/vars` 保留已有 JSON 键以兼容现有运维脚本。
- 观测 overlay 中 Prometheus 新增 `contest-service:9105/metrics` 抓取目标；默认 `make infra-up` 不依赖 Prometheus。

## 异步可靠性边界

投影指标只在既有 DB 事务返回后记录。重复投递仍通过原 `processed_events` 逻辑收敛；乱序/旧版本保护、排行榜重算、缓存版本与 generation、Outbox transaction、lease fencing、retry backoff、完成/失败持久化均未改变。缓存 Outbox retry counter 只在 retry 状态持久化成功后增加；lease 丢失单独计数。Metrics exporter 或 endpoint 停止不参与业务执行条件。

缓存 Outbox Gauge 由既有后台 Worker 每 15 秒单独查询，包括 Redis 未启用时；它不是事务内数据，也不阻塞 Projection ACK。`observed_at_seconds` 表示最近一次成功查询时间。查询故障不把旧值覆盖成 0。

## 验证

- `go test ./services/contest/...`：通过。
- `go test -race ./services/contest/internal/server ./services/contest/internal/data`：通过；覆盖并发 histogram scrape 一致性。
- `make fmt-check`、`make vet`、`make build`、`go test ./...`：通过。
- `promtool check config`、`promtool check metrics` 与 Compose observability 配置检查：通过。
- Outbox 观测查询失败的测试确认旧 Gauge/observed_at 保留，observation failure 增加。
- Async reliability review：RELIABLE FOR REVIEWED SCENARIOS。指标变更不改投影事务、去重、Outbox 状态或 ACK 顺序。
- 独立代码审查：APPROVE。进程级 Counter 在重启窗口可能漏采样，属于运行指标限制，不影响业务状态。
