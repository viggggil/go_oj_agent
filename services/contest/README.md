# Contest Service

Contest Service 当前完成 API contract 和运行时初始化，提供文档约定的内部 gRPC
接口：

```text
GetContest
ListContests
GetLeaderboard
```

本阶段没有 Contest 数据库、Redis 排行榜或 `submission.judged` consumer。有效请求
经过 protobuf 和分页校验后返回 `UNIMPLEMENTED`，不会返回伪造比赛或排行榜数据。

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
