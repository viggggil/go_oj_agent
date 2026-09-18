# Problem Service

`problem-service` owns problems, tags, testcase metadata, and MinIO testcase objects.

## Current contract

- Problem create, full update, archive, detail, and paginated summaries.
- One-call testcase upload for paired `.in` and `.out` files.
- Testcase archive instead of physical deletion.
- One testcase metadata listing RPC shared by admin and trusted judge callers.
- An internal Judge Profile RPC exposing the active immutable judge revision.

Search and difficulty/tag filters are deliberately deferred. Ordinary problem
responses never include testcase metadata or object keys.

Each testcase file is limited to 16 MiB and must be named from its positive
case number, for example `1.in` and `1.out` for `case_no=1`. The gRPC server
accepts 34 MiB messages so the paired files and request metadata fit in one
request. The Gateway exposes the multipart bridge at
`POST /api/v1/problems/{problem_id}/testcases/upload` with form fields
`case_no`, `input`, and `output`.

MySQL is the source of truth for problem, tag, and testcase metadata. MinIO
stores testcase contents. Redis caches problem details for ten minutes; cache
failure falls back to MySQL, while update and archive invalidate the key.

Every testcase add/archive publishes a complete snapshot under
`problem-{problem_id}/judge-revisions/{26-char-ulid}/` before MySQL atomically
commits the latest testcase state and switches `problems.active_judge_revision`.
Revision manifests and historical testcase files live only in MinIO; MySQL does
not maintain a revision history table. Published revision objects are never
overwritten or synchronously deleted.

`GetJudgeProfile` is restricted to an RS256-authenticated `judge-service`
principal. Archived problems, empty active testcase sets, and problems without
a published revision are rejected.

```bash
go run ./services/problem/cmd/problem-service -conf services/problem/configs/config.yaml
```

The development Compose stack creates the `oj_problem` schema, applies all
problem migrations, creates the `problem-data` bucket, and starts this service
on gRPC port `9002`.
