"""LangSmith export must be an allowlisted projection, never a Run dump."""

import asyncio
import json
from datetime import UTC, datetime
from typing import Any
from unittest.mock import patch
from uuid import UUID

from app.langsmith_exporter import (
    LangSmithExporter,
    build_run_batch,
    build_safe_run,
    new_exporter,
)
from app.models.provider import (
    KnowledgeAttempt,
    KnowledgeSource,
    ModelAttempt,
    ModelSummary,
    ToolTraceStep,
)

SECRET = "secret-marker-do-not-export"
RUN_ID = UUID("15d1f61c-95c9-4e72-8ea0-dab2413e90aa")
NOW = datetime(2026, 10, 22, tzinfo=UTC)


class CapturingClient:
    def __init__(self) -> None:
        self.create: list[dict[str, Any]] = []

    def batch_ingest_runs(self, *, create: list[dict[str, Any]]) -> None:
        self.create = create


def test_langsmith_batch_contains_only_allowlisted_metadata() -> None:
    summary = ModelSummary(
        attempts=[
            ModelAttempt(
                provider_id=SECRET,
                model_profile_id=SECRET,
                model=SECRET,
                returned_model=SECRET,
                input_tokens=17,
                output_tokens=9,
                usage_source="provider",
                duration_ms=123,
                finish_reason="completed",
            )
        ],
        tool_steps=[
            ToolTraceStep(
                run_id=str(RUN_ID),
                step=1,
                call_id="call_1",
                tool_name="retrieve_knowledge",
                parameter_validation="valid",
                status="succeeded",
                duration_ms=41,
                summary=SECRET,
                evidence_refs=[SECRET],
            )
        ],
        knowledge=[
            KnowledgeAttempt(
                call_id="call_1",
                ok=True,
                sources=[
                    KnowledgeSource(
                        source_id=str(RUN_ID),
                        base_id=str(RUN_ID),
                        document_id=str(RUN_ID),
                        version_id=str(RUN_ID),
                        title=SECRET,
                        section=SECRET,
                        number=1,
                        start=0,
                        end=20,
                        score=0.8,
                    )
                ],
            )
        ],
    )
    safe = build_safe_run(
        run_id=RUN_ID,
        started_at=NOW,
        finished_at=NOW,
        status="COMPLETED",
        error_code=SECRET,
        model_summary=summary,
        trace_id="a" * 32,
        environment="production",
        deployment_version=SECRET,
        config_version=SECRET,
    )
    client = CapturingClient()
    exporter = LangSmithExporter(
        "unit-test-key", environment="production", deployment_version="1.2.3", client=client
    )
    exporter._send((safe,))

    payload = json.dumps(client.create, sort_keys=True, default=str)
    assert SECRET not in payload
    assert len(client.create) == 4  # root, model attempt, tool and aggregate retrieval
    assert client.create[0]["inputs"] == {}
    assert client.create[0]["outputs"] == {}
    assert client.create[0]["extra"]["metadata"]["otel_trace_id"] == "a" * 32
    assert client.create[0]["extra"]["metadata"]["deployment_version"] == "unknown"
    assert client.create[1]["extra"]["metadata"]["model"] == "other"
    assert client.create[1]["extra"]["metadata"]["input_tokens"] == 17
    assert client.create[2]["extra"]["metadata"]["tool"] == "retrieve_knowledge"
    assert client.create[3]["extra"]["metadata"]["source_count"] == 1


def test_run_batch_has_stable_parent_child_relationships() -> None:
    safe = build_safe_run(
        run_id=RUN_ID,
        started_at=NOW,
        finished_at=NOW,
        status="CANCELLED",
        error_code="AGENT_CANCELLED",
        model_summary=ModelSummary(),
        trace_id=None,
        environment="development",
        deployment_version="dev",
        config_version=str(RUN_ID),
    )
    client = CapturingClient()
    batch = build_run_batch(safe, client)
    assert batch[0]["id"] == str(RUN_ID)
    assert batch[0]["error"] == "AGENT_CANCELLED"
    assert batch[0]["inputs"] == batch[0]["outputs"] == {}


