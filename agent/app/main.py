"""应用工厂；所有长生命周期依赖在 lifespan 内建立并释放。"""

import asyncio
import logging
from collections.abc import AsyncIterator, Callable
from contextlib import asynccontextmanager

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse
from pydantic import BaseModel

from app.api.health import router
from app.core.database import DatabaseProbe, ReadinessProbe
from app.core.resources import AppResources
from app.core.settings import Settings, load_settings

logger = logging.getLogger(__name__)


class ErrorResponse(BaseModel):
    code: str
    message: str


def create_app(
    settings: Settings | None = None,
    *,
    probe_factory: Callable[[Settings], ReadinessProbe] = DatabaseProbe,
) -> FastAPI:
    config = settings if settings is not None else load_settings()

    @asynccontextmanager
    async def lifespan(application: FastAPI) -> AsyncIterator[None]:
        database = probe_factory(config)
        application.state.resources = AppResources(settings=config, database=database)
        logger.info("Agent service started")
        try:
            yield
        finally:
            try:
                async with asyncio.timeout(config.shutdown_timeout_seconds):
                    await database.close()
            except Exception as exc:
                logger.error(
                    "Agent resource shutdown failed",
                    extra={
                        "error_code": "AGENT_SHUTDOWN_FAILED",
                        "error_type": type(exc).__name__,
                    },
                )
            finally:
                del application.state.resources
                logger.info("Agent service stopped")

    application = FastAPI(
        title="OJ Agent Service",
        lifespan=lifespan,
        docs_url=None,
        redoc_url=None,
        openapi_url=None,
    )

    @application.exception_handler(Exception)
    async def internal_error(_request: Request, exc: Exception) -> JSONResponse:
        logger.error(
            "Agent request failed",
            extra={"error_code": "AGENT_INTERNAL_ERROR", "error_type": type(exc).__name__},
        )
        error = ErrorResponse(
            code="AGENT_INTERNAL_ERROR",
            message="Agent service failed to handle the request",
        )
        return JSONResponse(status_code=500, content=error.model_dump())

    application.include_router(router)
    return application
