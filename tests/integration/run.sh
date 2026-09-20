#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose_file="${repository_root}/deploy/compose/compose.yaml"
project_name="go-oj-auth-integration"

compose() {
  docker compose --project-name "${project_name}" -f "${compose_file}" "$@"
}

cleanup() {
  compose down --volumes --remove-orphans
  rm -rf -- "${test_artifacts_dir:-}"
}

trap cleanup EXIT

cleanup

test_artifacts_dir="$(mktemp -d)"

export MYSQL_PORT="${AUTH_TEST_MYSQL_PORT:-13306}"
export REDIS_PORT="${AUTH_TEST_REDIS_PORT:-16379}"
export USER_GRPC_PORT="${AUTH_TEST_USER_GRPC_PORT:-19001}"
export GATEWAY_HTTP_PORT="${AUTH_TEST_GATEWAY_HTTP_PORT:-18080}"
export PROBLEM_GRPC_PORT="${PROBLEM_TEST_GRPC_PORT:-19002}"
export JUDGE_GRPC_PORT="${JUDGE_TEST_GRPC_PORT:-19004}"
export MINIO_API_PORT="${PROBLEM_TEST_MINIO_PORT:-19000}"
export MINIO_CONSOLE_PORT="${PROBLEM_TEST_MINIO_CONSOLE_PORT:-19003}"
export ACCESS_TOKEN_TTL="2s"
export REFRESH_TOKEN_TTL="5m"
export AUTH_ACCESS_TOKEN_KEY="integration-test-access-token-key"

if ! compose up --build --detach --wait; then
  echo "===== integration compose up failed; dumping service logs ====="
  compose logs --no-color || true
  exit 1
fi
compose cp gateway-service:/run/auth-keys/gateway-private.pem "${test_artifacts_dir}/gateway-private.pem"

cd "${repository_root}"
AUTH_INTEGRATION_BASE_URL="http://127.0.0.1:${GATEWAY_HTTP_PORT}" \
PROBLEM_TEST_MYSQL_DSN="root:${MYSQL_ROOT_PASSWORD:-local-root-password}@tcp(127.0.0.1:${MYSQL_PORT})/oj_problem?parseTime=true" \
PROBLEM_TEST_USER_MYSQL_DSN="root:${MYSQL_ROOT_PASSWORD:-local-root-password}@tcp(127.0.0.1:${MYSQL_PORT})/oj_user?parseTime=true" \
SUBMISSION_TEST_MYSQL_DSN="root:${MYSQL_ROOT_PASSWORD:-local-root-password}@tcp(127.0.0.1:${MYSQL_PORT})/oj_submission?parseTime=true" \
PROBLEM_TEST_REDIS_ADDR="127.0.0.1:${REDIS_PORT}" \
PROBLEM_TEST_MINIO_ENDPOINT="127.0.0.1:${MINIO_API_PORT}" \
JUDGE_TEST_GRPC_ENDPOINT="127.0.0.1:${JUDGE_GRPC_PORT}" \
JUDGE_TEST_GATEWAY_PRIVATE_KEY_FILE="${test_artifacts_dir}/gateway-private.pem" \
  go test -count=1 -v ./tests/integration
