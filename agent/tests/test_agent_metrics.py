"""验证 Agent 运行和后台任务指标的结果口径与标签边界。"""

import asyncio
from collections.abc import Generator
from hashlib import sha256
from types import SimpleNamespace
from typing import Any, cast

import pytest
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import InMemoryMetricReader

from app import observability
from app.graphs.direct_answer import DirectAnswer
from app.models.provider import ModelFailure
from app.rag.models import REVISION, SPLITTER
from app.rag.worker import process_one
from tests.test_model import Credentials, configured


@pytest.fixture
def metric_reader(
    monkeypatch: pytest.MonkeyPatch,
) -> Generator[InMemoryMetricReader, None, None]:
    reader = InMemoryMetricReader()
    provider = MeterProvider(metric_readers=[reader])
    monkeypatch.setattr(
        observability,
        "_metric_instruments",
        observability._create_metric_instruments(provider.get_meter("test.agent")),
    )
    yield reader
    provider.shutdown()


def _metrics(reader: InMemoryMetricReader) -> dict[str, Any]:
    data = reader.get_metrics_data()
    assert data is not None
    return {
        metric.name: metric
        for resource in data.resource_metrics
        for scope in resource.scope_metrics
        for metric in scope.metrics
    }


@pytest.mark.asyncio
async def test_agent_metrics_cover_model_tool_rag_and_background_outcomes(
    metric_reader: InMemoryMetricReader,
) -> None:
    measurement = observability.model_attempt_started("gpt-4o-secret-marker")
    observability.model_attempt_finished(measurement, "timeout")
    observability.record_model_retry("gpt-4o-secret-marker")

    class FailedToolExecutor:
        @observability.traced_async("agent.tool")
        async def execute(self, name: str, error_code: str) -> SimpleNamespace:
            return SimpleNamespace(ok=False, error_code=error_code)

    executor = FailedToolExecutor()
    await executor.execute("retrieve_knowledge", "TOOL_PERMISSION_DENIED")
    await executor.execute("retrieve_knowledge", "TOOL_TIMEOUT")

    @observability.traced_async("agent.rag.retrieve")
    async def timed_out_retrieval() -> None:
        raise TimeoutError("private retrieval detail")

    with pytest.raises(TimeoutError):
        await timed_out_retrieval()

    async def background_job(purpose: str, status: str) -> None:
        async with observability.job_metrics(purpose):
            await asyncio.sleep(0)
            observability.job_status(status, "PRIVATE_ERROR_MARKER")

    await asyncio.gather(background_job("eval", "failed"), background_job("memory", "completed"))
    observability.record_job_retry("memory")

    data = _metrics(metric_reader)
    model = data["agent.model.attempt"].data.data_points
    assert any(
        point.attributes == {"model": "other", "purpose": "chat", "outcome": "timeout"}
        for point in model
    )
    retries = data["agent.model.retry"].data.data_points
    assert any(
        point.attributes == {"model": "other", "purpose": "chat", "outcome": "retrying"}
        for point in retries
    )
    tools = data["agent.tool.total"].data.data_points
    tool_dimensions = {
        (point.attributes["tool"], point.attributes["purpose"], point.attributes["outcome"])
        for point in tools
    }
    assert ("retrieve_knowledge", "chat", "error") in tool_dimensions
    assert ("retrieve_knowledge", "chat", "timeout") in tool_dimensions
    rag = data["agent.rag.total"].data.data_points
    assert any(point.attributes == {"purpose": "chat", "outcome": "timeout"} for point in rag)
    jobs = data["agent.job.total"].data.data_points
    assert any(point.attributes == {"purpose": "eval", "status": "failed"} for point in jobs)
    assert any(point.attributes == {"purpose": "memory", "status": "completed"} for point in jobs)
    assert any(point.attributes == {"purpose": "memory", "status": "retrying"} for point in jobs)
    serialized = repr(data)
    assert "PRIVATE_ERROR_MARKER" not in serialized
    assert "private retrieval detail" not in serialized
    assert "gpt-4o-secret-marker" not in serialized


