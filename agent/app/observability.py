"""可选 OpenTelemetry provider；不记录 HTTP 请求或响应正文。"""

import asyncio
import logging
import os
from collections.abc import AsyncIterator, Callable, Coroutine, Mapping
from contextlib import asynccontextmanager
from contextvars import ContextVar
from functools import wraps
from typing import Any, ParamSpec, TypeVar

from opentelemetry import metrics, propagate, trace
from opentelemetry.context import Context, attach, detach
from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import OTLPMetricExporter
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.system_metrics import SystemMetricsInstrumentor
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.trace import Span, SpanKind, Status, StatusCode, set_tracer_provider
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

logger = logging.getLogger(__name__)
_tracer = trace.get_tracer("oj.agent")
P = ParamSpec("P")
T = TypeVar("T")
_purpose: ContextVar[str] = ContextVar("agent_trace_purpose", default="chat")
_JOB_PURPOSES = frozenset({"memory", "eval", "review", "rag_ingestion"})
_JOB_ATTRIBUTE_KEYS = frozenset({"job.attempt", "eval.attempt", "rag.chunk_count"})
_JOB_STATUSES = frozenset({"completed", "failed", "retrying", "cancelled", "lease_lost"})
_JOB_ERRORS = frozenset(
    {
        "AGENT_CANCELLED",
        "AGENT_DEADLINE_EXCEEDED",
        "AGENT_EVAL_CASE_FAILED",
        "AGENT_EVAL_CASE_INVALID",
        "AGENT_EVAL_PRINCIPAL_INVALID",
        "AGENT_EVAL_REVIEW_INVALID",
        "AGENT_EVAL_REVIEW_INPUT_INVALID",
        "AGENT_EVAL_REVIEW_UNAVAILABLE",
        "AGENT_RAG_INDEX_VERSION_INVALID",
        "AGENT_RAG_OBJECT_INVALID",
        "AGENT_RAG_INDEX_FAILED",
        "AGENT_RAG_DEPENDENCY_UNAVAILABLE",
        "AGENT_MODEL_TIMEOUT",
        "AGENT_MODEL_UNAVAILABLE",
        "AGENT_MODEL_RATE_LIMIT",
        "AGENT_MODEL_REJECTED",
        "AGENT_MODEL_AUTH_FAILED",
        "AGENT_MODEL_QUOTA",
        "AGENT_MODEL_BLOCKED",
        "AGENT_MODEL_UNSUPPORTED_OUTPUT",
        "AGENT_MODEL_ENDPOINT_DENIED",
        "AGENT_MODEL_INVALID_RESPONSE",
        "AGENT_MODEL_OUTPUT_LIMIT",
        "AGENT_MODEL_INPUT_LIMIT",
        "AGENT_MODEL_CALL_LIMIT",
        "AGENT_CREDENTIAL_UNAVAILABLE",
        "AGENT_RUN_FAILED",
        "AGENT_HISTORY_UNAVAILABLE",
    }
)


def current_purpose() -> str:
    return _purpose.get()


def job_status(status: str, error_code: object = None) -> None:
    """后台任务只记录受控状态与错误码，调用方不得传入异常正文。"""
    safe_status = status if status in _JOB_STATUSES else "failed"
    current = trace.get_current_span()
    current.set_attribute("job.status", safe_status)
    if safe_status in {"failed", "retrying"}:
        current.set_status(Status(StatusCode.ERROR))
        current.set_attribute(
            "error.code",
            error_code
            if isinstance(error_code, str) and error_code in _JOB_ERRORS
            else "AGENT_BACKGROUND_FAILED",
        )


@asynccontextmanager
async def detached_context() -> AsyncIterator[None]:
    """手动重评脱离发起请求；内部真实 Review Span 自行成为根节点。"""
    token = attach(Context())
    try:
        yield
    finally:
        detach(token)


@asynccontextmanager
async def job_span(
    name: str,
    purpose: str,
    *,
    root: bool = False,
    attributes: Mapping[str, str | int | bool] | None = None,
) -> AsyncIterator[Span]:
    safe_purpose = purpose if purpose in _JOB_PURPOSES else "other"
    safe_attributes = {
        key: value
        for key, value in (attributes or {}).items()
        if key in _JOB_ATTRIBUTE_KEYS and isinstance(value, int) and 0 <= value <= 1_000_000
    }
    token = _purpose.set(safe_purpose)
    try:
        async with span(
            name,
            {"agent.purpose": safe_purpose, **safe_attributes},
            root=root,
            safe_errors=True,
        ) as item:
            try:
                yield item
            except asyncio.CancelledError:
                job_status("cancelled")
                raise
            except Exception as exc:
                job_status("failed", getattr(exc, "code", None))
                raise
    finally:
        _purpose.reset(token)


