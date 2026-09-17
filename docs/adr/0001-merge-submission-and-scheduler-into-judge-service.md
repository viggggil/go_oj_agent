# ADR-0001：合并 Submission 与 Judge Scheduler

- 状态：Accepted（总体服务边界）；部分实现细节待决策
- 日期：2026-09-17

## 背景

原设计把提交持久化、Outbox、调度和执行拆为 `submission-service`、`judge-scheduler` 与 `judge-worker`。独立 Scheduler 只负责语言路由、优先级、重试路由和任务规范化，会增加一次消息消费/发布、一个部署单元和一组故障恢复状态，但当前规模尚不足以证明这层服务边界的收益。

## 决策

1. 删除独立 `judge-scheduler`。
2. 将 `submission-service` 重命名为 `judge-service`。
3. `judge-service` 拥有 `oj_submission` Schema，以及 Submission、Case Result、Outbox 和消费幂等数据。
4. 创建提交时，在同一个 MySQL 事务中写入 Submission 与 `judge.requested` Outbox 意图。
5. `judge-service` 的 Outbox Relay 读取意图，完成任务规范化、语言/优先级路由，直接发布到 `judge.task.<language>`。
6. 收到 RabbitMQ Publisher Confirm 后，Relay 才把 Outbox 标记为已发布。
7. `judge-worker` 保持独立部署，只消费任务、执行不可信代码并发布 `judge.completed` 或 `judge.failed`。
8. `judge-service` 幂等消费结果并更新 Submission，仍是提交状态和结果的唯一事实来源。

## 目标链路

```text
Client
  -> Gateway
  -> judge-service/CreateSubmission
  -> MySQL transaction: submission + outbox intent
  -> judge-service Outbox Relay
  -> RabbitMQ judge.task.<language>
  -> judge-worker
  -> RabbitMQ judge.completed / judge.failed
  -> judge-service Result Consumer
  -> MySQL submission + case results
```

Outbox Relay 可以与 API Server 使用同一二进制的不同启动参数，也可以先以内嵌后台组件运行。无论部署方式如何，它都属于 `judge-service`，共享同一数据所有权与发布语义，不形成新的业务服务。

## 不变量

- Submission 与 Outbox Intent 必须同事务提交。
- RabbitMQ 使用 at-least-once delivery，Relay 和所有 Consumer 必须幂等。
- Worker 只在结果发布收到 Publisher Confirm 后 ACK 原任务。
- Judge Service 只有在结果持久化成功后 ACK 结果消息。
- 重试必须有上限，不可恢复消息进入 DLQ。
- Judge Service 不执行用户代码；Worker 不直接写 Judge Service 数据库。
- 任务消息不能依赖数据库事务尚未提交的数据。

## 后果

收益：

- 减少一次 `judge.requested -> scheduler -> judge.task.*` 消息跳转。
- 少维护一个服务、部署单元、消费幂等表和故障恢复状态机。
- 调度决策与 Submission/Outbox 数据处于同一所有权边界。
- 创建提交到任务发布的可观测链路更短。

代价：

- `judge-service` 同时承担同步 API、Outbox 发布和结果消费，需要清晰的运行角色和资源隔离。
- 调度策略扩展会增加 Judge Service 内部复杂度。
- 如果未来需要跨集群、全局公平调度或独立容量规划，可以重新提取 Scheduler，但必须以实际需求和监控数据为依据。

## 待决策

以下问题不阻塞本次架构文档更新，但必须在 Judge 链路编码前或对应功能 PR 中敲定：

1. **Proto 包和外部路径是否同步改名。** 推荐服务实现与部署名使用 `judge-service`；现有 `submission.v1` 业务 API 可以保留，避免把 Submission 资源错误改名为 Judge。`api/judge/v1` 继续用于 Worker 管理接口和内部判题模型。是否合并两个 Proto 包需要单独做兼容性评审。
2. **Relay 的首版运行形态。** 推荐同一仓库和二进制提供 `api`、`relay`、`result-consumer` 三种 role，Compose 可以分别启动；不建议 API 进程内无条件启动 Relay，否则 API 横向扩容会隐式增加 Relay 并发。
3. **Outbox 并发领取策略。** 推荐 MySQL 8 使用短事务 `SELECT ... FOR UPDATE SKIP LOCKED` 领取批次，并设置 lease/attempt；需要确定是否允许同一事件被多个 Relay 重复发布。即使使用 lease，也必须按至少一次语义设计。
4. **优先级模型。** 尚未确定普通提交、比赛提交、重判和管理员任务的优先级来源及防饥饿规则。首版可以只有普通优先级，但字段和 routing policy 不应依赖客户端自报。
5. **重判模型。** 需要确定重判复用 `submission_id` 还是创建 `judge_attempt`。推荐引入 attempt/version，使迟到的旧结果不能覆盖新一轮结果。
6. **测试用例快照。** 当前 Problem Testcase 已取消 version 字段，需要确定任务创建后测试点变化时的可重复判题策略。推荐 Judge Task 固化一组不可变 testcase object keys 或 manifest ID，而不是 Worker 在执行时读取“当前所有测试点”。
7. **源码存储。** 需要确定源代码正文直接存 MySQL、存 MinIO 仅留 object key，或按大小分层。MQ 中不应携带大段源码。
8. **结果消费事务。** 推荐 `processed_events` 去重、Submission 状态迁移、Case Result 写入和 `submission.judged` Outbox 事件在同一个事务内完成。
9. **取消与超时。** 需要定义用户取消、系统超时、Worker 心跳丢失后由谁产生终态，以及迟到结果如何处理。
10. **SSE 更新来源。** 推荐 MySQL 为事实来源，Redis 只做实时视图；结果事务提交后再更新/失效 Redis，SSE 断线重连必须能回查 MySQL。
