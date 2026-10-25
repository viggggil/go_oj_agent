# Issue #200 PR3：结果回写到 Contest 的异步 Trace

PR3 把 PR2 产生的 `judge.completed` / `judge.failed` Trace 延续到 Judge Result
Consumer、Submission 事务、`submission.judged` Outbox 和 Contest 投影。Contest
排行榜缓存还有自己的本地 Cache Outbox，因此投影事务会把同一条 W3C 上下文保存到
缓存 Outbox，缓存 Relay 领取后再恢复并创建独立的应用 Span。

## 传播路径

```text
judge.worker.task
  └─ judge.worker.result.publish
      └─ judge.result.consume
          └─ judge.result.transaction
              └─ submission.judged Outbox
                  └─ contest.result.consume
                      └─ contest.projection.apply
                          └─ contest.projection.transaction
                              └─ contest.cache_outbox.apply
```

每次 RabbitMQ delivery、结果发布、投影事务和缓存 Outbox 实际处理都有自己的 Span。
`event_id`、`causation_id`、`submission_id`、`contest_id`、`judge_revision` 和
`result_version` 只作为 Trace 属性，不作为 Prometheus 标签。Trace 元数据不包含
源码、测试输入输出、Provider 错误原文或用户请求正文。

## 持久化和兼容性

- Judge Result Consumer 使用消息上下文执行结果事务；事务写入的
  `submission.judged` Outbox 会保存当前 Trace Context。旧消息没有上下文时仍能正常
  处理，只会从新的本地 Trace 开始。
- Contest `contest_cache_outbox` 新增可空 `traceparent` / `tracestate`，迁移为
  `migrations/contest/000005_add_cache_outbox_trace_context`。历史行保持 NULL。
- Contest 投影仍在原有 MySQL 事务中完成 processed-event 去重、事实更新、榜单重算和
  Cache Outbox 写入；只有事务成功后消费者才 ACK。
- Cache Relay 的 Trace 失败、Collector 不可用、Redis 失败或租约丢失不会改变事实表、
  重试退避、租约 fencing 或缓存 Outbox 的状态机。

## 验证

关键单测覆盖 Projection Envelope 恢复 Trace Context、Worker 结果/重试发布 Span 和
Judge Result 事务 Span。Contest MySQL 集成用例额外验证 Trace Context 与榜单快照在同一
投影事务保存；现有重复、乱序、作废、回放、租约过期和 Redis 失败用例继续覆盖业务
收敛语义。

CI 及真实环境验证应继续使用 `make test-integration`，并在宿主机已有 Qdrant 占用
6333 时通过 `QDRANT_PORT` 指定临时端口。
