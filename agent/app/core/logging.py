"""基础 JSON 日志；请求内容和驱动异常文本不进入日志。"""

import logging
import os
from datetime import UTC, datetime
from logging.config import dictConfig
from typing import Any

_KNOWN_ERROR_CODES = frozenset(
    {
        "AGENT_DEMO_FAILED",
        "AGENT_INTERNAL_ERROR",
        "AGENT_STARTUP_FAILED",
        "AGENT_SHUTDOWN_FAILED",
        "AGENT_RUN_FAILED",
        "AGENT_BACKGROUND_FAILED",
        "AGENT_FINALIZATION_FAILED",
        "AGENT_RUNTIME_CLOSE_FAILED",
        "AGENT_MODEL_TIMEOUT",
        "AGENT_MODEL_UNAVAILABLE",
        "AGENT_MODEL_RATE_LIMIT",
        "AGENT_MODEL_REJECTED",
        "AGENT_MODEL_AUTH_FAILED",
        "AGENT_MODEL_QUOTA",
        "AGENT_MODEL_BLOCKED",
        "AGENT_MODEL_INVALID_RESPONSE",
        "AGENT_MODEL_OUTPUT_LIMIT",
        "AGENT_MODEL_INPUT_LIMIT",
        "AGENT_MODEL_CALL_LIMIT",
        "AGENT_MODEL_UNSUPPORTED_OUTPUT",
        "AGENT_CREDENTIAL_UNAVAILABLE",
        "METRIC_RECORD_FAILED",
        "LANGSMITH_ENQUEUE_FAILED",
        "LANGSMITH_QUEUE_FULL",
        "LANGSMITH_STATUS_UPDATE_FAILED",
        "LANGSMITH_EXPORT_FAILED",
        "LANGSMITH_EXPORTER_WORKER_FAILED",
        "LANGSMITH_SHUTDOWN_TIMEOUT",
        "LANGSMITH_KEY_MISSING",
        "LANGSMITH_INIT_FAILED",
        "AGENT_RESTARTED",
        "AGENT_DEADLINE_EXCEEDED",
        "AGENT_CANCELLED",
        "AGENT_EVAL_REVIEW_INTERRUPTED",
        "AGENT_TOOL_FAILED",
    }
)


class JSONFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        import json

        entry: dict[str, str] = {
            "timestamp": datetime.fromtimestamp(record.created, UTC).isoformat(),
            "level": record.levelname,
            "logger": record.name,
            "message": record.getMessage(),
            "service_name": "agent-service",
            "environment": os.getenv("AGENT_ENVIRONMENT", "development"),
        }
        try:
            from opentelemetry import trace

            span_context = trace.get_current_span().get_span_context()
            if span_context.is_valid:
                entry["trace_id"] = f"{span_context.trace_id:032x}"
                entry["span_id"] = f"{span_context.span_id:016x}"
        except ImportError:
            pass
        for key in ("error_code", "error_type", "run_id", "request_id"):
            value = getattr(record, key, None)
            if isinstance(value, str):
                if key == "error_code":
                    value = value if value in _KNOWN_ERROR_CODES else "OTHER"
                entry[key] = value
        # 不输出 exc_info / stack_info，防止驱动异常中附带连接凭据。
        return json.dumps(entry, ensure_ascii=False)


def logging_config(level: str) -> dict[str, Any]:
    return {
        "version": 1,
        "disable_existing_loggers": False,
        "formatters": {"json": {"()": "app.core.logging.JSONFormatter"}},
        "handlers": {
            "stderr": {
                "class": "logging.StreamHandler",
                "stream": "ext://sys.stderr",
                "formatter": "json",
            }
        },
        "root": {"level": level, "handlers": ["stderr"]},
        "loggers": {
            "uvicorn": {"handlers": ["stderr"], "level": level, "propagate": False},
            "uvicorn.error": {"level": level},
            "uvicorn.access": {"handlers": [], "propagate": False},
        },
    }


def configure_logging(level: str) -> None:
    dictConfig(logging_config(level))
