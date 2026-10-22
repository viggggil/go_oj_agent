"""显式、脱敏且有界的 LangSmith Run 导出。"""

import asyncio
import logging
import os
import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any
from uuid import UUID, uuid5

from langsmith import Client
from langsmith.utils import Retry  # type: ignore[attr-defined]

from app.models.provider import ModelSummary

logger = logging.getLogger(__name__)
_NAMESPACE = UUID("06e6c355-fd54-4db2-9b62-d3240134980a")
_TRACE_ID = re.compile(r"^[0-9a-f]{32}$")
_DEPLOYMENT_VERSION = re.compile(r"^(?:\d+\.\d+\.\d+(?:[-+][a-f0-9]{7,12})?|[a-f0-9]{7,12})$")
_MODEL_ALIASES = {"deepseek-v4-flash", "deepseek-v4-pro", "fake"}
_TOOLS = {
    "get_problem",
    "get_submission",
    "get_submission_source",
    "get_judge_result",
    "list_problems",
    "list_submissions",
    "retrieve_knowledge",
    "run_code",
}
_ERROR_CODES = {
    "AGENT_CANCELLED",
    "AGENT_DEADLINE_EXCEEDED",
    "AGENT_RUN_FAILED",
    "AGENT_HISTORY_UNAVAILABLE",
    "AGENT_MODEL_TIMEOUT",
    "AGENT_MODEL_UNAVAILABLE",
    "AGENT_MODEL_RATE_LIMIT",
    "AGENT_MODEL_REJECTED",
    "AGENT_MODEL_AUTH_FAILED",
    "AGENT_MODEL_QUOTA",
    "AGENT_MODEL_INVALID_RESPONSE",
    "AGENT_MODEL_OUTPUT_LIMIT",
    "AGENT_MODEL_INPUT_LIMIT",
    "AGENT_MODEL_CALL_LIMIT",
    "AGENT_MODEL_INVALID_ARGUMENT",
    "AGENT_MODEL_UNSUPPORTED_OUTPUT",
    "AGENT_MODEL_BLOCKED",
    "AGENT_MODEL_ENDPOINT_DENIED",
    "AGENT_CREDENTIAL_UNAVAILABLE",
    "AGENT_TOOL_CALL_LIMIT",
    "TOOL_PERMISSION_DENIED",
    "TOOL_INVALID_ARGUMENT",
    "TOOL_DUPLICATE_CALL_ID",
    "TOOL_REPEATED_CALL",
    "AGENT_TOOL_FAILED",
    "AGENT_TOOL_TIMEOUT",
}


@dataclass(frozen=True)
class SafeChild:
    id: UUID
    name: str
    run_type: str
    duration_ms: int
    metadata: dict[str, Any]
    error: str | None = None


@dataclass(frozen=True)
class SafeRun:
    id: UUID
    started_at: datetime
    duration_ms: int
    status: str
    error: str | None
    metadata: dict[str, Any]
    children: tuple[SafeChild, ...]


def _safe_error(value: str | None) -> str | None:
    return value if value in _ERROR_CODES else ("AGENT_RUN_FAILED" if value else None)


def _safe_model(value: str) -> str:
    return value if value in _MODEL_ALIASES else "other"


def _safe_tool(value: str) -> str:
    return value if value in _TOOLS else "other"


