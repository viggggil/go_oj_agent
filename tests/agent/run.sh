#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose_file="${repository_root}/tests/agent/compose.yaml"
project_name="oj-agent-pr1-${RANDOM}-$$"
uv_command="${UV:-uv}"

compose() {
  docker compose --project-name "${project_name}" -f "${compose_file}" "$@"
}

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

cleanup() {
  local status=$?
  trap - EXIT
  if [[ ${status} -ne 0 ]]; then
    echo "===== Agent integration compose logs ====="
    compose logs --no-color || true
  fi
  compose down --volumes --remove-orphans
  exit "${status}"
}

trap cleanup EXIT

export AGENT_TEST_MYSQL_PORT="${AGENT_TEST_MYSQL_PORT:-$(pick_free_port)}"
export AGENT_TEST_HTTP_PORT="${AGENT_TEST_HTTP_PORT:-$(pick_free_port)}"

compose down --volumes --remove-orphans
compose up --build --detach --wait

cd "${repository_root}"
AGENT_TEST_DATABASE_URL="mysql+asyncmy://agent:agent-password@127.0.0.1:${AGENT_TEST_MYSQL_PORT}/oj_agent" \
AGENT_TEST_BAD_DATABASE_URL="mysql+asyncmy://agent:wrong-password@127.0.0.1:${AGENT_TEST_MYSQL_PORT}/oj_agent" \
AGENT_TEST_BASE_URL="http://127.0.0.1:${AGENT_TEST_HTTP_PORT}" \
  "${uv_command}" run --directory agent --frozen pytest -m integration
