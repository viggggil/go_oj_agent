"""管理员 Eval 数据集 API。"""

import asyncio
from typing import cast
from uuid import UUID

from fastapi import APIRouter, Request
from fastapi.encoders import jsonable_encoder
from fastapi.responses import JSONResponse
from pydantic import ValidationError
from sqlalchemy.exc import SQLAlchemyError

from app.api.management import read_body
from app.core.auth import InvalidDelegation
from app.core.operations import ADMIN_ROLES, operation
from app.core.resources import get_resources
from app.evals.models import EvalDatasetInvalid, EvalDatasetUpload, EvalPage
from app.models.configuration import ConfigError

router = APIRouter()
PREFIX = "/api/v1/admin/agent/evals"


async def handle(request: Request) -> JSONResponse:
    resources = get_resources(request)
    try:
        service = getattr(resources, "evals", None)
        if not resources.settings.admin_enabled or service is None:
            raise ConfigError("AGENT_MANAGEMENT_DISABLED", 503)
        assert resources.verifier is not None
        principal = resources.verifier.verify(
            request.headers.get("authorization"), operation(request.method, request.url.path)
        )
        if not principal.roles.intersection(ADMIN_ROLES):
            raise ConfigError("AGENT_FORBIDDEN", 403)
        parameters = dict(request.query_params)
        if len(parameters) != len(request.query_params.multi_items()):
            raise ConfigError("AGENT_INVALID_ARGUMENT")
        async with asyncio.timeout(25):
            if request.method == "GET":
                if parameters.get("dataset_id"):
                    raise ConfigError("AGENT_INVALID_ARGUMENT")
                if request.path_params.get("dataset_id"):
                    if parameters and not request.url.path.endswith("/cases"):
                        raise ConfigError("AGENT_INVALID_ARGUMENT")
                    dataset_id = str(request.path_params["dataset_id"])
                    if request.url.path.endswith("/cases"):
                        result = await service.cases_page(
                            dataset_id, EvalPage.model_validate(parameters)
                        )
                    else:
                        result = await service.detail(dataset_id)
                else:
                    result = await service.page(EvalPage.model_validate(parameters))
            else:
                if parameters or str(UUID(principal.request_id)) != principal.request_id:
                    raise ConfigError("AGENT_INVALID_ARGUMENT")
                body = cast(EvalDatasetUpload, await read_body(request, EvalDatasetUpload, 950_000))
                result = await service.upload(principal, body)
        return JSONResponse(content=jsonable_encoder(result), headers={"Cache-Control": "no-store"})
    except InvalidDelegation:
        return JSONResponse(status_code=401, content={"code": "AGENT_UNAUTHENTICATED"})
    except EvalDatasetInvalid as exc:
        return JSONResponse(
            status_code=400,
            content={
                "code": "AGENT_EVAL_DATASET_INVALID",
                "message": str(exc),
                **({"line": exc.line} if exc.line is not None else {}),
            },
        )
    except ConfigError as exc:
        return JSONResponse(status_code=exc.status, content={"code": exc.code})
    except (ValidationError, ValueError):
        return JSONResponse(status_code=400, content={"code": "AGENT_INVALID_ARGUMENT"})
    except (SQLAlchemyError, TimeoutError):
        return JSONResponse(status_code=503, content={"code": "AGENT_UNAVAILABLE"})


router.add_api_route(f"{PREFIX}/datasets", handle, methods=["GET", "POST"])
router.add_api_route(f"{PREFIX}/datasets/{{dataset_id:uuid}}", handle, methods=["GET"])
router.add_api_route(f"{PREFIX}/datasets/{{dataset_id:uuid}}/cases", handle, methods=["GET"])
