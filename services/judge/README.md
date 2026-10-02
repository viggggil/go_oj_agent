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
principal. Owners can read and list their own submissions; administrators can
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
