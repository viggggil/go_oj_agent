# Problem Service

`problem-service` owns problems, tags, testcase metadata, and MinIO testcase objects.

## Current contract

- Problem create, full update, archive, detail, and paginated summaries.
- One-call testcase upload for paired `.in` and `.out` files.
- Testcase archive instead of physical deletion.
- One testcase metadata listing RPC shared by admin and trusted judge callers.

Search and difficulty/tag filters are deliberately deferred. Ordinary problem
responses never include testcase metadata or object keys.

Each testcase file is limited to 16 MiB by the Proto contract. The gRPC server
accepts 34 MiB messages by default so the paired files and request metadata fit
in one request. The future Gateway endpoint will receive multipart form files
and bridge them to this internal RPC.

The current implementation is the runnable Kratos/gRPC skeleton. Business,
MySQL, Redis, and MinIO implementations will be added as tested vertical slices.

```bash
go run ./services/problem/cmd/problem-service -conf services/problem/configs/config.yaml
```
