#!/usr/bin/env bash
set -euo pipefail
prometheus_url="${PROMETHEUS_URL:-http://127.0.0.1:9090}"
grafana_url="${GRAFANA_URL:-http://127.0.0.1:3000}"
collector_url="${COLLECTOR_URL:-http://127.0.0.1:13133}"
gateway_url="${GATEWAY_URL:-http://127.0.0.1:8080}"
curl --fail --silent "${collector_url}/" >/dev/null
curl --fail --silent "${prometheus_url}/-/ready" >/dev/null
curl --fail --silent "${grafana_url}/api/health" >/dev/null
curl --fail --silent "${grafana_url}/api/dashboards/uid/oj-system-overview" >/dev/null
gateway_status="$(curl --silent --output /dev/null --write-out '%{http_code}' "${gateway_url}/api/v1/admin/observability/metrics?window=15m")"
case "${gateway_status}" in
  2*|401|403) ;;
  *) echo "Gateway observability endpoint returned unexpected HTTP ${gateway_status}" >&2; exit 1 ;;
esac
query() { curl --fail --silent --get "${prometheus_url}/api/v1/query" --data-urlencode "query=$1"; }
curl --fail --silent "${prometheus_url}/api/v1/rules" | grep -Fq 'oj:judge_queue_p95_seconds:5m'
curl --fail --silent "${prometheus_url}/api/v1/rules" | grep -Fq 'oj:request_rate:sum5m'
query 'up{job="otel-collector"}' | grep -Eq '"status":"success"'
query 'up{job="otel-collector-self"}' | grep -Eq '"status":"success"'
query 'up{job="contest-service"}' | grep -Eq '"status":"success"'
query 'oj:judge_queue_p95_seconds:5m' | grep -Eq '"status":"success"'
query 'oj:request_rate:sum5m' | grep -Eq '"status":"success"'
echo "Observability smoke checks passed"
