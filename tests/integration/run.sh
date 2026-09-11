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
}

trap cleanup EXIT

cleanup

export MYSQL_PORT="${AUTH_TEST_MYSQL_PORT:-13306}"
export REDIS_PORT="${AUTH_TEST_REDIS_PORT:-16379}"
export USER_GRPC_PORT="${AUTH_TEST_USER_GRPC_PORT:-19001}"
export GATEWAY_HTTP_PORT="${AUTH_TEST_GATEWAY_HTTP_PORT:-18080}"
export ACCESS_TOKEN_TTL="2s"
export REFRESH_TOKEN_TTL="5m"
export AUTH_ACCESS_TOKEN_KEY="integration-test-access-token-key"

compose up --build --detach --wait

cd "${repository_root}"
AUTH_INTEGRATION_BASE_URL="http://127.0.0.1:${GATEWAY_HTTP_PORT}" \
  go test -count=1 -v ./tests/integration
