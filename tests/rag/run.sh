#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
project="oj-rag-test-${RANDOM}-$$"
uv_command="${UV:-uv}"
port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()'; }
export RAG_MYSQL_PORT="$(port)" RAG_MINIO_PORT="$(port)" RAG_QDRANT_PORT="$(port)" RAG_EMBEDDING_PORT="$(port)"
compose() { docker compose -p "$project" -f "$root/tests/rag/compose.yaml" "$@"; }
cleanup() {
  result=$?
  trap - EXIT
  if [ "$result" != 0 ]; then compose logs --tail=80 --no-color; fi
  compose down --volumes --remove-orphans
  exit "$result"
}
trap cleanup EXIT
compose up --build --detach --wait
AGENT_TEST_RAG_DATABASE_URL="mysql+asyncmy://agent:agent-password@127.0.0.1:$RAG_MYSQL_PORT/oj_agent" \
AGENT_TEST_RAG_MINIO_ENDPOINT="127.0.0.1:$RAG_MINIO_PORT" \
AGENT_TEST_RAG_QDRANT_URL="http://127.0.0.1:$RAG_QDRANT_PORT" \
AGENT_TEST_RAG_EMBEDDING_URL="http://127.0.0.1:$RAG_EMBEDDING_PORT" \
"$uv_command" run --directory "$root/agent" --frozen pytest tests/integration/test_rag.py -q
# 在隔离测试库验证破坏性 down 与重新初始化；不接触开发数据。
compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot' < "$root/migrations/agent/000004_create_knowledge.down.sql"
compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot' < "$root/migrations/agent/000004_create_knowledge.up.sql"
python3 "$root/scripts/build_algorithm_corpus.py"
