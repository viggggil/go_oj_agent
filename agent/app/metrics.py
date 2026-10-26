"""Shared low-cardinality metric attribute policy for the Agent service."""

from __future__ import annotations

import re
from collections.abc import Mapping

_ALLOWED_LABELS = frozenset(
    {
        "service",
        "environment",
        "route",
        "rpc",
        "status",
        "outcome",
        "verdict",
        "language",
        "purpose",
        "model",
        "tool",
        "error_code",
        "usage_source",
        "phase",
    }
)
_VALUE_PATTERN = re.compile(r"^[a-z0-9/_:.+{}-][a-z0-9._:/+{}-]{0,63}$")
_UUID_PATTERN = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")
_NUMERIC_PATH_ID = re.compile(r"(?:^|/)[0-9]+(?:/|$)")
_ULID_PATTERN = re.compile(r"^[0-9a-hjkmnp-tv-z]{26}$")
_CONTROLLED_VALUES: dict[str, frozenset[str]] = {
    "service": frozenset(
        {
            "agent-rag-worker",
            "agent-service",
            "contest-service",
            "gateway-service",
            "judge-service",
            "judge-worker",
            "problem-service",
            "other",
        }
    ),
    "environment": frozenset({"development", "production", "staging", "test", "other"}),
    "route": frozenset(
        {
            "/healthz",
            "/api/v1/agent/chat",
            "/api/v1/users/me",
            "/api/v1/users/{id}",
            "/api/v1/problems",
            "/api/v1/problems/{problem_id}",
            "/api/v1/problems/{problem_id}/testcases",
            "/api/v1/problems/{problem_id}/testcases/{testcase_id}",
            "/api/v1/contests",
            "/api/v1/contests/{contest_id}",
            "/api/v1/contests/{contest_id}/leaderboard",
            "/api/v1/contests/{contest_id}/join",
            "/api/v1/contests/{contest_id}/problems/{problem_id}/submissions",
            "/api/v1/submissions",
            "/api/v1/submissions/{submission_id}",
            "/api/v1/submissions/{submission_id}/source",
            "/api/v1/submissions/{submission_id}/result",
            "/api/v1/submissions/{submission_id}/rejudge",
            "/api/v1/auth/register",
            "/api/v1/auth/login",
            "/api/v1/auth/refresh",
            "/api/v1/auth/logout",
            "other",
        }
    ),
    "rpc": frozenset(
        {
            "gateway.v1.gatewayservice/createsubmission",
            "judge.v1.judgeservice/createsubmission",
            "problem.v1.problemservice/getproblem",
            "contest.v1.contestservice/getleaderboard",
            "other",
        }
    ),
    "status": frozenset(
        {
            "completed",
            "failed",
            "cancelled",
            "timeout",
            "retrying",
            "running",
            "success",
            "error",
            "other",
        }
    ),
    "outcome": frozenset(
        {"success", "error", "timeout", "cancelled", "retrying", "scheduled", "dropped", "other"}
    ),
    "verdict": frozenset(
        {"ac", "wa", "ce", "tle", "mle", "re", "system_error", "unspecified", "other"}
    ),
    "language": frozenset({"c", "cpp", "go", "java", "python", "rust", "other"}),
    "purpose": frozenset({"chat", "memory", "eval", "review", "rag_ingestion", "other"}),
    "model": frozenset({"fake", "deepseek-v4-flash", "deepseek-v4-pro", "other"}),
    "tool": frozenset(
        {
            "get_problem",
            "list_problems",
            "get_submission",
            "get_submission_source",
            "get_judge_result",
            "list_submissions",
            "run_code",
            "other",
        }
    ),
    "error_code": frozenset(
        {
            "agent_model_timeout",
            "agent_model_unavailable",
            "agent_model_rate_limit",
            "agent_model_rejected",
            "agent_model_auth_failed",
            "agent_model_quota",
            "agent_model_blocked",
            "agent_model_invalid_response",
            "agent_model_output_limit",
            "agent_model_input_limit",
            "agent_model_call_limit",
            "agent_model_unsupported_output",
            "agent_credential_unavailable",
            "agent_run_failed",
            "agent_background_failed",
            "invalid_message",
            "publish_failed",
            "otel_export_failed",
            "otel_init_failed",
            "otel_shutdown_failed",
            "collector_unavailable",
            "queue_full",
            "other",
        }
    ),
    "usage_source": frozenset({"provider", "estimated", "other"}),
    "phase": frozenset({"compile", "execute", "result", "outbox", "other"}),
}


def _normalize_value(value: object) -> str:
    if not isinstance(value, str):
        return "other"
    normalized = value.strip().lower()
    if (
        not normalized
        or len(normalized) > 64
        or normalized.isdigit()
        or _UUID_PATTERN.search(normalized)
        or _NUMERIC_PATH_ID.search(normalized)
        or _ULID_PATTERN.fullmatch(normalized)
        or not _VALUE_PATTERN.fullmatch(normalized)
    ):
        return "other"
    return normalized


def safe_metric_attributes(
    values: Mapping[str, object] | None = None, **extra: object
) -> dict[str, str]:
    """Return only bounded, allowlisted metric attributes.

    Request IDs, run IDs, source text and arbitrary provider/tool values are
    intentionally excluded by the key allowlist. Invalid values collapse to
    ``other`` instead of creating a new Prometheus series.
    """

    merged = dict(values or {})
    merged.update(extra)
    result: dict[str, str] = {}
    for key, value in merged.items():
        normalized_key = key.strip().lower() if isinstance(key, str) else ""
        if normalized_key in _ALLOWED_LABELS:
            normalized_value = _normalize_value(value)
            if normalized_value not in _CONTROLLED_VALUES[normalized_key]:
                normalized_value = "other"
            result[normalized_key] = normalized_value
    return dict(sorted(result.items()))
