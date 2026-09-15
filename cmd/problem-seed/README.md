# Problem Seed

Imports the initial original problem catalog through Gateway APIs. It never
connects to MySQL, Redis, or MinIO.

```bash
PROBLEM_SEED_ADMIN_ACCOUNT=admin \
PROBLEM_SEED_ADMIN_PASSWORD='...' \
go run ./cmd/problem-seed
```

`PROBLEM_SEED_BASE_URL` defaults to `http://127.0.0.1:8080`. The command is
idempotent by slug: existing problems are skipped. Every new problem is first
created through `POST /api/v1/problems`, then its numbered testcase pairs are
uploaded through the multipart Gateway endpoint.