def build_safe_run(
    *,
    run_id: UUID,
    started_at: datetime,
    status: str,
    error_code: str | None,
    model_summary: ModelSummary | None,
    trace_id: str | None,
    environment: str,
    deployment_version: str,
    config_version: str | None,
    finished_at: datetime | None = None,
) -> SafeRun:
    """仅从脱敏摘要挑选字段；不会保留 AgentState 或任何自由文本。"""
    status_map = {
        "COMPLETED": "completed",
        "FAILED": "failed",
        "CANCELLED": "cancelled",
        "INTERRUPTED": "failed",
    }
    safe_status = status_map.get(status, "failed")
    metadata: dict[str, Any] = {
        "service": "agent-service",
        "environment": environment
        if environment in {"development", "test", "production"}
        else "other",
        "deployment_version": deployment_version
        if _DEPLOYMENT_VERSION.fullmatch(deployment_version)
        else "unknown",
    }
    if trace_id and _TRACE_ID.fullmatch(trace_id):
        metadata["otel_trace_id"] = trace_id
    if config_version:
        try:
            metadata["agent_config_version"] = str(UUID(config_version))
        except ValueError:
            pass

    started_at = started_at.replace(tzinfo=UTC) if started_at.tzinfo is None else started_at
    end = finished_at or datetime.now(UTC)
    end = end.replace(tzinfo=UTC) if end.tzinfo is None else end
    duration_ms = max(0, min(int((end - started_at).total_seconds() * 1000), 86_400_000))
    children: list[SafeChild] = []
    if model_summary is not None:
        tool_durations = {step.call_id: step.duration_ms for step in model_summary.tool_steps}
        for index, attempt in enumerate(model_summary.attempts):
            attempt_error = _safe_error(attempt.error_code)
            children.append(
                SafeChild(
                    id=uuid5(_NAMESPACE, f"{run_id}:model:{index}"),
                    name="agent.model_attempt",
                    run_type="llm",
                    duration_ms=min(attempt.duration_ms, 300_000),
                    metadata={
                        "model": _safe_model(attempt.model),
                        "input_tokens": attempt.input_tokens,
                        "output_tokens": attempt.output_tokens,
                        "usage_source": attempt.usage_source,
                        "status": attempt.finish_reason
                        if attempt.finish_reason
                        in {"completed", "failed", "incomplete", "cancelled"}
                        else "failed",
                        **({"error_code": attempt_error} if attempt_error else {}),
                    },
                    error=attempt_error,
                )
            )
        for index, step in enumerate(model_summary.tool_steps):
            step_error = _safe_error(step.error_code)
            children.append(
                SafeChild(
                    id=uuid5(_NAMESPACE, f"{run_id}:tool:{index}"),
                    name=f"agent.tool.{_safe_tool(step.tool_name)}",
                    run_type="tool",
                    duration_ms=min(step.duration_ms, 300_000),
                    metadata={
                        "tool": _safe_tool(step.tool_name),
                        "status": step.status,
                        **({"error_code": step_error} if step_error else {}),
                    },
                    error=step_error,
                )
            )
        for index, knowledge_attempt in enumerate(model_summary.knowledge):
            error = _safe_error(knowledge_attempt.error_code)
            children.append(
                SafeChild(
                    id=uuid5(_NAMESPACE, f"{run_id}:knowledge:{index}"),
                    name="agent.knowledge_retrieval",
                    run_type="retriever",
                    duration_ms=min(tool_durations.get(knowledge_attempt.call_id, 0), 300_000),
                    metadata={
                        "status": "completed" if knowledge_attempt.ok else "failed",
                        "source_count": len(knowledge_attempt.sources),
                        **({"error_code": error} if error else {}),
                    },
                    error=error,
                )
            )
    return SafeRun(
        id=run_id,
        started_at=started_at,
        duration_ms=duration_ms,
        status=safe_status,
        error=_safe_error(error_code),
        metadata=metadata,
        children=tuple(children),
    )


def build_run_batch(run: SafeRun, client: Any) -> list[dict[str, Any]]:
    """Build LangSmith SDK request dictionaries with empty inputs and outputs."""
    project = f"oj-agent-{run.metadata['environment']}"
    root_trace_id = (
        UUID(run.metadata["otel_trace_id"]) if "otel_trace_id" in run.metadata else run.id
    )
    root_started = run.started_at
    root_dotted = f"{root_started.strftime('%Y%m%dT%H%M%S%fZ')}{run.id}"
    result: list[dict[str, Any]] = [
        {
            "id": str(run.id),
            "name": "agent.chat",
            "run_type": "chain",
            "start_time": root_started,
            "end_time": root_started + timedelta(milliseconds=run.duration_ms),
            "inputs": {},
            "outputs": {},
            "error": run.error,
            "tags": ["oj-agent", "chat"],
            "extra": {"metadata": {**run.metadata, "status": run.status}},
            "session_name": project,
            "trace_id": str(root_trace_id),
            "dotted_order": root_dotted,
        }
    ]
    for child in run.children:
        child_start = root_started
        result.append(
            {
                "id": str(child.id),
                "name": child.name,
                "run_type": child.run_type,
                "start_time": child_start,
                "end_time": child_start + timedelta(milliseconds=child.duration_ms),
                "inputs": {},
                "outputs": {},
                "error": child.error,
                "tags": ["oj-agent"],
                "extra": {"metadata": child.metadata},
                "session_name": project,
                "parent_run_id": str(run.id),
                "trace_id": str(root_trace_id),
                "dotted_order": (
                    f"{root_dotted}.{child_start.strftime('%Y%m%dT%H%M%S%fZ')}{child.id}"
                ),
            }
        )
    return result


