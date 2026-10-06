"""python -m app 启动入口。"""

import logging

import uvicorn

from app.core.logging import configure_logging, logging_config
from app.core.settings import ConfigurationError, load_settings
from app.main import create_app


def main() -> int:
    try:
        settings = load_settings()
    except ConfigurationError as exc:
        configure_logging("ERROR")
        logging.getLogger(__name__).error("%s", exc)
        return 2

    configure_logging(settings.log_level)
    uvicorn.run(
        create_app(settings),
        host=settings.host,
        port=settings.port,
        log_config=logging_config(settings.log_level),
        access_log=False,
        timeout_graceful_shutdown=settings.shutdown_timeout_seconds,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
