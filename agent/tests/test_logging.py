"""日志采用固定字段，异常文本不会进入结构化日志。"""

import json
import logging

from app.core.logging import JSONFormatter


def test_formatter_redacts_exception_and_unlisted_extras() -> None:
    try:
        raise RuntimeError("private-error-password")
    except RuntimeError as exc:
        record = logging.LogRecord(
            "agent",
            logging.ERROR,
            __file__,
            1,
            "Database readiness check failed",
            (),
            (RuntimeError, exc, exc.__traceback__),
        )
    record.error_code = "AGENT_DATABASE_UNAVAILABLE"
    record.api_key = "private-key"
    output = JSONFormatter().format(record)
    data = json.loads(output)
    assert data["error_code"] == "AGENT_DATABASE_UNAVAILABLE"
    assert data["message"] == "Database readiness check failed"
    assert "private-error-password" not in output
    assert "private-key" not in output
    assert "timestamp" in data
