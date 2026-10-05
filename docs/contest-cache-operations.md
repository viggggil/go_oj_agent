# Contest 排行榜缓存的部署与验证

本阶段对应 issue #134。MySQL 保存提交事实、题目结果和用户汇总；Redis 保存可重建的查询投影。缓存允许短暂投递延迟，单次页面的总数、排名和各题内容在一个 Lua 读取内保持一致。不同页面请求仍使用实时榜单。

## 启用和升级

默认本地配置和 Compose 配置均设置 `leaderboard_cache.enabled: true`。没有 Redis 连接时 Contest 仍可启动、消费结果和通过 MySQL 返回排行榜。缓存开关不控制 RabbitMQ 消费者；关闭缓存不会停掉成绩更新。

已有安装应按以下顺序升级：

1. 暂停全部 Contest 实例和比赛提交，保留 RabbitMQ，待处理结果仍在持久队列中。
2. 备份 Contest 数据库。使用部署使用的迁移流程确认 `000001`、`000002` 已应用，再应用 `000003_create_leaderboard_outbox` 和 `000004_index_cache_lag`。初始化 SQL 挂载仅处理新 MySQL 卷，已有卷不能通过重新启动自动补迁移。
3. 在消费者暂停期间回填已有题目结果。该命令按用户提交事务，可安全重跑，不重置已存在的用户版本：

   ```bash
   CONTEST_MYSQL_DSN='.../oj_contest?parseTime=true' \
     go run ./services/contest/cmd/leaderboard-backfill
   ```

4. 更新 Judge 和 Contest。PR4 的比赛内部提交新增开始/结束时间字段，旧 Contest 不能和新 Judge 继续创建比赛提交；同时部署后恢复比赛提交。普通题库提交无此字段要求。更新调用 `UpdateContest` 的客户端，使其原样携带详情中的 `updated_at` 到 `expected_updated_at`。
5. 确认 Redis 地址、密码、DB、namespace 和超时配置，启动 Contest。访问一个已授权比赛的排行榜，首次缺失回源 SQL，后台登记比赛并重建。仅报名、尚无结果的用户仍不上榜。
6. 观察命中率、SQL 回源、Outbox 积压和最老事件年龄。冷启动没有活动代次时 relay 会重试；首次访问触发重建后恢复投递。没有启动时全量扫描全部比赛，以免大量历史比赛同时消耗数据库。

迁移顺序为 `000001` → `000002` → `000003` → `000004`。PR4/PR5 不增加新的业务表。新安装在 Compose 初始化流程中应用全部迁移。不得清空业务事实或数据库卷来完成升级。

临时禁用：将 `leaderboard_cache.enabled` 设为 `false`，或使用 `KRATOS_LEADERBOARD_CACHE_ENABLED=false` 覆盖配置，然后按正常部署流程重启 Contest。此时 relay/maintenance 不启动，排行榜直接查询 MySQL，结果事务继续生成 Outbox。重新启用后首次访问触发重建，保留的 pending 事件继续重试。

应用回滚时优先关闭缓存，保留 `000003` 的用户版本和投递状态。删除派生表会丢失版本栅栏和投递进度，再升级需要回填并重建全新的 Redis 代次；不能重置单个用户版本后继续使用原代次。`000004` 的 down 仅删除索引。

## 运行观测

`server.metrics_address` 控制独立运维端口，空字符串禁用。默认本地监听 `127.0.0.1:9105`；Compose 内部监听 `:9105`，可由同一网络中的监控服务抓取 `contest-service:9105/metrics`，不映射公网端口。

```bash
curl http://127.0.0.1:9105/metrics
curl http://127.0.0.1:9105/debug/vars
```

端口仅暴露进程级数值，不包含用户/比赛 ID、源码或默认的 pprof handler。`/metrics` 是 Prometheus 文本格式；`/debug/vars` 是相同计数的 JSON。进程重启后计数归零，多实例分别抓取后聚合。

| 指标（前缀均为 `contest_leaderboard_`） | 含义 |
| --- | --- |
| `cache_hits`、`cache_misses`、`cache_errors` | API Repository 缓存命中、回源请求、Redis 非结构错误；cooldown 中的请求计为 miss |
| `sql_fallbacks` | 实际 SQL 回源次数，经过 singleflight 合并；短期 SQL 页面复用不重复计数 |
| `outbox_backlog`、`outbox_dead` | pending/dead 队列数量及 dead 数量，15 秒刷新一次 |
| `outbox_oldest_microseconds` | 数据库 UTC 时间计算的最老 pending/dead 年龄；空队列为 0 |
| `outbox_observed_at_seconds`、`observation_errors` | 上次成功刷新时间及刷新失败次数，用于识别保留旧 gauge 的监控故障 |
| `outbox_applied`、`outbox_failures` | 成功确认次数；sink 重试、dead、claim 和确认失败次数 |
| `update_microseconds_total` | 已成功确认事件从数据库 created_at 到应用/确认的累计延迟，除以 applied 增量可观察平均延迟 |
| `rebuilds`、`rebuild_failures`、`rebuild_microseconds_total` | 实际重建尝试、失败及累计耗时；争抢租约得到 busy 不计尝试/失败 |
| `transaction_retries` | MySQL 死锁/锁等待超时的完整事务重试次数 |