class LangSmithExporter:
    """Small bounded queue; all SDK/network work runs off the event loop."""

    def __init__(
        self,
        api_key: str,
        *,
        environment: str,
        deployment_version: str,
        queue_size: int = 32,
        client: Any | None = None,
    ) -> None:
        if not api_key:
            raise ValueError("LangSmith API key is required")
        self.environment = environment
        self.deployment_version = deployment_version
        self._client = client or Client(
            api_url="https://api.smith.langchain.com",
            api_key=api_key,
            timeout_ms=3000,
            retry_config=Retry(total=0),
            auto_batch_tracing=False,
            hide_inputs=True,
            hide_outputs=True,
            omit_traced_runtime_info=True,
            max_batch_size_bytes=256 * 1024,
        )
        self._queue: asyncio.Queue[tuple[SafeRun, ...] | None] = asyncio.Queue(queue_size)
        self._worker: asyncio.Task[None] | None = None

    def start(self) -> None:
        if self._worker is None:
            self._worker = asyncio.create_task(self._run(), name="langsmith-exporter")

    def submit(
        self,
        *,
        run_id: UUID,
        started_at: datetime,
        status: str,
        error_code: str | None,
        model_summary: ModelSummary | None,
        trace_id: str | None,
        config_version: str | None,
        finished_at: datetime | None = None,
    ) -> None:
        safe = build_safe_run(
            run_id=run_id,
            started_at=started_at,
            status=status,
            error_code=error_code,
            model_summary=model_summary,
            trace_id=trace_id,
            environment=self.environment,
            deployment_version=self.deployment_version,
            config_version=config_version,
            finished_at=finished_at,
        )
        try:
            self._queue.put_nowait((safe,))
        except asyncio.QueueFull:
            logger.warning(
                "LangSmith export queue full", extra={"error_code": "LANGSMITH_QUEUE_FULL"}
            )

    async def _run(self) -> None:
        while True:
            item = await self._queue.get()
            try:
                if item is None:
                    return
                await asyncio.to_thread(self._send, item)
            except Exception as exc:
                logger.warning(
                    "LangSmith export failed",
                    extra={
                        "error_code": "LANGSMITH_EXPORT_FAILED",
                        "error_type": type(exc).__name__,
                    },
                )
            finally:
                self._queue.task_done()

    def _send(self, runs: tuple[SafeRun, ...]) -> None:
        batch: list[dict[str, Any]] = []
        for run in runs:
            batch.extend(build_run_batch(run, self._client))
        self._client.batch_ingest_runs(create=batch)

    async def close(self, timeout_seconds: float = 3) -> None:
        worker = self._worker
        if worker is None:
            close = getattr(self._client, "close", None)
            if close:
                await asyncio.to_thread(close)
            return
        try:
            await asyncio.wait_for(self._queue.put(None), timeout=timeout_seconds / 2)
            await asyncio.wait_for(worker, timeout=timeout_seconds / 2)
        except TimeoutError:
            logger.warning(
                "LangSmith exporter shutdown timed out",
                extra={"error_code": "LANGSMITH_SHUTDOWN_TIMEOUT"},
            )
            worker.cancel()
            await asyncio.gather(worker, return_exceptions=True)
        finally:
            close = getattr(self._client, "close", None)
            if close:
                await asyncio.to_thread(close)


def new_exporter(enabled: bool, api_key: str | None, environment: str) -> LangSmithExporter | None:
    if not enabled:
        return None
    if not api_key:
        logger.warning(
            "LangSmith export enabled without API key",
            extra={"error_code": "LANGSMITH_KEY_MISSING"},
        )
        return None
    try:
        return LangSmithExporter(
            api_key,
            environment=environment,
            deployment_version=os.getenv("AGENT_VERSION", "dev"),
        )
    except Exception as exc:
        logger.warning(
            "LangSmith exporter initialization failed",
            extra={"error_code": "LANGSMITH_INIT_FAILED", "error_type": type(exc).__name__},
        )
        return None
