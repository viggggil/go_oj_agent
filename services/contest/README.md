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
时间计算。排行榜和 `submission.judged` consumer 尚未实现；排行榜请求返回
`UNIMPLEMENTED`，不会伪造数据。比赛提交会在比赛服务完成报名、时间和题目归属校验
后转发到 judge-service。用户通过 `JoinContest` 或 Gateway 的
`POST /api/v1/contests/{contest_id}/join` 报名；报名仅允许在比赛开始前，重复报名幂等。

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
