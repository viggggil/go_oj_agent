#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose_file="${repository_root}/deploy/compose/go-judge.yaml"
project_name="go-oj-go-judge-integration"
grpc_port="${GO_JUDGE_TEST_PORT:-15051}"
auth_token="${GO_JUDGE_TEST_TOKEN:-local-development-go-judge-token}"

compose() {
  GO_JUDGE_GRPC_PORT="${grpc_port}" GO_JUDGE_AUTH_TOKEN="${auth_token}" \
    docker compose --project-name "${project_name}" -f "${compose_file}" "$@"
}

cleanup() {
  compose down --volumes --remove-orphans
}

trap cleanup EXIT
cleanup
compose up --build --detach --wait

cd "${repository_root}"
GO_JUDGE_TEST_ENDPOINT="127.0.0.1:${grpc_port}" \
GO_JUDGE_TEST_TOKEN="${auth_token}" \
  go test -count=1 -v ./services/judge-worker/integration

