"""后台任务 Trace 根节点、并发隔离和导出字段边界。"""

import asyncio
from types import SimpleNamespace

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
        async with observability.job_span("agent.eval.case", "eval"):
            observability.job_status("failed", "AGENT_MODEL_QUOTA")

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
    failed = next(item for item in spans if item.name == "agent.eval.case")
    assert failed.attributes is not None
    assert failed.attributes["error.code"] == "AGENT_MODEL_QUOTA"
    assert failed.status.status_code.name == "ERROR"
    assert all("secret-marker" not in repr(item.attributes) for item in spans)
    provider.shutdown()


async def test_agent_business_spans_form_a_parented_tool_chain_without_raw_names(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    monkeypatch.setattr(observability, "_tracer", provider.get_tracer("test"))

    class ToolExecutor:
        @observability.traced_async("agent.tool")
        async def execute(self, name: str) -> SimpleNamespace:
            return SimpleNamespace(ok=True, error_code=None)

    async with observability.span("agent.run", {"agent.run_id": "run-1"}):
        async with observability.business_step(2, "tool"):
            await ToolExecutor().execute("get_problem")

    spans = exporter.get_finished_spans()
    by_name = {item.name: item for item in spans}
    assert {"agent.run", "agent.step", "agent.tool"}.issubset(by_name)
    run = by_name["agent.run"]
    step = by_name["agent.step"]
    tool = by_name["agent.tool"]
    assert step.parent is not None and step.parent.span_id == run.context.span_id
    assert tool.parent is not None and tool.parent.span_id == step.context.span_id
    assert step.attributes is not None and step.attributes["agent.step"] == 2
    assert step.attributes is not None and step.attributes["agent.phase"] == "tool"
    assert tool.attributes is not None and tool.attributes["agent.tool"] == "get_problem"
    assert tool.attributes is not None and tool.attributes["agent.status"] == "completed"
    assert all("private" not in repr(item.attributes) for item in spans)
    provider.shutdown()