def test_metric_sdk_failure_does_not_escape_business_helpers(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    class BrokenInstrument:
        def add(self, *_args: object, **_kwargs: object) -> None:
            raise RuntimeError("collector unavailable")

        def record(self, *_args: object, **_kwargs: object) -> None:
            raise RuntimeError("collector unavailable")

    monkeypatch.setattr(
        observability,
        "_metric_instruments",
        SimpleNamespace(runs_started=BrokenInstrument(), runs_in_flight=BrokenInstrument()),
    )
    observability.run_started()


@pytest.mark.asyncio
async def test_cancelled_job_finishes_in_flight_gauge_at_zero(
    metric_reader: InMemoryMetricReader,
) -> None:
    with pytest.raises(asyncio.CancelledError):
        async with observability.job_metrics("rag_ingestion"):
            raise asyncio.CancelledError

    data = _metrics(metric_reader)
    jobs = data["agent.job.total"].data.data_points
    assert any(
        point.attributes == {"purpose": "rag_ingestion", "status": "cancelled"} for point in jobs
    )
    in_flight = data["agent.job.in_flight"].data.data_points
    assert any(
        point.attributes == {"purpose": "rag_ingestion"} and point.value == 0 for point in in_flight
    )


@pytest.mark.asyncio
async def test_model_retry_metrics_follow_real_provider_attempts(
    metric_reader: InMemoryMetricReader,
) -> None:
    class RetryingModel:
        calls = 0

        async def stream(self, *_args: Any, **_kwargs: Any) -> Any:
            self.calls += 1
            if self.calls == 1:
                raise ModelFailure("AGENT_MODEL_RATE_LIMIT", retryable=True)
            attempt = _args[6]
            attempt.finish_reason = "completed"
            yield "answer"

    state = configured()
    state.config_snapshot["provider_config"]["max_retries"] = 1
    client = RetryingModel()
    direct = DirectAnswer(cast(Any, client), Credentials())
    prepared = direct.prepare(state)
    assert [text async for text in direct.stream(state, prepared)] == ["answer"]
    assert client.calls == 2

    data = _metrics(metric_reader)
    attempts = data["agent.model.attempt"].data.data_points
    assert sum(point.value for point in attempts) == 2
    retries = data["agent.model.retry"].data.data_points
    assert sum(point.value for point in retries) == 1


@pytest.mark.asyncio
async def test_rag_retry_metric_is_one_per_persisted_attempt(
    metric_reader: InMemoryMetricReader,
) -> None:
    content = "private document text"

    class RetryStore:
        attempts = 0
        finishes = 0
        persist_finish = True

        async def claim(self) -> dict[str, Any]:
            self.attempts += 1
            return {
                "id": "version-id",
                "document_id": "document-id",
                "lease_token": f"lease-{self.attempts}",
                "attempts": self.attempts,
                "model_revision": REVISION,
                "splitter_version": SPLITTER,
                "object_key": "private-object-key",
                "content_hash": sha256(content.encode()).hexdigest(),
            }

        async def heartbeat(self, _version_id: str, _token: str) -> bool:
            return True

        async def finish(self, _value: dict[str, Any], _count: int, error: str) -> bool:
            self.finishes += 1
            assert error == "AGENT_RAG_INDEX_FAILED"
            return self.persist_finish

    class FailedIndexClients:
        async def get(self, _key: str) -> str:
            return content

        async def split(self, _text: str) -> list[str]:
            return ["chunk"]

        async def ensure_collection(self) -> None:
            return None

        async def upsert(self, _value: dict[str, Any], _chunks: list[str]) -> None:
            raise RuntimeError("private vector store detail")

    store = RetryStore()
    clients = FailedIndexClients()
    assert await process_one(cast(Any, store), cast(Any, clients)) is True
    assert await process_one(cast(Any, store), cast(Any, clients)) is True
    assert store.attempts == store.finishes == 2
    store.persist_finish = False
    assert await process_one(cast(Any, store), cast(Any, clients)) is True
    assert store.attempts == store.finishes == 3

    jobs = _metrics(metric_reader)["agent.job.total"].data.data_points
    retries = [
        point
        for point in jobs
        if point.attributes == {"purpose": "rag_ingestion", "status": "retrying"}
    ]
    assert len(retries) == 1 and retries[0].value == 2
    lease_lost = [
        point
        for point in jobs
        if point.attributes == {"purpose": "rag_ingestion", "status": "lease_lost"}
    ]
    assert len(lease_lost) == 1 and lease_lost[0].value == 1
    assert "private document text" not in repr(retries)
    assert "private vector store detail" not in repr(retries)
