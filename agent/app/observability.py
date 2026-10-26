"""可选 OpenTelemetry provider；不记录 HTTP 请求或响应正文。"""

import asyncio
import logging
import os
from collections.abc import AsyncIterator, Callable, Coroutine, Mapping
from contextlib import asynccontextmanager
from contextvars import ContextVar
from dataclasses import dataclass
from functools import wraps
from time import monotonic
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

from app.metrics import safe_metric_attributes

logger = logging.getLogger(__name__)
_tracer = trace.get_tracer("oj.agent")
P = ParamSpec("P")
T = TypeVar("T")
_purpose: ContextVar[str] = ContextVar("agent_trace_purpose", default="chat")
_job_metric_status: ContextVar[str | None] = ContextVar("agent_job_metric_status", default=None)
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
    if _job_metric_status.get() is not None:
        _job_metric_status.set(safe_status)
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
            async with job_metrics(purpose):
                async with job_span(name, purpose, root=root):
                    return await function(*args, **kwargs)

        return wrapped

    return decorate


@dataclass(frozen=True)
class _MetricInstruments:
    runs_started: Any
    runs_terminal: Any
    runs_in_flight: Any
    run_duration: Any
    first_event: Any
    model_attempts: Any
    model_retries: Any
    model_duration: Any
    model_in_flight: Any
    tool_total: Any
    tool_duration: Any
    tool_in_flight: Any
    rag_total: Any
    rag_duration: Any
    rag_in_flight: Any
    job_total: Any
    job_duration: Any
    job_in_flight: Any


def _create_metric_instruments(meter: Any) -> _MetricInstruments:
    return _MetricInstruments(
        runs_started=meter.create_counter("agent.run.started", description="Accepted Agent Runs"),
        runs_terminal=meter.create_counter("agent.run.terminal", description="Terminal Agent Runs"),
        runs_in_flight=meter.create_up_down_counter(
            "agent.run.in_flight", description="Agent Runs currently streaming"
        ),
        run_duration=meter.create_histogram("agent.run.duration", unit="s"),
        first_event=meter.create_histogram("agent.run.first_event.duration", unit="s"),
        model_attempts=meter.create_counter("agent.model.attempt", description="Model Attempts"),
        model_retries=meter.create_counter("agent.model.retry", description="Model Retries"),
        model_duration=meter.create_histogram("agent.model.duration", unit="s"),
        model_in_flight=meter.create_up_down_counter("agent.model.in_flight"),
        tool_total=meter.create_counter("agent.tool.total", description="Tool Calls"),
        tool_duration=meter.create_histogram("agent.tool.duration", unit="s"),
        tool_in_flight=meter.create_up_down_counter("agent.tool.in_flight"),
        rag_total=meter.create_counter("agent.rag.total", description="RAG Operations"),
        rag_duration=meter.create_histogram("agent.rag.duration", unit="s"),
        rag_in_flight=meter.create_up_down_counter("agent.rag.in_flight"),
        job_total=meter.create_counter("agent.job.total", description="Background Job Outcomes"),
        job_duration=meter.create_histogram("agent.job.duration", unit="s"),
        job_in_flight=meter.create_up_down_counter("agent.job.in_flight"),
    )


_metric_instruments = _create_metric_instruments(metrics.get_meter("oj.agent"))


def _safe_add(instrument: Any, value: int, attributes: Mapping[str, str] | None = None) -> None:
    try:
        instrument.add(value, attributes)
    except Exception:
        logger.debug("Agent metric recording failed", extra={"error_code": "METRIC_RECORD_FAILED"})


def _safe_record(
    instrument: Any, value: float, attributes: Mapping[str, str] | None = None
) -> None:
    try:
        instrument.record(value, attributes)
    except Exception:
        logger.debug("Agent metric recording failed", extra={"error_code": "METRIC_RECORD_FAILED"})


def _operation_instruments(kind: str) -> tuple[Any, Any, Any]:
    if kind == "tool":
        return (
            _metric_instruments.tool_total,
            _metric_instruments.tool_duration,
            _metric_instruments.tool_in_flight,
        )
    if kind == "rag":
        return (
            _metric_instruments.rag_total,
            _metric_instruments.rag_duration,
            _metric_instruments.rag_in_flight,
        )
    raise ValueError("Unsupported Agent metric operation")


