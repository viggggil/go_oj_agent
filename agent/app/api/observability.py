"""管理员 Agent Chat Run 安全查询。"""

import json
from typing import Literal
from urllib.parse import urlencode
from uuid import UUID

from fastapi import APIRouter, Request
from fastapi.encoders import jsonable_encoder
from fastapi.responses import JSONResponse
from pydantic import BaseModel, ConfigDict, Field, ValidationError
from sqlalchemy.exc import SQLAlchemyError

from app.core.auth import InvalidDelegation
from app.core.operations import ADMIN_ROLES, operation
from app.core.resources import get_resources
from app.core.settings import Settings
from app.models.configuration import ConfigError
from app.storage.repository import RunNotFound

router = APIRouter()


class RunQuery(BaseModel):
    model_config = ConfigDict(extra="forbid")
    page: int = Field(default=1, ge=1, le=10000)
    page_size: int = Field(default=20, ge=1, le=100)
    status: Literal["RUNNING", "COMPLETED", "FAILED", "CANCELLED", "INTERRUPTED"] | None = None


async def handle(request: Request) -> JSONResponse:
    resources = get_resources(request)
    try:
        if resources.store is None or resources.verifier is None:
            raise ConfigError("AGENT_UNAVAILABLE", 503)
        expected = operation(request.method, request.url.path)
        principal = resources.verifier.verify(request.headers.get("authorization"), expected)
        if not principal.roles.intersection(ADMIN_ROLES):
            raise ConfigError("AGENT_FORBIDDEN", 403)
        if request.headers.get("content-length", "0") != "0":
            raise ConfigError("AGENT_INVALID_ARGUMENT")
        async for chunk in request.stream():
            if chunk:
                raise ConfigError("AGENT_INVALID_ARGUMENT")
        if request.path_params.get("run_id"):
            run_id = str(UUID(request.path_params["run_id"]))
            result = await resources.store.get_admin_run(run_id)
            result = with_links(result, resources.settings)
        else:
            parameters = dict(request.query_params)
            if len(parameters) != len(request.query_params.multi_items()):
                raise ConfigError("AGENT_INVALID_ARGUMENT")
            query = RunQuery.model_validate(parameters)
            result = await resources.store.list_admin_runs(
                page=query.page, page_size=query.page_size, status=query.status
            )
            result["items"] = [with_links(item, resources.settings) for item in result["items"]]
        return JSONResponse(content=jsonable_encoder(result), headers={"Cache-Control": "no-store"})
    except InvalidDelegation:
        return JSONResponse(status_code=401, content={"code": "AGENT_UNAUTHENTICATED"})
    except RunNotFound:
        return JSONResponse(status_code=404, content={"code": "AGENT_RUN_NOT_FOUND"})
    except ConfigError as exc:
        return JSONResponse(status_code=exc.status, content={"code": exc.code})
    except (ValidationError, ValueError):
        return JSONResponse(status_code=400, content={"code": "AGENT_INVALID_ARGUMENT"})
    except (SQLAlchemyError, TimeoutError):
        return JSONResponse(status_code=503, content={"code": "AGENT_UNAVAILABLE"})


async def overview(request: Request) -> JSONResponse:
    resources = get_resources(request)
    try:
        if resources.store is None or resources.verifier is None:
            raise ConfigError("AGENT_UNAVAILABLE", 503)
        expected = operation("GET", request.url.path)
        principal = resources.verifier.verify(request.headers.get("authorization"), expected)
        if not principal.roles.intersection(ADMIN_ROLES):
            raise ConfigError("AGENT_FORBIDDEN", 403)
        if request.headers.get("content-length", "0") != "0":
            raise ConfigError("AGENT_INVALID_ARGUMENT")
        async for chunk in request.stream():
            if chunk:
                raise ConfigError("AGENT_INVALID_ARGUMENT")
        result = await resources.store.get_admin_run_overview()
        return JSONResponse(content=jsonable_encoder(result), headers={"Cache-Control": "no-store"})
    except InvalidDelegation:
        return JSONResponse(status_code=401, content={"code": "AGENT_UNAUTHENTICATED"})
    except ConfigError as exc:
        return JSONResponse(status_code=exc.status, content={"code": exc.code})
    except (ValidationError, ValueError):
        return JSONResponse(status_code=400, content={"code": "AGENT_INVALID_ARGUMENT"})
    except (SQLAlchemyError, TimeoutError):
        return JSONResponse(status_code=503, content={"code": "AGENT_UNAVAILABLE"})


def with_links(item: dict[str, object], settings: Settings) -> dict[str, object]:
    trace_id = item.get("trace_id")
    grafana = settings.observability_grafana_url
    query = urlencode(
        {
            "left": json.dumps(
                {
                    "datasource": "tempo",
                    "queries": [{"queryType": "traceql", "query": str(trace_id)}],
                    "range": {"from": "now-1h", "to": "now"},
                },
                separators=(",", ":"),
            )
        }
    )
    item["tempo_url"] = f"{grafana}/explore?{query}" if trace_id and grafana else None
    exported_project_id = item.pop("langsmith_project_id", None)
    project = settings.observability_langsmith_project_url
    item["langsmith_url"] = (
        f"{project}/r/{item['run_id']}"
        if item.get("langsmith_export_status") == "EXPORTED"
        and exported_project_id == str(settings.langsmith_project_id)
        and project
        else None
    )
    return item


router.add_api_route("/api/v1/admin/agent/observability/runs", handle, methods=["GET"])
router.add_api_route(
    "/api/v1/admin/agent/observability/runs/{run_id:uuid}", handle, methods=["GET"]
)
router.add_api_route("/api/v1/admin/agent/observability/overview", overview, methods=["GET"])