def test_batch_is_accepted_by_locked_langsmith_sdk_and_remains_allowlisted() -> None:
    from langsmith import Client

    safe = build_safe_run(
        run_id=RUN_ID,
        started_at=NOW,
        finished_at=NOW,
        status="COMPLETED",
        error_code=None,
        model_summary=ModelSummary(),
        trace_id="b" * 32,
        environment="test",
        deployment_version="1.2.3",
        config_version=None,
    )
    sdk = Client(
        api_key="unit-test-key",
        auto_batch_tracing=False,
        omit_traced_runtime_info=True,
        info={},
    )
    try:
        payloads: list[bytes] = []

        def capture(_client: Any, body: bytes, **_kwargs: Any) -> None:
            payloads.append(body)

        with patch.object(Client, "_post_batch_ingest_runs", capture):
            sdk.batch_ingest_runs(create=build_run_batch(safe, sdk))
        assert len(payloads) == 1
        payload = payloads[0].decode()
        assert SECRET not in payload
        assert "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb" in payload
        parsed = json.loads(payload)
        root = parsed["post"][0]
        assert root["trace_id"] == str(UUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))
        assert root["dotted_order"].endswith(str(RUN_ID))
        assert root["extra"] == {"metadata": {**safe.metadata, "status": "completed"}}
        assert root["inputs"] == root["outputs"] == {}
    finally:
        sdk.close()


def test_configured_project_id_is_used_for_all_exported_runs() -> None:
    project_id = UUID("00000000-0000-0000-0000-000000000001")
    safe = build_safe_run(
        run_id=RUN_ID,
        started_at=NOW,
        status="COMPLETED",
        error_code=None,
        model_summary=None,
        trace_id=None,
        environment="test",
        deployment_version="dev",
        config_version=None,
        project_id=project_id,
    )
    batch = build_run_batch(safe, CapturingClient())
    assert all(run["session_id"] == str(project_id) for run in batch)


def test_exporter_is_disabled_without_explicit_enablement(monkeypatch: Any) -> None:
    monkeypatch.setenv("LANGSMITH_API_KEY", "ignored")
    monkeypatch.setenv("LANGSMITH_TRACING", "true")
    assert new_exporter(False, "ignored", "development") is None


def test_queue_is_bounded() -> None:
    exporter = LangSmithExporter(
        "unit-test-key",
        environment="test",
        deployment_version="dev",
        queue_size=1,
        client=CapturingClient(),
    )
    request: dict[str, Any] = {
        "run_id": RUN_ID,
        "started_at": NOW,
        "status": "COMPLETED",
        "error_code": None,
        "model_summary": None,
        "trace_id": None,
        "config_version": None,
    }
    assert exporter.submit(**request)
    assert not exporter.submit(**request)


async def test_exporter_reports_only_confirmed_send_result() -> None:
    class FailingClient(CapturingClient):
        def batch_ingest_runs(self, *, create: list[dict[str, Any]]) -> None:
            raise RuntimeError(SECRET)

    for client, expected in ((CapturingClient(), True), (FailingClient(), False)):
        exporter = LangSmithExporter(
            "unit-test-key", environment="test", deployment_version="dev", client=client
        )
        results: list[bool] = []
        completed = asyncio.Event()

        async def report(
            value: bool, results: list[bool] = results, completed: Any = completed
        ) -> None:
            results.append(value)
            completed.set()

        exporter.start()
        assert exporter.submit(
            run_id=RUN_ID,
            started_at=NOW,
            status="COMPLETED",
            error_code=None,
            model_summary=None,
            trace_id=None,
            config_version=None,
            on_result=report,
        )
        await asyncio.wait_for(completed.wait(), timeout=2)
        assert results == [expected]
        await exporter.close()
