from app.metrics import safe_metric_attributes


def test_safe_metric_attributes_allowlist_and_normalization() -> None:
    result = safe_metric_attributes(
        {
            "status": " COMPLETED ",
            "route": "/api/v1/agent/chat",
            "rpc": "/api/v1/submissions/7",
            "submission_id": "42",
            "tool": "550e8400-e29b-41d4-a716-446655440000",
            "model": "7",
            "purpose": "0190f7b2-7b3e-7c4b-8d59-6a8e6f2b9d10",
            "error_code": "AGENT_MODEL_TIMEOUT",
        }
    )

    assert result == {
        "error_code": "agent_model_timeout",
        "route": "/api/v1/agent/chat",
        "rpc": "other",
        "status": "completed",
        "tool": "other",
        "model": "other",
        "purpose": "other",
    }
    assert "submission_id" not in result


def test_safe_metric_attributes_rejects_arbitrary_keys_and_values() -> None:
    result = safe_metric_attributes(secret_marker="must-not-escape", model="gpt 4o")

    assert result == {"model": "other"}


def test_safe_metric_attributes_collapses_dynamic_values() -> None:
    result = safe_metric_attributes(
        route="/api/v1/users/alice",
        model="tenant-acme",
        tool="01HZX5YJ4VEXAMPLE0000000000",
        error_code="PROVIDER_SECRET_123",
    )

    assert result == {
        "error_code": "other",
        "model": "other",
        "route": "other",
        "tool": "other",
    }
