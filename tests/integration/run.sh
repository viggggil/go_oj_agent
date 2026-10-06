#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose_file="${repository_root}/deploy/compose/compose.yaml"
project_name="go-oj-auth-integration"

compose() {
  docker compose --project-name "${project_name}" -f "${compose_file}" "$@"
}

cleanup() {
	status=$?
	trap - EXIT
	if [[ ${status} -ne 0 ]]; then
		echo "===== integration test failed; dumping service logs ====="
		compose logs --no-color || true
	fi
  compose down --volumes --remove-orphans
  rm -rf -- "${test_artifacts_dir:-}"
  exit "${status}"
}

trap cleanup EXIT

compose down --volumes --remove-orphans

test_artifacts_dir="$(mktemp -d)"

pick_free_port() {
	local port
	while true; do
		port="$(shuf -i 20000-40000 -n 1)"
		if ! (echo >/dev/tcp/127.0.0.1/"${port}") 2>/dev/null; then
			echo "${port}"
			return 0
		fi
	done
}

export MYSQL_PORT="${AUTH_TEST_MYSQL_PORT:-$(pick_free_port)}"
export REDIS_PORT="${AUTH_TEST_REDIS_PORT:-$(pick_free_port)}"
export USER_GRPC_PORT="${AUTH_TEST_USER_GRPC_PORT:-$(pick_free_port)}"
export GATEWAY_HTTP_PORT="${AUTH_TEST_GATEWAY_HTTP_PORT:-$(pick_free_port)}"
export PROBLEM_GRPC_PORT="${PROBLEM_TEST_GRPC_PORT:-$(pick_free_port)}"
export JUDGE_GRPC_PORT="${JUDGE_TEST_GRPC_PORT:-$(pick_free_port)}"
export CONTEST_GRPC_PORT="${CONTEST_TEST_GRPC_PORT:-$(pick_free_port)}"
export MINIO_API_PORT="${PROBLEM_TEST_MINIO_PORT:-$(pick_free_port)}"
export MINIO_CONSOLE_PORT="${PROBLEM_TEST_MINIO_CONSOLE_PORT:-$(pick_free_port)}"
export RABBITMQ_AMQP_PORT="${JUDGE_TEST_RABBITMQ_PORT:-$(pick_free_port)}"
export RABBITMQ_MANAGEMENT_PORT="${JUDGE_TEST_RABBITMQ_MANAGEMENT_PORT:-$(pick_free_port)}"
export WEB_HTTP_PORT="${WEB_TEST_HTTP_PORT:-$(pick_free_port)}"
export RABBITMQ_USER="${RABBITMQ_USER:-judge}"
export RABBITMQ_PASSWORD="${RABBITMQ_PASSWORD:-judge-password}"
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
CONTEST_TEST_MYSQL_DSN="root:${MYSQL_ROOT_PASSWORD:-local-root-password}@tcp(127.0.0.1:${MYSQL_PORT})/oj_contest?parseTime=true" \
CONTEST_TEST_REDIS_ADDR="127.0.0.1:${REDIS_PORT}" \
CONTEST_TEST_GRPC_ENDPOINT="127.0.0.1:${CONTEST_GRPC_PORT}" \
PROBLEM_TEST_REDIS_ADDR="127.0.0.1:${REDIS_PORT}" \
PROBLEM_TEST_MINIO_ENDPOINT="127.0.0.1:${MINIO_API_PORT}" \
JUDGE_TEST_GRPC_ENDPOINT="127.0.0.1:${JUDGE_GRPC_PORT}" \
JUDGE_TEST_GATEWAY_PRIVATE_KEY_FILE="${test_artifacts_dir}/gateway-private.pem" \
JUDGE_TEST_RABBITMQ_URL="amqp://${RABBITMQ_USER}:${RABBITMQ_PASSWORD}@127.0.0.1:${RABBITMQ_AMQP_PORT}/" \
  go test -count=1 -v ./tests/integration ./tests/e2e ./services/contest/internal/server ./services/contest/internal/data ./services/judge/internal/data
