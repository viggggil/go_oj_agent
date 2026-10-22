"""后台任务 Trace 根节点、并发隔离和导出字段边界。"""

import asyncio

import pytest
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from app import observability


async def test_background_jobs_are_detached_roots_and_keep_context_isolated(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    monkeypatch.setattr(observability, "_tracer", provider.get_tracer("test"))

    async def job(purpose: str) -> None:
        async with observability.job_span(
            "agent.test.job",
            purpose,
            root=True,
            attributes={"job.status": "completed", "secret": "secret-marker"},
        ):
            async with observability.job_span("agent.test.child", purpose):
                await asyncio.sleep(0)

    async with observability.span("http.request"):
        await asyncio.gather(job("memory"), job("eval"))

    spans = exporter.get_finished_spans()
    roots = [item for item in spans if item.name == "agent.test.job"]
    children = [item for item in spans if item.name == "agent.test.child"]
    assert len(roots) == len(children) == 2
    assert all(item.parent is None for item in roots)
    parent_ids = set()
    for child in children:
        parent = child.parent
        assert parent is not None
        parent_ids.add((child.context.trace_id, parent.span_id))
    assert parent_ids == {(root.context.trace_id, root.context.span_id) for root in roots}
    purposes = set()
    for item in roots:
        assert item.attributes is not None
        purposes.add(item.attributes["agent.purpose"])
    assert purposes == {"memory", "eval"}
    assert all("secret-marker" not in repr(item.attributes) for item in spans)
    provider.shutdown()
