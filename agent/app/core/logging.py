"""基础 JSON 日志；请求内容和驱动异常文本不进入日志。"""

import logging
import os
from datetime import UTC, datetime
from logging.config import dictConfig
from typing import Any


class JSONFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        import json

        entry: dict[str, str] = {
            "timestamp": datetime.fromtimestamp(record.created, UTC).isoformat(),
            "level": record.levelname,
            "logger": record.name,
            "message": record.getMessage(),
            "service": "agent-service",
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
