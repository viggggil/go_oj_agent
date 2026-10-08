"""应用工厂；所有长生命周期依赖在 lifespan 内建立并释放。"""

import asyncio
import logging
from collections.abc import AsyncIterator, Callable
from contextlib import asynccontextmanager

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse
from pydantic import BaseModel

from app.api.chat import ChatController
from app.api.chat import router as chat_router
from app.api.health import router
from app.clients.business import build_business_clients
from app.core.auth import DelegationVerifier
from app.core.database import DatabaseProbe, ReadinessProbe
from app.core.resources import AppResources
from app.core.runtime_config import DemoConfigReader
from app.core.settings import ConfigurationError, Settings, load_settings
from app.graphs.langgraph_runtime import LangGraphRuntime
from app.graphs.runtime import FakeModelClient, FakeRuntime
from app.graphs.service import RunService
from app.storage.repository import AgentStore
from app.tools.business import build_business_tool_registry
from app.tools.registry import ToolExecutor

logger = logging.getLogger(__name__)


class ErrorResponse(BaseModel):
    code: str
    message: str


def create_app(
    settings: Settings | None = None,
    *,
    probe_factory: Callable[[Settings], ReadinessProbe] = DatabaseProbe,
    run_service_factory: Callable[[Settings, ReadinessProbe], RunService] | None = None,
) -> FastAPI:
    config = settings if settings is not None else load_settings()

    @asynccontextmanager
    async def lifespan(application: FastAPI) -> AsyncIterator[None]:
        database = probe_factory(config)
        controller: ChatController | None = None
        business_clients = None
        business_tools = None
        try:
            if config.business_tools_enabled:
                business_clients = build_business_clients(config)
                business_tools = build_business_tool_registry(
                    business_clients, config.tool_max_result_bytes, config.tool_max_page_size
                )
            tool_executor = ToolExecutor(business_tools) if business_tools is not None else None
            if config.chat_enabled:
                verifier = DelegationVerifier(config)
                if run_service_factory is not None:
                    service = run_service_factory(config, database)
                else:
                    if not isinstance(database, DatabaseProbe) or database.engine is None:
                        raise ConfigurationError("Chat requires an Agent database")
                    runtime = (
                        LangGraphRuntime(FakeModelClient(tool_executor))
                        if config.runtime_mode == "langgraph_fake"
                        else FakeRuntime(tool_executor=tool_executor)
                    )
                    service = RunService(
                        AgentStore(database.engine), runtime, DemoConfigReader(config)
                    )
                try:
                    async with asyncio.timeout(config.preflight_timeout_seconds):
                        await service.initialize()
                except Exception as exc:
                    # Uvicorn 会打印 lifespan 异常，不能让驱动原始异常进入启动日志。
                    logger.error(
                        "Agent Chat initialization failed",
                        extra={
                            "error_code": "AGENT_STARTUP_FAILED",
                            "error_type": type(exc).__name__,
                        },
                    )
                    raise ConfigurationError("Agent Chat initialization failed") from None
                controller = ChatController(config, verifier, service)
            application.state.resources = AppResources(
                settings=config,
                database=database,
                chat=controller,
                business_clients=business_clients,
                tools=business_tools,
            )
            logger.info("Agent service started")
            yield
        finally:
            try:
                try:
                    if controller is not None:
                        async with asyncio.timeout(config.shutdown_timeout_seconds / 2):
                            await controller.close()
                    if business_clients is not None:
                        await business_clients.close()
                finally:
                    database_budget = (
                        config.shutdown_timeout_seconds / 2
                        if controller is not None
                        else config.shutdown_timeout_seconds
                    )
                    async with asyncio.timeout(database_budget):
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
                if hasattr(application.state, "resources"):
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
    if config.chat_enabled:
        application.include_router(chat_router)
    return application
