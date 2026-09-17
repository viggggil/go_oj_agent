# ADR-0001：合并 Submission 与 Judge Scheduler

- 状态：Accepted
- 日期：2026-09-17

## 背景

原设计把提交持久化、Outbox、调度和执行拆为 `submission-service`、`judge-scheduler` 与 `judge-worker`。独立 Scheduler 只负责语言路由、优先级、重试路由和任务规范化，会增加一次消息消费/发布、一个部署单元和一组故障恢复状态，但当前规模尚不足以证明这层服务边界的收益。

## 决策

1. 删除独立 `judge-scheduler`。
2. 将 `submission-service` 重命名为 `judge-service`。
3. `judge-service` 拥有 `oj_submission` Schema，以及 Submission、Case Result、Outbox 和消费幂等数据。
4. 创建提交时，在同一个 MySQL 事务中写入 Submission 与 `judge.requested` Outbox 意图。
5. `judge-service` 的 Outbox Relay 读取意图，完成任务规范化和语言路由，直接发布到 `judge.task.<language>`。
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

首版只启动一个 `judge-service` 实例，在同一进程内运行 API Server、Outbox Relay 和 Result Consumer。三者仍保持独立模块、独立生命周期和有界并发，便于后续按运行角色拆分；当前不增加额外部署单元。

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

## 首版实现决策

1. **Proto 边界。** 服务实现与部署名使用 `judge-service`；Submission 资源 API 保留 `submission.v1`，Worker、内部判题和管理契约使用 `judge.v1`，暂不合并 Proto 包。
2. **运行形态。** 首版仅运行一个 `judge-service` 实例，并在同一进程中启动 API、Relay 和 Result Consumer。每个后台组件必须支持优雅停止，启动失败必须使服务启动失败，不能静默降级。
3. **Outbox 领取。** 使用 MySQL 8 `SELECT ... FOR UPDATE SKIP LOCKED` 在短事务中领取批次，并记录 lease、attempt、next retry time。Publisher Confirm 前不得标记 published；整体仍按至少一次语义设计。
4. **优先级。** 首版所有任务都是普通优先级，不接受客户端提供的 priority，也不创建多级优先队列。未来引入比赛或管理员优先级时另行版本化契约。
5. **Submission 即判题轮次。** 一个 `submission_id` 唯一标识一次逻辑判题，`judge_revision` 直接固定在 Submission 上。基础设施重试复用原 ID；管理员重判把旧 Submission 标记为 `INVALIDATED`，为同一用户创建新 Submission，并通过 `submission.invalidated` 让 Contest 撤销旧结果。不引入 `judge_attempt` 或 parent/root/origin 字段。
6. **测试集快照。** Problem Service 发布 revision 时，把完整测试集写入 MinIO 不可变前缀 `problem-{problem_id}/judge-revisions/{judge_revision}/`，包含 `manifest.json` 及成对的 `testcases/{case_no}.in|out`。所有对象和 hash 完整后才能原子切换题目的 active revision。首版所有已发布 revision 均不覆盖、不物理删除，避免 Problem Service 跨库判断 Judge 引用关系。
7. **源码存储。** 源码正文存入 MinIO 不可变对象，Judge 数据库只保存 `source_object_key`、SHA-256 和大小。RabbitMQ 只传对象引用和 hash，不传源码正文。
8. **结果事务。** `processed_events` 去重、Submission 状态与 Case Result 以及 `submission.judged` Outbox 必须在同一事务内更新。
9. **取消、作废与超时。** Judge Service 是终态所有者。取消、管理员作废或系统超时通过 Submission 条件更新产生终态；Worker 的迟到消息不能改写已终止的 Submission。
10. **SSE 来源。** MySQL 是事实来源，Redis 只作为实时状态视图。结果事务提交后再更新或失效 Redis；SSE 断线重连必须从 MySQL 恢复状态。