def traced_job(
    name: str, purpose: str, *, root: bool = False
) -> Callable[[Callable[P, Coroutine[Any, Any, T]]], Callable[P, Coroutine[Any, Any, T]]]:
    def decorate(
        function: Callable[P, Coroutine[Any, Any, T]],
    ) -> Callable[P, Coroutine[Any, Any, T]]:
        @wraps(function)
        async def wrapped(*args: P.args, **kwargs: P.kwargs) -> T:
            async with job_span(name, purpose, root=root):
                return await function(*args, **kwargs)

        return wrapped

    return decorate


_meter = metrics.get_meter("oj.agent")
_runs_started = _meter.create_counter("agent.run.started", description="Accepted Agent Runs")
_runs_terminal = _meter.create_counter("agent.run.terminal", description="Terminal Agent Runs")
_runs_in_flight = _meter.create_up_down_counter(
    "agent.run.in_flight", description="Agent Runs currently streaming"
)
_run_duration = _meter.create_histogram("agent.run.duration", unit="s")
_first_event = _meter.create_histogram("agent.run.first_event.duration", unit="s")


@asynccontextmanager
async def span(
    name: str,
    attributes: Mapping[str, str | int | bool] | None = None,
    *,
    root: bool = False,
    safe_errors: bool = False,
) -> AsyncIterator[Span]:
    """Create a low-cardinality business span without recording exception text."""
    with _tracer.start_as_current_span(
        name,
        kind=SpanKind.INTERNAL,
        context=Context() if root else None,
        attributes=attributes,
        record_exception=False,
        set_status_on_exception=False,
    ) as item:
        try:
            yield item
        except asyncio.CancelledError:
            item.set_attribute("operation.status", "cancelled")
            raise
        except (GeneratorExit, KeyboardInterrupt, SystemExit):
            raise
        except Exception as exc:
            code = getattr(exc, "code", None)
            if safe_errors or current_purpose() != "chat":
                code = (
                    code
                    if isinstance(code, str) and code in _JOB_ERRORS
                    else "AGENT_BACKGROUND_FAILED"
                )
            if isinstance(code, str) and code.isascii() and len(code) <= 64:
                item.set_attribute("error.code", code)
            item.set_status(Status(StatusCode.ERROR))
            raise


def traced_async(
    name: str,
) -> Callable[[Callable[P, Coroutine[Any, Any, T]]], Callable[P, Coroutine[Any, Any, T]]]:
    """Trace a bounded async operation, recording no exception message or input."""

    def decorate(
        function: Callable[P, Coroutine[Any, Any, T]],
    ) -> Callable[P, Coroutine[Any, Any, T]]:
        @wraps(function)
        async def wrapped(*args: P.args, **kwargs: P.kwargs) -> T:
            async with span(name):
                value = await function(*args, **kwargs)
                error_code = getattr(value, "error_code", None)
                if isinstance(error_code, str):
                    current = trace.get_current_span()
                    current.set_attribute("tool.status", "error" if error_code else "ok")
                    if error_code:
                        current.set_attribute("error.code", error_code)
                return value

        return wrapped

    return decorate


def run_started() -> None:
    _runs_started.add(1)
    _runs_in_flight.add(1)


def run_first_event(seconds: float) -> None:
    _first_event.record(seconds)


def run_finished(status: str, seconds: float) -> None:
    safe_status = status if status in {"completed", "failed", "cancelled"} else "failed"
    attributes = {"status": safe_status}
    _runs_terminal.add(1, attributes)
    _runs_in_flight.add(-1)
    _run_duration.record(seconds, attributes)


def configure(service: str, environment: str) -> Callable[[], None]:
    endpoint = os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
    resource = Resource.create({"service.name": service, "deployment.environment": environment})
    try:
        propagate.set_global_textmap(TraceContextTextMapPropagator())
        traces = TracerProvider(resource=resource)
        readers = []
        if endpoint:
            traces.add_span_processor(
                BatchSpanProcessor(
                    OTLPSpanExporter(endpoint=endpoint),
                    max_queue_size=512,
                    max_export_batch_size=128,
                    export_timeout_millis=3000,
                )
            )
            readers.append(
                PeriodicExportingMetricReader(
                    OTLPMetricExporter(endpoint=endpoint),
                    export_interval_millis=15000,
                    export_timeout_millis=3000,
                )
            )
        metric_provider = MeterProvider(resource=resource, metric_readers=readers)
        set_tracer_provider(traces)
        metrics.set_meter_provider(metric_provider)
        if endpoint:
            SystemMetricsInstrumentor().instrument(meter_provider=metric_provider)
    except Exception:
        logger.error(
            "OpenTelemetry initialization failed", extra={"error_code": "OTEL_INIT_FAILED"}
        )
        return lambda: None

    def shutdown() -> None:
        try:
            traces.force_flush(timeout_millis=2000)
            traces.shutdown()
            metric_provider.shutdown(timeout_millis=2000)
        except Exception:
            logger.error(
                "OpenTelemetry shutdown failed", extra={"error_code": "OTEL_SHUTDOWN_FAILED"}
            )

    return shutdown
