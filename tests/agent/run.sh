#!/usr/bin/env bash

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose_file="${repository_root}/tests/agent/compose.yaml"
project_name="oj-agent-integration-${RANDOM}-$$"
uv_command="${UV:-uv}"
test_key_dir="$(mktemp -d)"
export AGENT_TEST_KEY_DIR="${test_key_dir}"

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
  rm -rf -- "${test_key_dir}"
  exit "${status}"
}

trap cleanup EXIT

export AGENT_TEST_MYSQL_PORT="${AGENT_TEST_MYSQL_PORT:-$(pick_free_port)}"
export AGENT_TEST_HTTP_PORT="${AGENT_TEST_HTTP_PORT:-$(pick_free_port)}"
export AGENT_TEST_GATEWAY_PORT="${AGENT_TEST_GATEWAY_PORT:-$(pick_free_port)}"

"${uv_command}" run --directory "${repository_root}/agent" --frozen \
  python "${repository_root}/tests/agent/generate_keys.py" "${test_key_dir}"

compose down --volumes --remove-orphans
compose up --build --detach --wait

cd "${repository_root}"
AGENT_TEST_DATABASE_URL="mysql+asyncmy://agent:agent-password@127.0.0.1:${AGENT_TEST_MYSQL_PORT}/oj_agent" \
AGENT_TEST_BAD_DATABASE_URL="mysql+asyncmy://agent:wrong-password@127.0.0.1:${AGENT_TEST_MYSQL_PORT}/oj_agent" \
AGENT_TEST_BASE_URL="http://127.0.0.1:${AGENT_TEST_HTTP_PORT}" \
AGENT_TEST_GATEWAY_URL="http://127.0.0.1:${AGENT_TEST_GATEWAY_PORT}" \
AGENT_TEST_USER_PRIVATE_KEY_FILE="${test_key_dir}/user-private.pem" \
  "${uv_command}" run --directory agent --frozen pytest -m integration
