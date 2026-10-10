"""经 Gateway 委托的管理 HTTP 与公开目录，不透传原始异常。"""

import asyncio
from typing import Any, cast
from uuid import UUID

from fastapi import APIRouter, Request
from fastapi.encoders import jsonable_encoder
from fastapi.responses import JSONResponse
from pydantic import BaseModel, ValidationError
from sqlalchemy.exc import SQLAlchemyError

from app.core.auth import InvalidDelegation
from app.core.management import ManagementService, summary
from app.core.operations import ADMIN_ROLES, operation
from app.core.resources import get_resources
from app.models.admin import (
    CreateConfiguration,
    ExpectedConfiguration,
    PageQuery,
    ReplaceConfiguration,
    RestoreConfiguration,
)
from app.models.configuration import ConfigError, ConfigKind, ConfigWrite
from app.models.runtime import Principal

router = APIRouter()
KINDS: dict[str, ConfigKind] = {"agents": "agent", "prompts": "prompt", "skills": "skill"}


async def read_body(request: Request, model: type[BaseModel], maximum: int) -> Any:
    if (
        request.headers.get("content-encoding", "identity") != "identity"
        or request.headers.get("content-type", "").split(";", 1)[0].strip().lower()
        != "application/json"
    ):
        raise ConfigError("AGENT_UNSUPPORTED_MEDIA_TYPE", 415)
    data = bytearray()
    async for chunk in request.stream():
        if len(data) + len(chunk) > maximum:
            raise ConfigError("AGENT_REQUEST_TOO_LARGE", 413)
        data.extend(chunk)
    return model.model_validate_json(data)


async def handle(request: Request) -> JSONResponse:
    resources = get_resources(request)
    service: ManagementService | None = resources.management
    try:
        expected = operation(request.method, request.url.path)
        admin = request.url.path.startswith("/api/v1/admin/")
        if admin and not resources.settings.admin_enabled:
            raise ConfigError("AGENT_MANAGEMENT_DISABLED", 503)
        if resources.verifier is None:
            raise ConfigError("AGENT_MANAGEMENT_DISABLED", 503)
        principal = resources.verifier.verify(request.headers.get("authorization"), expected)
        if admin and not principal.roles.intersection(ADMIN_ROLES):
            raise ConfigError("AGENT_FORBIDDEN", 403)
        parameters = dict(request.query_params)
        if len(parameters) != len(request.query_params.multi_items()):
            raise ConfigError("AGENT_INVALID_ARGUMENT")
        query = PageQuery.model_validate(parameters)
        if request.method == "GET":
            if request.headers.get("content-length", "0") != "0":
                raise ConfigError("AGENT_INVALID_ARGUMENT")
            if (
                request.path_params.get("identifier")
                or request.path_params.get("agent_key")
                or request.path_params.get("key")
                and not request.path_params.get("action")
            ) and parameters:
                raise ConfigError("AGENT_INVALID_ARGUMENT")
        if not admin and (query.state != "active"):
            raise ConfigError("AGENT_INVALID_ARGUMENT")
        if query.state != "active" and (
            request.url.path.endswith(("/model-options", "/tools", "/versions"))
        ):
            raise ConfigError("AGENT_INVALID_ARGUMENT")
        async with asyncio.timeout(resources.settings.preflight_timeout_seconds):
            if request.method == "GET":
                async for chunk in request.stream():
                    if chunk:
                        raise ConfigError("AGENT_INVALID_ARGUMENT")
            if not admin and resources.settings.config_mode == "demo":
                if request.path_params.get("agent_key"):
                    raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
                return JSONResponse(
                    content={
                        "items": [],
                        "page": {"page": query.page, "page_size": query.page_size, "total": 0},
                    },
                    headers={"Cache-Control": "no-store"},
                )
            if service is None:
                raise ConfigError("AGENT_MANAGEMENT_DISABLED", 503)
            result = await dispatch(request, service, principal, query)
        return JSONResponse(content=jsonable_encoder(result), headers={"Cache-Control": "no-store"})
    except InvalidDelegation:
        return JSONResponse(status_code=401, content={"code": "AGENT_UNAUTHENTICATED"})
    except ConfigError as exc:
        return JSONResponse(status_code=exc.status, content={"code": exc.code})
    except (ValidationError, ValueError):
        return JSONResponse(status_code=400, content={"code": "AGENT_INVALID_ARGUMENT"})
    except (SQLAlchemyError, TimeoutError):
        return JSONResponse(status_code=503, content={"code": "AGENT_UNAVAILABLE"})


