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

export AGENT_TEST_MINIO_PORT="${AGENT_TEST_MINIO_PORT:-$(pick_free_port)}"
export AGENT_TEST_MYSQL_PORT="${AGENT_TEST_MYSQL_PORT:-$(pick_free_port)}"
export AGENT_TEST_HTTP_PORT="${AGENT_TEST_HTTP_PORT:-$(pick_free_port)}"
export AGENT_TEST_MOCK_PORT="${AGENT_TEST_MOCK_PORT:-$(pick_free_port)}"
export AGENT_TEST_GATEWAY_PORT="${AGENT_TEST_GATEWAY_PORT:-$(pick_free_port)}"

"${uv_command}" run --directory "${repository_root}/agent" --frozen \
  python "${repository_root}/tests/agent/generate_keys.py" "${test_key_dir}"

compose down --volumes --remove-orphans
compose up --build --detach --wait
"${uv_command}" run --directory "${repository_root}/agent" --frozen python - <<'PY_BUCKET'
import os
from minio import Minio
client = Minio("127.0.0.1:" + os.environ["AGENT_TEST_MINIO_PORT"], access_key="minioadmin", secret_key="minioadmin", secure=False)
if not client.bucket_exists("agent-knowledge"):
    client.make_bucket("agent-knowledge")
PY_BUCKET

AGENT_DATABASE_URL="mysql+asyncmy://agent:agent-password@127.0.0.1:${AGENT_TEST_MYSQL_PORT}/oj_agent" \
  "${uv_command}" run --directory "${repository_root}/agent" --frozen \
  python -m app.config_cli bootstrap --file "${repository_root}/agent/examples/learning-assistant.json" \
  --actor-id 7 --request-id integration-bootstrap

cd "${repository_root}"
AGENT_TEST_DATABASE_URL="mysql+asyncmy://agent:agent-password@127.0.0.1:${AGENT_TEST_MYSQL_PORT}/oj_agent" \
AGENT_TEST_BAD_DATABASE_URL="mysql+asyncmy://agent:wrong-password@127.0.0.1:${AGENT_TEST_MYSQL_PORT}/oj_agent" \
AGENT_TEST_BASE_URL="http://127.0.0.1:${AGENT_TEST_HTTP_PORT}" \
AGENT_TEST_GATEWAY_URL="http://127.0.0.1:${AGENT_TEST_GATEWAY_PORT}" \
AGENT_TEST_USER_PRIVATE_KEY_FILE="${test_key_dir}/user-private.pem" \
  "${uv_command}" run --directory agent --frozen pytest -m integration

# 第二阶段：同一服务切换为真实 Runtime，使用隔离 mock Provider（无外网/计费）。
export AGENT_DATABASE_URL="mysql+asyncmy://agent:agent-password@127.0.0.1:${AGENT_TEST_MYSQL_PORT}/oj_agent"
export AGENT_CREDENTIAL_KEYRING_FILE="${test_key_dir}/model-keyring.json"
export AGENT_PROVIDER_ALLOWED_ORIGINS='["http://mock-provider:8001"]'
export AGENT_PROVIDER_ALLOW_PRIVATE_NETWORK=true
printf '%s' 'integration-model-key' > "${test_key_dir}/model.key"
chmod 600 "${test_key_dir}/model.key"
AGENT_ENVIRONMENT=test "${uv_command}" run --directory agent --frozen python -m app.credential_cli create \
  --key-file "${test_key_dir}/model.key" --name mock-provider --actor-id 7 \
  --request-id model-integration-credential > "${test_key_dir}/credential.json"
"${uv_command}" run --directory agent --frozen python - "${test_key_dir}" "${repository_root}/agent/examples/su8-responses.json" <<'PY_CONFIG'
import json, sys
from pathlib import Path
directory = Path(sys.argv[1])
body = json.loads(Path(sys.argv[2]).read_text())
body['provider']['content']['base_url'] = 'http://mock-provider:8001/v1'
body['provider']['content']['credential_id'] = json.loads((directory / 'credential.json').read_text())['id']
(directory / 'model-config.json').write_text(json.dumps(body, ensure_ascii=False))
PY_CONFIG
"${uv_command}" run --directory agent --frozen python -m app.config_cli bootstrap \
  --file "${test_key_dir}/model-config.json" --actor-id 7 --request-id model-integration-bootstrap
# Compose 测试挂载对容器用户可读；部署 bootstrap 仍使用 0600 的独立副本。
cp "${AGENT_CREDENTIAL_KEYRING_FILE}" "${test_key_dir}/bootstrap-keyring.json"
chmod 600 "${test_key_dir}/bootstrap-keyring.json"
AGENT_RUNTIME_MODE=model AGENT_CONFIG_MODE=database AGENT_DEFAULT_AGENT_KEY=learning_assistant \
AGENT_ENVIRONMENT=test AGENT_CREDENTIAL_KEYRING_FILE="${test_key_dir}/bootstrap-keyring.json" \
AGENT_DATABASE_URL="${AGENT_DATABASE_URL}" AGENT_PROVIDER_ALLOWED_ORIGINS='["http://mock-provider:8001"]' \
  "${uv_command}" run --directory agent --frozen python - <<'PY_REVIEWER'
import asyncio
from app.core.settings import load_settings
from app.development_bootstrap import bootstrap_su8
asyncio.run(bootstrap_su8(load_settings()))
PY_REVIEWER
export AGENT_TEST_RUNTIME_MODE=model
# Runtime 阶段切换同时重建 Gateway，清除连向上一容器的连接池；Chat POST 不自动重放。
compose up --detach --wait --force-recreate agent-service gateway-service
AGENT_TEST_DATABASE_URL="${AGENT_DATABASE_URL}" \
AGENT_TEST_GATEWAY_URL="http://127.0.0.1:${AGENT_TEST_GATEWAY_PORT}" \
AGENT_TEST_MOCK_URL="http://127.0.0.1:${AGENT_TEST_MOCK_PORT}" \
AGENT_TEST_MODEL_MODE=true \
AGENT_TEST_USER_PRIVATE_KEY_FILE="${test_key_dir}/user-private.pem" \
  "${uv_command}" run --directory agent --frozen pytest tests/integration/test_model_http.py tests/integration/test_management.py tests/integration/test_eval_runs.py -m integration

# 可选真实浏览器管理链路；仅用户资料 fixture，配置通过 Gateway 写入上述隔离 MySQL。
if [[ "${AGENT_TEST_WEB:-false}" == "true" ]]; then
  AGENT_TEST_GATEWAY_URL="http://127.0.0.1:${AGENT_TEST_GATEWAY_PORT}" \
  AGENT_TEST_MODEL_MODE=true \
  AGENT_TEST_USER_PRIVATE_KEY_FILE="${test_key_dir}/user-private.pem" \
    npm --prefix web run test:e2e -- e2e/agent-live.spec.ts
fi