结构不完整、dirty、签名变化、代次切换和过期均触发回源；Redis 连接错误另外触发 5 秒 cooldown。判题写入不等待 Redis。维护线程只处理被请求过的比赛，最多并行两项；每场比赛每 5 秒检查，代次 TTL 为 10 分钟，周期对账年龄为 5 分钟。超过 30 秒或最老未投递事件造成的可接受陈旧窗口会拒绝缓存读取。

## 故障处理

- **Redis 断开或 key 丢失**：先看 `cache_errors`、`sql_fallbacks` 和 pending backlog。成绩仍写入 MySQL。恢复 Redis 后后台重新从一致性 SQL 快照构建新代次；不能仅重放未确认事件恢复已被淘汰的已确认成绩。重建失败采用退避。
- **缺少汇总**：日志出现 `leaderboard summaries missing; run migration backfill` 时，确认迁移和回填已执行。暂停消费者完成回填后再恢复，不清空事实。
- **dead 事件**：查看 `contest_cache_outbox.last_error`、payload schema 和版本，修正产生错误快照的代码或数据损坏原因，再使用受控运维流程恢复该事件投递。不要将坏消息盲目改为 applied；dead 会使榜单无法维持可信的新鲜度，安全行为是 SQL 回源。
- **进程在 claim 后退出**：30 秒租约到期后新实例可领取。旧 token 无法确认新租约；Redis 已写入但确认失败时完整快照安全重放，版本不会回退。
- **多实例重建冲突**：busy 是正常结果。旧 owner 不能续租、激活或清理新 owner 的代次。不要只删除活动指针或单个用户版本来强制更新；等待维护重建完整代次。
- **管理员编辑冲突**：`ABORTED` 表示更新时间令牌已过期，重新读取详情并让管理员确认最新配置。不要自动把原请求用新的令牌重发，避免覆盖他人编辑。

诊断 SQL 仅读 Contest 自有派生数据：

```sql
SELECT status, COUNT(*) FROM contest_cache_outbox GROUP BY status;
SELECT id, contest_id, user_id, result_version, retry_count,
       lease_until, last_error, created_at
FROM contest_cache_outbox
WHERE status IN ('pending', 'dead') ORDER BY created_at, id LIMIT 50;
```

## 可重复验证和性能记录

```bash
make test-integration
go test -race ./services/contest/internal/biz \
  ./services/contest/internal/data ./services/contest/internal/server
```

Docker runner 使用独立 project 和测试卷、随机端口；测试创建/删除随机 MySQL schema，Redis 只操作测试 namespace。MySQL 测试账户需要建库权限。Runner 结束时只删除测试 project，不接触当前业务容器。

| 行为 | 验证位置 |
| --- | --- |
| 并发编辑冲突、报名/归档顺序、令牌严格递增 | `contest_concurrency_integration_test.go` |
| 精确开始/结束边界，以 MySQL statement clock 验证 | Judge `contest_deadline_integration_test.go` |
| 同用户结果并发、事务 rollback、旧消息/乱序/失效 | `leaderboard_outbox_integration_test.go`、结果消费者和 E2E 测试 |
| Redis 写后确认失败、新 relay 接手、重复消费不加版本 | `leaderboard_failure_integration_test.go` |
| 两个 Repository 争抢重建、旧 owner 失效 | `leaderboard_failure_integration_test.go`、generation 测试 |
| 构建期间更新、排序并列/ID 2 与 10、空榜分页、部分 key/全丢失 | `leaderboard_rebuild_integration_test.go`、generation/Redis 测试 |
| 真实 socket 断开、成绩继续入库、连接恢复后 SQL 重建 | `leaderboard_failure_integration_test.go`（MySQL + 独立 miniredis）；真实 Redis Lua 另有集成覆盖 |
| 归档权限、命中不读 SQL、回源合并/取消 | biz leaderboard access 测试、`leaderboard_cache_test.go` |
| 100 用户/10 题的混合读写负载、分页逐项 SQL 对比、丢失后恢复 | `leaderboard_load_integration_test.go` |

独立运行负载测试（地址指向专用测试依赖，不能指向生产实例）：

```bash
CONTEST_TEST_MYSQL_DSN='.../oj_contest?parseTime=true' \
CONTEST_TEST_REDIS_ADDR='127.0.0.1:6379' \
  go test -count=1 -v ./services/contest/internal/data \
  -run TestLeaderboardLoadAndRecoveryProfile
```

测试日志记录 OS/架构、CPU 线程数、参赛人数、题目数、写入速率、刷新 QPS、命中率、SQL 回源、P95/P99、relay 时间、重建与缓存丢失后的恢复耗时。样本为 100 用户、10 题、page_size=20、8 个读 worker 共 400 次请求、4 个写 worker 共 20 个结果。SQL 基线是 100 次单线程 Repository 查询；Redis 样本为混合并发，不能把两者 QPS 比值当成固定加速倍数。它衡量 Repository 路径，尚不包含 Gateway、认证、公网延迟和生产规模容量。

2026-10-05 的本地实验（Linux amd64、Ryzen 7 8845H、16 线程、Docker MySQL 8.4/Redis 7.4）：热读约 1050 QPS、命中率 100%、回源 0、P95 11.46 ms/P99 18.68 ms；SQL 单线程约 20.8 QPS、P95 54.13 ms/P99 67.53 ms；混合窗口结果写入约 52.5 条/秒；初次重建约 134 ms，key 全丢失后的 SQL 重建约 130 ms。生产性能目标需要在实际参赛人数、题目数和刷新负载下再次测量，本实验只提供可重复的基线与正确性验收。
