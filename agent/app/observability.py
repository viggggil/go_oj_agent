"""可选 OpenTelemetry provider；不记录 HTTP 请求或响应正文。"""

import asyncio
import logging
import os
from collections.abc import AsyncIterator, Callable, Coroutine, Mapping
from contextlib import asynccontextmanager
from functools import wraps
from typing import Any, ParamSpec, TypeVar

from opentelemetry import metrics, propagate, trace
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
    name: str, attributes: Mapping[str, str | int | bool] | None = None
) -> AsyncIterator[Span]:
    """Create a low-cardinality business span without recording exception text."""
    with _tracer.start_as_current_span(
        name,
        kind=SpanKind.INTERNAL,
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
