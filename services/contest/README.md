# Contest Service

Contest Service 当前提供 API contract、运行时初始化、Contest 基础 CRUD 和比赛提交校验，
提供以下内部 gRPC 接口：

```text
CreateContest
GetContest
ListContests
UpdateContest
ArchiveContest
JoinContest
GetLeaderboard
```

Contest 和 ContestProblem 使用 `oj_contest` MySQL schema；创建和全量更新在同一事务中
替换题目集合。管理员只能创建、更新或归档尚未开始的 DRAFT 比赛，状态由开始/结束
时间计算。排行榜由 `submission.judged` / `submission.invalidated` 消费者维护本地事实和
题目结果投影，仅读取 `oj_contest`，不访问 Judge 数据库或 Redis。比赛提交会在比赛服务完成报名、时间和题目归属校验
后转发到 judge-service。用户通过 `JoinContest` 或 Gateway 的
`POST /api/v1/contests/{contest_id}/join` 报名；报名仅允许在比赛开始前，重复报名幂等。

## 结果投影与排行榜

- 应用 `migrations/contest/000002_create_result_projection.up.sql`；新增 processed events、Submission facts 和 problem results 三张表。
- `oj.events` 上的 `contest.submission-judged` durable queue 同时绑定 `submission.judged` 和 `submission.invalidated`；默认 prefetch=16，manual ACK，DLQ=`contest.results.dlq`。
- 协议无效消息 Reject(false)；数据库、本地比赛/报名/题目归属校验失败 NACK(requeue=true)。成功提交事务后 ACK，ACK/NACK 失败使消费者退出并交由运行环境重启。
- `event_id` 幂等；同一 Submission 仅接受更大的 `judged_at`。生产者必须为结果修正提供严格递增时间；同一时间视为同一版本。作废记录不可恢复，即使 judged 迟到也不会复活。
- 按 `submitted_at, submission_id` 重算全部局部 facts。AC 前 WA/TLE/MLE/RE/CE 每次罚时 1200 秒；SYSTEM_ERROR 不计错误次数；首次 AC 用提交时间计分。
- `GetLeaderboard` 按 solved DESC、penalty 秒 ASC、user_id ASC 排序，分页最大 100，返回所有比赛题目的 solved/wrong_attempts/accepted_at。没有结果的参与者尚不进入榜单。
- 原 `accepted_count`/`score` 为 ACM solved 数，`penalty` 为秒；新增 `solved_count`/`penalty_seconds` 是显式同义字段，保持旧字段编号。
- 配置 `messaging.url/exchange/queue/dead_letter_queue/prefetch`，生产启动必须提供 messaging；连接或拓扑准备失败会阻止服务启动。支持 `KRATOS_MESSAGING_URL` 覆盖。
- `make test-integration` 包含 RabbitMQ/MySQL 重复、乱序、版本更新、作废、停机投递、未 ACK 重投与 Gateway 比赛 WA -> AC E2E。

## Local run

```bash
go run ./services/contest/cmd/contest-service \
  -conf services/contest/configs/config.yaml
```

默认 gRPC 地址为 `:9005`。配置支持 Kratos `KRATOS_*` 环境变量覆盖；配置中提供了
内部认证和 Consul 注册接入点，默认关闭。

## Generation

Contest API 和服务配置 proto 都通过 Buf 生成：

```bash
make generate
```

生成文件包括 `api/contest/v1` 下的 Go API、gRPC 和 validation 代码，以及
`services/contest/internal/conf` 下的配置代码。

## Contest details

`GetContest` returns `contest.joined` for the authenticated actor. Membership is queried from `contest_participants` on each detail request and is independent of contest lifecycle. The default false value may be omitted by the JSON codec; clients should treat an absent value as false.

Each `ContestProblem` includes a read-only `title`. Contest resolves all problem IDs in one `ProblemService.BatchGetProblems` call, preserving the incoming actor ID and roles in internal authentication. Configure the `problem` client alongside `judge` (endpoint, timeout and signing credentials). Titles submitted in create/update requests are ignored. Missing or inaccessible problems have an empty title; clients can show the problem ID. A failed batch call fails the detail request rather than silently presenting incomplete titles.

No schema migration is needed. The deployment must update Problem Service (new RPC), Contest Service (client/config) and Gateway (new response fields) before the web client.

## Leaderboard summaries and cache Outbox (issue #134, PR1)

Migration `000003_create_leaderboard_outbox` adds `contest_user_results` and
`contest_cache_outbox`. Accepted result facts, per-problem rebuilds, user totals,
monotonic versions and complete JSON snapshots commit in the same transaction.
The existing participant row lock serializes changes for one user. Duplicate or
stale events do not advance the version. An event whose fact changes but leaves
scores unchanged still publishes a new complete snapshot (e.g. wrong-attempt
metadata). Snapshots include all contest problems, omit rank and use a decimal
string for the version to preserve integers beyond JavaScript/Lua's 2^53 limit.

The relay claims one event at a time using `FOR UPDATE SKIP LOCKED`. Each claim
has a unique token, a 30-second lease and a 3-second sink timeout. Confirmation
and failure updates require an unexpired matching token. Transient failures
retry indefinitely with capped exponential backoff (1–60 seconds); malformed
snapshots enter `dead` with `last_error` for investigation. A cancelled process
leaves its lease to expire. Confirmation failures safely replay the snapshot.
PR1 provides the relay contract; it is not started until the Redis sink is added.
Cache writes and cache reads remain disabled by default.

### Upgrade existing data without dropping volumes

1. Stop all Contest Service result consumers/instances. Leave RabbitMQ running;
   durable result messages will queue while the consumers are stopped.
2. Back up the database and apply the new migration. MySQL initialization mounts
   only run for a new volume; explicitly apply migration 000003 for existing data.
3. Run the backfill from the repository root, supplying a DSN via the environment:

```bash
CONTEST_MYSQL_DSN='.../oj_contest?parseTime=true' \
  go run ./services/contest/cmd/leaderboard-backfill
```

4. Deploy the new Contest Service and restart consumers. Verify each existing
   `(contest_id,user_id)` result has a summary and a pending snapshot event.

Backfill processes users in bounded transactions. It creates version 1 only for
users without a summary; already initialized users are skipped. Interruptions
are safe to rerun. It never modifies submission facts, per-problem scores or
participant records. Run with consumers paused so the initial user inventory
cannot change during the scan. No Redis connection is needed.

For rollback stop consumers and relays, restore the previous application, then
apply the down migration. It discards only derived summaries and cache delivery
state. Re-upgrade requires backfill and cache reconstruction. Never reset an
individual user version while an older cache generation is active.
