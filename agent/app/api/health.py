"""探针仅返回稳定状态，不包含数据库或部署凭据。"""

from typing import Annotated, Literal

from fastapi import APIRouter, Depends, Response
from pydantic import BaseModel

from app.core.database import DatabaseStatus
from app.core.resources import AppResources, get_resources

router = APIRouter()


class HealthResponse(BaseModel):
    service: Literal["agent-service"] = "agent-service"
    status: Literal["ok"] = "ok"


class ReadinessResponse(BaseModel):
    service: Literal["agent-service"] = "agent-service"
    status: Literal["ready", "not_ready"]
    checks: dict[str, DatabaseStatus]


@router.get("/healthz", response_model=HealthResponse)
async def health() -> HealthResponse:
    return HealthResponse()


@router.get(
    "/readyz",
    response_model=ReadinessResponse,
    responses={503: {"model": ReadinessResponse}},
)
async def readiness(
    response: Response,
    resources: Annotated[AppResources, Depends(get_resources)],
) -> ReadinessResponse:
    status = await resources.database.check()
    if status != "ready":
        response.status_code = 503
    return ReadinessResponse(
        status="ready" if status == "ready" else "not_ready",
        checks={"database": status},
    )
