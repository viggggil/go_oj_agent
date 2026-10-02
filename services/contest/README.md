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
