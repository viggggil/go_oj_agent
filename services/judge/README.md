# Judge Service

`judge-service` owns submissions, judge state, transactional Outbox dispatch,
result consumption, retry convergence, and result invalidation.

The initial runtime starts one process. The gRPC API, Outbox Relay, and result
consumer remain separate internal modules but are not separate deployments.

The service currently provides the submission domain and state machine, atomic
MySQL submission/Outbox/idempotency repositories, immutable MinIO source
storage, and an authenticated Problem Service Judge Profile client. The five
Submission RPC handlers, RabbitMQ Relay/Retry/DLQ, and result consumers are
implemented in later vertical slices.

One `submission_id` identifies one logical judge run. Infrastructure retries
reuse that ID. An administrator rejudge invalidates the old submission and
creates a new submission for the same user with the current immutable
`judge_revision`.

Source bodies are stored only in the MinIO `submission-source` bucket under an
immutable `sources/{ulid}/source.{ext}` key. MySQL stores the object key,
SHA-256, size, and the active Problem revision pinned when the submission is
created. Rejudge reuses the source object and atomically invalidates the old
submission before creating the replacement.

```bash
go run ./services/judge/cmd/judge-service -conf services/judge/configs/config.yaml
```
