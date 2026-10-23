from datetime import datetime, timedelta

from app.storage.repository import AgentStore


def test_admin_run_projection_excludes_private_runtime_payload() -> None:
    started = datetime(2026, 1, 1, 12, 0, 0)
    result = AgentStore._safe_run(
        {
            "id": "run-1",
            "trace_id": "a" * 32,
            "user_id": 7,
            "status": "FAILED",
            "started_at": started,
            "finished_at": started + timedelta(milliseconds=125),
            "error_code": "AGENT_RUN_FAILED",
            "model_summary": {
                "knowledge": [{"ok": True}, {"ok": False}],
                "tool_steps": [{"tool_name": "get_problem", "arguments": "secret"}],
                "prompt": "must not escape",
            },
            "config_snapshot": {"api_key": "secret"},
        }
    )

    assert result == {
        "run_id": "run-1",
        "trace_id": "a" * 32,
        "status": "FAILED",
        "purpose": "chat",
        "started_at": started,
        "finished_at": started + timedelta(milliseconds=125),
        "duration_ms": 125,
        "error_code": "AGENT_RUN_FAILED",
        "langsmith_export_status": None,
        "langsmith_project_id": None,
        "tool_step_count": 1,
        "knowledge_query_count": 2,
    }
    assert "config_snapshot" not in result
    assert "prompt" not in result
