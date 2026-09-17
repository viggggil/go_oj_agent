# Judge Service

`judge-service` owns submissions, judge state, transactional Outbox dispatch,
result consumption, retry convergence, and result invalidation.

The initial runtime starts one process. The gRPC API, Outbox Relay, and result
consumer remain separate internal modules but are not separate deployments.
This bootstrap registers the gRPC contract only; persistence, MinIO source
upload, RabbitMQ Relay/Retry/DLQ, and result consumers are implemented in later
vertical slices.

One `submission_id` identifies one logical judge run. Infrastructure retries
reuse that ID. An administrator rejudge invalidates the old submission and
creates a new submission for the same user with the current immutable
`judge_revision`.

```bash
go run ./services/judge/cmd/judge-service -conf services/judge/configs/config.yaml
```