async def dispatch(
    request: Request, service: ManagementService, principal: Principal, query: PageQuery
) -> dict[str, Any]:
    path, method = request.url.path, request.method
    if not path.startswith("/api/v1/admin/"):
        key = cast(str | None, request.path_params.get("agent_key"))
        return await service.catalog(query, principal, key)
    if path.endswith("/model-options"):
        return await service.models(query)
    if path.endswith("/tools"):
        values = [item for item in service.registry.catalog() if query.search in item["name"]]
        offset = (query.page - 1) * query.page_size
        return {
            "items": values[offset : offset + query.page_size],
            "page": {"page": query.page, "page_size": query.page_size, "total": len(values)},
        }
    kind = KINDS[request.path_params["resource"]]
    key = request.path_params.get("key")
    action = request.path_params.get("action")
    identifier = request.path_params.get("identifier")
    if method == "GET":
        if key is None or action == "versions" and identifier is None:
            return await service.list(kind, query, key)
        return await service.detail(kind, key, identifier)
    if request.query_params:
        raise ConfigError("AGENT_INVALID_ARGUMENT")
    # 写操作必须携带调用方保留的 UUID；随机生成的 Gateway request_id 不能作为用户重试编号。
    if str(UUID(principal.request_id)) != principal.request_id:
        raise ConfigError("AGENT_INVALID_ARGUMENT")
    maximum = service.settings.max_request_bytes
    if key is None:
        body = cast(CreateConfiguration, await read_body(request, CreateConfiguration, maximum))
        return await service.save(kind, body.key, body.content, principal)
    if method == "PUT":
        replacement = cast(
            ReplaceConfiguration, await read_body(request, ReplaceConfiguration, maximum)
        )
        return await service.save(
            kind, key, replacement.content, principal, replacement.expected_id
        )
    if action == "restore":
        restore = cast(
            RestoreConfiguration, await read_body(request, RestoreConfiguration, maximum)
        )
        source = await service.query.detail(kind, key, str(restore.source_id))
        return await service.save(
            kind,
            key,
            source.content.model_dump(mode="json"),
            principal,
            restore.expected_id,
            restore.source_id,
        )
    state = cast(ExpectedConfiguration, await read_body(request, ExpectedConfiguration, maximum))
    write = ConfigWrite(
        kind=kind,
        key=key,
        actor_id=principal.user_id,
        request_id=principal.request_id,
        expected_id=state.expected_id,
    )
    from app.storage.configuration import ConfigAction

    value = await service.store.change_state(write, cast(ConfigAction, action))
    return {"configuration": summary(value, full=True)}


router.add_api_route("/api/v1/agent/agents", handle, methods=["GET"])
router.add_api_route("/api/v1/agent/agents/{agent_key}/skills", handle, methods=["GET"])
router.add_api_route("/api/v1/admin/agent/model-options", handle, methods=["GET"])
router.add_api_route("/api/v1/admin/agent/tools", handle, methods=["GET"])
# 操作白名单进一步校验 resource/key/action；不是任意路径代理。
router.add_api_route("/api/v1/admin/agent/{resource}", handle, methods=["GET", "POST"])
router.add_api_route("/api/v1/admin/agent/{resource}/{key}", handle, methods=["GET", "PUT"])
router.add_api_route(
    "/api/v1/admin/agent/{resource}/{key}/{action}", handle, methods=["GET", "POST"]
)
router.add_api_route(
    "/api/v1/admin/agent/{resource}/{key}/{action}/{identifier}", handle, methods=["GET"]
)