@asynccontextmanager
async def operation_metrics(
    kind: str, **dimensions: object
) -> AsyncIterator[Callable[[str], None]]:
    """Measure one bounded model-adjacent operation using safe dimensions only."""
    total, duration, in_flight = _operation_instruments(kind)
    base = safe_metric_attributes({"purpose": current_purpose(), **dimensions})
    status = "success"
    started = monotonic()

    def set_status(value: str) -> None:
        nonlocal status
        status = value if value in {"success", "error", "timeout", "cancelled"} else "error"

    _safe_add(in_flight, 1, base)
    try:
        yield set_status
    except (asyncio.CancelledError, GeneratorExit):
        status = "cancelled"
        raise
    except TimeoutError:
        status = "timeout"
        raise
    except Exception as exc:
        code = getattr(exc, "code", None)
        status = "timeout" if isinstance(code, str) and code.endswith("_TIMEOUT") else "error"
        raise
    finally:
        attributes = safe_metric_attributes({**base, "outcome": status})
        _safe_add(total, 1, attributes)
        _safe_record(duration, max(0.0, monotonic() - started), attributes)
        _safe_add(in_flight, -1, base)


@asynccontextmanager
async def job_metrics(purpose: str) -> AsyncIterator[None]:
    """Measure one logical job while keeping retry and lease statuses explicit."""
    safe_purpose = purpose if purpose in _JOB_PURPOSES else "other"
    base = safe_metric_attributes(purpose=safe_purpose)
    status_token = _job_metric_status.set("completed")
    status = "completed"
    started = monotonic()
    _safe_add(_metric_instruments.job_in_flight, 1, base)
    try:
        try:
            yield
        except (asyncio.CancelledError, GeneratorExit):
            status = "cancelled"
            raise
        except TimeoutError:
            status = "failed"
            raise
        except Exception:
            status = "failed"
            raise
        else:
            current = _job_metric_status.get()
            status = current if current in _JOB_STATUSES else "completed"
    finally:
        attributes = safe_metric_attributes({**base, "status": status})
        try:
            _safe_add(_metric_instruments.job_total, 1, attributes)
            _safe_record(
                _metric_instruments.job_duration, max(0.0, monotonic() - started), attributes
            )
            _safe_add(_metric_instruments.job_in_flight, -1, base)
        finally:
            _job_metric_status.reset(status_token)


def record_job_retry(purpose: str) -> None:
    """Count a retry only after the caller has decided to schedule it."""
    attributes = safe_metric_attributes(purpose=purpose, status="retrying")
    _safe_add(_metric_instruments.job_total, 1, attributes)


def model_attempt_started(model: object) -> tuple[float, dict[str, str]]:
    attributes = safe_metric_attributes(purpose=current_purpose(), model=model)
    _safe_add(_metric_instruments.model_in_flight, 1, attributes)
    return monotonic(), attributes


def model_attempt_finished(measurement: tuple[float, dict[str, str]], outcome: str) -> None:
    started, base = measurement
    safe_outcome = outcome if outcome in {"success", "error", "timeout", "cancelled"} else "error"
    attributes = safe_metric_attributes({**base, "outcome": safe_outcome})
    _safe_add(_metric_instruments.model_attempts, 1, attributes)
    _safe_record(_metric_instruments.model_duration, max(0.0, monotonic() - started), attributes)
    _safe_add(_metric_instruments.model_in_flight, -1, base)


def record_model_retry(model: object) -> None:
    attributes = safe_metric_attributes(purpose=current_purpose(), model=model, outcome="retrying")
    _safe_add(_metric_instruments.model_retries, 1, attributes)


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
            async def invoke() -> T:
                async with span(name):
                    value = await function(*args, **kwargs)
                    error_code = getattr(value, "error_code", None)
                    if isinstance(error_code, str):
                        current = trace.get_current_span()
                        current.set_attribute("tool.status", "error" if error_code else "ok")
                        if error_code:
                            current.set_attribute("error.code", error_code)
                    return value

            if name == "agent.tool":
                tool = kwargs.get("name")
                if tool is None:
                    tool = args[1] if len(args) > 1 else (args[0] if args else None)
                async with operation_metrics("tool", tool=tool) as set_status:
                    value = await invoke()
                    ok = getattr(value, "ok", None)
                    if isinstance(ok, bool):
                        if ok:
                            set_status("success")
                        elif getattr(value, "error_code", None) == "TOOL_TIMEOUT":
                            set_status("timeout")
                        else:
                            set_status("error")
                    return value
            if name == "agent.rag.retrieve":
                async with operation_metrics("rag"):
                    return await invoke()
            return await invoke()

        return wrapped

    return decorate


def run_started() -> None:
    _safe_add(_metric_instruments.runs_started, 1)
    _safe_add(_metric_instruments.runs_in_flight, 1)


def run_first_event(seconds: float) -> None:
    _safe_record(_metric_instruments.first_event, seconds)


def run_finished(status: str, seconds: float) -> None:
    safe_status = status if status in {"completed", "failed", "cancelled"} else "failed"
    attributes = safe_metric_attributes(status=safe_status)
    _safe_add(_metric_instruments.runs_terminal, 1, attributes)
    _safe_add(_metric_instruments.runs_in_flight, -1)
    _safe_record(_metric_instruments.run_duration, seconds, attributes)


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
