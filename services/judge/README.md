# Judge Service

`judge-service` owns submissions, judge state, transactional Outbox dispatch,
result consumption, retry convergence, and result invalidation.

The initial runtime starts one process. The gRPC API, Outbox Relay, and result
consumer remain separate internal modules but are not separate deployments.

The service currently provides the submission domain and state machine, atomic
MySQL submission/Outbox/idempotency repositories, immutable MinIO source
storage, an authenticated Problem Service Judge Profile client, and all five
submission RPCs: `CreateSubmission`, `GetSubmission`, `ListSubmissions`,
`GetJudgeResult`, and `RejudgeSubmission`. RabbitMQ publishing is managed by a
Kratos application server backed by the `internal/message` adapter; the result
consumer applies idempotent completed and failed events to submission state.

One `submission_id` identifies one logical judge run. Infrastructure retries
reuse that ID. An administrator rejudge invalidates the old submission and
creates a new submission for the same user with the current immutable
`judge_revision`.

Source bodies are stored only in the MinIO `submission-source` bucket under an
immutable `sources/{ulid}/source.{ext}` key. MySQL stores the object key,
SHA-256, size, and the active Problem revision pinned when the submission is
created. Rejudge reuses the source object and atomically invalidates the old
submission before creating the replacement.

Submission RPCs accept identity only from the verified internal RS256
principal. Owners can read and list their own submissions; `admin` and
`system_admin` administrators can
read other submissions and select a `user_id` filter. Lists use stable
`created_at DESC, id DESC` ordering and cap `page_size` at 100.

The Outbox Relay claims leased events from MySQL, publishes durable messages
with RabbitMQ Publisher Confirms, and marks events published only after a
positive confirmation. Temporary failures use bounded exponential backoff;
events exceeding the retry limit are marked `DEAD`. The Relay is a Kratos
server managed by the application lifecycle, while RabbitMQ SDK code remains
isolated in `internal/message`.

```bash
go run ./services/judge/cmd/judge-service -conf services/judge/configs/config.yaml
```

比赛提交完成时，在 `processed_events`、Submission/Case Result 的同一事务内写入
version=1 的 `submission.judged` Outbox，包含 contest/user/problem、verdict、submitted_at、
judged_at。普通提交不产生此事件；最终基础设施失败写入 DONE/SYSTEM_ERROR 并发布比赛事件。
Outbox Relay 继续在 Publisher Confirm 后标记 published。比赛重判仍作废旧 ID、创建新 ID，
新 ID 保留原 `created_at` 用于比赛计分，deadline 和 updated_at 使用重判时刻。
`submission.invalidated` 包含比赛 ID 和原提交时间，支持 Contest 在 judged 到达前建立作废记录。

Judge Trace 的 Outbox 上下文由 `migrations/submission/000010_add_outbox_trace_context`
扩展为可空 `traceparent` / `tracestate`。Relay 从持久化上下文建立 publish Span，并在
AMQP Header 和 Envelope 字段中传播新父上下文；Judge Worker 的 retry/result 消息继续
同一 W3C Trace。Judge 结果消费者和 Contest 投影消费者创建各自的消费 Span。历史
Outbox 和无 Trace 的旧消息仍可处理；上下文无效时只会开启新 Trace。Publisher Confirm、
手动 ACK/NACK、事件 ID 和幂等事务语义不变。
# Trace 排障

管理员排查某次提交时，从 Gateway 的 Judge Trace 入口按 `submission.id` 打开 Tempo。Judge Service 不读取 Gateway 或 Contest 数据库，也不把 Trace 缺失当作判题失败；Publisher Confirm、ACK/NACK、事件 ID 和幂等事务语义保持不变。
