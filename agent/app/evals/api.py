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
from app.evals.models import (
    EvalDatasetInvalid,
    EvalDatasetUpload,
    EvalModelReviewPage,
    EvalModelReviewRetry,
    EvalPage,
    EvalResultPage,
    EvalReviewPage,
    EvalReviewWrite,
    EvalRunCancel,
    EvalRunCreate,
    EvalRunPage,
)
from app.models.configuration import ConfigError

router = APIRouter()
PREFIX = "/api/v1/admin/agent/evals"


async def handle(request: Request) -> JSONResponse:
    resources = get_resources(request)
    try:
        service = getattr(resources, "evals", None)
        runner = getattr(resources, "eval_runner", None)
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
                if request.url.path.endswith("/model-reviews"):
                    result = await service.model_reviews_page(
                        str(request.path_params["result_id"]),
                        EvalModelReviewPage.model_validate(parameters),
                    )
                elif request.url.path.endswith("/reviews"):
                    result = await service.reviews_page(
                        str(request.path_params["result_id"]),
                        EvalReviewPage.model_validate(parameters),
                    )
                elif request.path_params.get("run_id"):
                    run_id = str(request.path_params["run_id"])
                    if request.path_params.get("result_id"):
                        if parameters:
                            raise ConfigError("AGENT_INVALID_ARGUMENT")
                        result = {
                            "result": await service.get_result(
                                run_id, str(request.path_params["result_id"])
                            )
                        }
                    elif request.url.path.endswith("/results"):
                        result = await service.results_page(
                            run_id, EvalResultPage.model_validate(parameters)
                        )
                    elif parameters:
                        raise ConfigError("AGENT_INVALID_ARGUMENT")
                    else:
                        result = {
                            "run": await service.get_run(run_id),
                            "metrics": await service.run_metrics(run_id),
                        }
                elif request.path_params.get("dataset_id"):
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
                    if request.url.path.endswith("/runs"):
                        result = await service.page_runs(EvalRunPage.model_validate(parameters))
                    else:
                        result = await service.page(EvalPage.model_validate(parameters))
            else:
                if str(UUID(principal.request_id)) != principal.request_id:
                    raise ConfigError("AGENT_INVALID_ARGUMENT")
                if request.url.path.endswith("/review"):
                    if parameters:
                        raise ConfigError("AGENT_INVALID_ARGUMENT")
                    review_body = cast(
                        EvalReviewWrite, await read_body(request, EvalReviewWrite, 32_000)
                    )
                    result = await service.save_review(
                        str(request.path_params["result_id"]), review_body, principal
                    )
                elif request.url.path.endswith("/review-agent"):
                    if parameters:
                        raise ConfigError("AGENT_INVALID_ARGUMENT")
                    if runner is None:
                        raise ConfigError("AGENT_EVAL_UNAVAILABLE", 503)
                    retry_body = cast(
                        EvalModelReviewRetry,
                        await read_body(request, EvalModelReviewRetry, 8_000),
                    )
                    result = await runner.retry_model_review(
                        str(request.path_params["result_id"]),
                        retry_body.expected_attempt,
                        principal,
                    )
                elif request.url.path.endswith("/runs"):
                    if parameters:
                        raise ConfigError("AGENT_INVALID_ARGUMENT")
                    if runner is None:
                        raise ConfigError("AGENT_EVAL_UNAVAILABLE", 503)
                    run_body = cast(EvalRunCreate, await read_body(request, EvalRunCreate, 32_000))
                    result = await runner.create(run_body, principal)
                elif request.path_params.get("run_id"):
                    if not request.url.path.endswith("/cancel"):
                        raise ConfigError("AGENT_INVALID_ARGUMENT")
                    if set(parameters) - {"version"} or len(parameters) > 1:
                        raise ConfigError("AGENT_INVALID_ARGUMENT")
                    if "version" not in parameters:
                        raise ConfigError("AGENT_INVALID_ARGUMENT")
                    try:
                        expected_version = EvalRunCancel(
                            expected_version=int(parameters["version"])
                        ).expected_version
                    except ValueError:
                        raise ConfigError("AGENT_INVALID_ARGUMENT") from None
                    if runner is None:
                        raise ConfigError("AGENT_EVAL_UNAVAILABLE", 503)
                    result = {
                        "run": await runner.cancel(
                            str(request.path_params["run_id"]),
                            expected_version,
                            principal.request_id,
                        )
                    }
                else:
                    if parameters:
                        raise ConfigError("AGENT_INVALID_ARGUMENT")
                    dataset_body = cast(
                        EvalDatasetUpload, await read_body(request, EvalDatasetUpload, 950_000)
                    )
                    result = await service.upload(principal, dataset_body)
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
router.add_api_route(f"{PREFIX}/runs", handle, methods=["GET", "POST"])
router.add_api_route(f"{PREFIX}/runs/{{run_id:uuid}}", handle, methods=["GET"])
router.add_api_route(f"{PREFIX}/runs/{{run_id:uuid}}/cancel", handle, methods=["POST"])
router.add_api_route(f"{PREFIX}/runs/{{run_id:uuid}}/results", handle, methods=["GET"])

router.add_api_route(
    f"{PREFIX}/runs/{{run_id:uuid}}/results/{{result_id:uuid}}", handle, methods=["GET"]
)

router.add_api_route(f"{PREFIX}/results/{{result_id:uuid}}/review", handle, methods=["POST"])
router.add_api_route(f"{PREFIX}/results/{{result_id:uuid}}/reviews", handle, methods=["GET"])
router.add_api_route(f"{PREFIX}/results/{{result_id:uuid}}/review-agent", handle, methods=["POST"])
router.add_api_route(f"{PREFIX}/results/{{result_id:uuid}}/model-reviews", handle, methods=["GET"])
