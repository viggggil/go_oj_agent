"""RAG worker liveness and dependency readiness without sensitive details."""

import asyncio
import logging
from collections.abc import Awaitable, Callable
from typing import Literal, Protocol

from fastapi import FastAPI, Response
from pydantic import BaseModel

from app.core.database import ReadinessProbe

logger = logging.getLogger(__name__)

DependencyStatus = Literal["ready", "unavailable"]
ReadinessStatus = Literal["ready", "unavailable", "not_configured"]


class RagDependencyProbe(Protocol):
    async def check_embedding(self) -> DependencyStatus: ...

    async def check_qdrant(self) -> DependencyStatus: ...

    async def check_minio(self) -> DependencyStatus: ...


class HealthResponse(BaseModel):
    service: Literal["rag-worker"] = "rag-worker"
    status: Literal["ok"] = "ok"


class ReadinessResponse(BaseModel):
    service: Literal["rag-worker"] = "rag-worker"
    status: Literal["ready", "not_ready"]
    checks: dict[str, ReadinessStatus]


async def _safe_check(
    check: Callable[[], Awaitable[ReadinessStatus]], name: str
) -> ReadinessStatus:
    try:
        result = await check()
        if result in {"ready", "unavailable", "not_configured"}:
            return result
    except Exception as exc:
        logger.warning(
            "RAG readiness check failed",
            extra={
                "error_code": "AGENT_RAG_READINESS_FAILED",
                "check": name,
                "error_type": type(exc).__name__,
            },
        )
    return "unavailable"


def create_health_app(database: ReadinessProbe, clients: RagDependencyProbe) -> FastAPI:
    """Create the standalone HTTP app used by the background RAG worker."""

    application = FastAPI(docs_url=None, redoc_url=None, openapi_url=None)

    @application.get("/healthz", response_model=HealthResponse)
    async def health() -> HealthResponse:
        return HealthResponse()

    @application.get(
        "/readyz",
        response_model=ReadinessResponse,
        responses={503: {"model": ReadinessResponse}},
    )
    async def readiness(response: Response) -> ReadinessResponse:
        checks_list = await asyncio.gather(
            _safe_check(database.check, "database"),
            _safe_check(clients.check_embedding, "embedding"),
            _safe_check(clients.check_qdrant, "qdrant"),
            _safe_check(clients.check_minio, "minio"),
        )
        checks = dict(zip(("database", "embedding", "qdrant", "minio"), checks_list, strict=True))
        ready = all(value == "ready" for value in checks.values())
        if not ready:
            response.status_code = 503
        return ReadinessResponse(status="ready" if ready else "not_ready", checks=checks)

    return application


__all__ = ["create_health_app"]
