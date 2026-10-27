#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compose_file="${repo_root}/deploy/compose/compose.yaml"
observability_file="${repo_root}/deploy/compose/observability.yaml"
prometheus_config="${repo_root}/deploy/compose/prometheus.yaml"
rules_file="${repo_root}/deploy/compose/prometheus-rules/observability.yaml"
for file in "${compose_file}" "${observability_file}" "${prometheus_config}" "${rules_file}"; do
  test -f "${file}" || { echo "missing observability file: ${file}" >&2; exit 1; }
done
python3 - "${repo_root}" <<'PY'
import json
import pathlib
import sys
try:
    import yaml
except ImportError as exc:
    raise SystemExit("PyYAML is required (python3 -m pip install PyYAML)") from exc
root = pathlib.Path(sys.argv[1])
for relative in (
    "deploy/compose/compose.yaml",
    "deploy/compose/observability.yaml",
    "deploy/compose/prometheus.yaml",
    "deploy/compose/prometheus-rules/observability.yaml",
    "deploy/compose/otel-collector.yaml",
    "deploy/compose/grafana/provisioning/datasources/datasources.yaml",
    "deploy/compose/grafana/provisioning/dashboards/dashboards.yaml",
):
    path = root / relative
    with path.open(encoding="utf-8") as handle:
        yaml.safe_load(handle)
print("validated observability YAML files")
dashboards = sorted((root / "deploy/compose/grafana/dashboards").glob("*.json"))
if len(dashboards) < 4:
    raise SystemExit("expected at least four Grafana dashboards")
for path in dashboards:
    with path.open(encoding="utf-8") as handle:
        value = json.load(handle)
    if not value.get("uid") or not value.get("panels"):
        raise SystemExit(f"dashboard missing uid/panels: {path}")
print(f"validated {len(dashboards)} Grafana dashboards")
PY
docker_available=false
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  docker_available=true
fi
if command -v promtool >/dev/null 2>&1; then
  promtool check config "${prometheus_config}"
  promtool check rules "${rules_file}"
elif [[ "${docker_available}" == true ]]; then
  docker run --rm --entrypoint promtool \
    -v "${prometheus_config}:/etc/prometheus/prometheus.yml:ro" \
    -v "${repo_root}/deploy/compose/prometheus-rules:/etc/prometheus/rules:ro" \
    prom/prometheus:v3.8.0 check config /etc/prometheus/prometheus.yml
  docker run --rm --entrypoint promtool \
    -v "${rules_file}:/etc/prometheus/rules/observability.yaml:ro" \
    prom/prometheus:v3.8.0 check rules /etc/prometheus/rules/observability.yaml
else
  echo "promtool and docker are unavailable; Prometheus syntax check skipped" >&2
fi
if [[ "${docker_available}" == true ]]; then
  docker compose -f "${compose_file}" -f "${observability_file}" config --quiet
else
  echo "docker is unavailable; Compose config check skipped" >&2
fi
echo "Observability configuration validation completed"
