"""知识库管理及运行证据仅对管理员开放；模型使用独立内部检索工具。"""

import asyncio
from typing import Any, cast
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
from app.models.configuration import ConfigError
from app.rag.models import (
    BaseInput,
    Page,
    PreviewInput,
    RenameInput,
    SearchInput,
    StateInput,
    Upload,
)
from app.rag.service import KnowledgeService
from app.rag.store import public

router = APIRouter()


async def handle(request: Request) -> JSONResponse:
    resources = get_resources(request)
    try:
        if not resources.settings.admin_enabled or resources.rag is None:
            raise ConfigError("AGENT_MANAGEMENT_DISABLED", 503)
        assert resources.verifier is not None
        principal = resources.verifier.verify(
            request.headers.get("authorization"),
            operation(request.method, request.url.path),
        )
        if not principal.roles.intersection(ADMIN_ROLES):
            raise ConfigError("AGENT_FORBIDDEN", 403)
        service: KnowledgeService = resources.rag
        async with asyncio.timeout(25):
            parameters = dict(request.query_params)
            if len(parameters) != len(request.query_params.multi_items()):
                raise ConfigError("AGENT_INVALID_ARGUMENT")
            if request.method == "GET":
                async for chunk in request.stream():
                    if chunk:
                        raise ConfigError("AGENT_INVALID_ARGUMENT")
                page = Page.model_validate(parameters)
                doc_id = request.path_params.get("document_id")
                if run_id := request.path_params.get("run_id"):
                    if parameters:
                        raise ConfigError("AGENT_INVALID_ARGUMENT")
                    result = {"evidence": await service.store.evidence(str(run_id))}
                elif base_id := request.path_params.get("base_id"):
                    if parameters:
                        raise ConfigError("AGENT_INVALID_ARGUMENT")
                    result = await service.store.base_detail(str(base_id))
                elif doc_id and request.url.path.endswith("/versions"):
                    if page.base_id:
                        raise ConfigError("AGENT_INVALID_ARGUMENT")
                    result = await service.store.page("documents", page, str(doc_id))
                elif doc_id:
                    if parameters:
                        raise ConfigError("AGENT_INVALID_ARGUMENT")
                    row = await service.store.detail(str(doc_id))
                    result = {
                        "document": public(row)
                        | {
                            "content": await service.clients.get(row["object_key"]),
                        }
                    }
                else:
                    result = await service.store.page(request.url.path.rsplit("/", 1)[1], page)
            else:
                if parameters or str(UUID(principal.request_id)) != principal.request_id:
                    raise ConfigError("AGENT_INVALID_ARGUMENT")
                result = await dispatch(request, service, principal)
        return JSONResponse(content=jsonable_encoder(result), headers={"Cache-Control": "no-store"})
    except InvalidDelegation:
        return JSONResponse(status_code=401, content={"code": "AGENT_UNAUTHENTICATED"})
    except ConfigError as exc:
        return JSONResponse(status_code=exc.status, content={"code": exc.code})
    except (ValidationError, ValueError):
        return JSONResponse(status_code=400, content={"code": "AGENT_INVALID_ARGUMENT"})
    except (SQLAlchemyError, TimeoutError):
        return JSONResponse(status_code=503, content={"code": "AGENT_UNAVAILABLE"})


async def dispatch(request: Request, service: KnowledgeService, principal: Any) -> dict[str, Any]:
    path = request.url.path
    maximum = 262_144
    if path.endswith("/knowledge-bases"):
        body = cast(BaseInput, await read_body(request, BaseInput, maximum))
        return await service.store.create_base(principal, body.name)
    if path.endswith("/rename"):
        rename = cast(RenameInput, await read_body(request, RenameInput, maximum))
        return await service.store.rename_base(
            principal, str(request.path_params["base_id"]), rename.name, rename.expected_name
        )
    if path.endswith("/preview"):
        preview = cast(PreviewInput, await read_body(request, PreviewInput, maximum))
        chunks = await service.clients.split(preview.content)
        start = (preview.page - 1) * 20
        return {
            "chunks": chunks[start : start + 20],
            "page": {"page": preview.page, "page_size": 20, "total": len(chunks)},
        }
    if path.endswith("/documents"):
        upload = cast(Upload, await read_body(request, Upload, maximum))
        return await service.store.upload(principal, upload, service.clients)
    if path.endswith("/search"):
        search = cast(SearchInput, await read_body(request, SearchInput, maximum))
        return {"matches": await service.search(search)}
    body_state = cast(StateInput, await read_body(request, StateInput, maximum))
    return await service.store.change(
        principal,
        str(request.path_params["document_id"]),
        str(body_state.expected_version),
        path.rsplit("/", 1)[1],
    )


PREFIX = "/api/v1/admin/agent/rag"
router.add_api_route(f"{PREFIX}/knowledge-bases", handle, methods=["GET", "POST"])
router.add_api_route(f"{PREFIX}/knowledge-bases/{{base_id:uuid}}", handle, methods=["GET"])
router.add_api_route(f"{PREFIX}/knowledge-bases/{{base_id:uuid}}/rename", handle, methods=["POST"])
router.add_api_route(f"{PREFIX}/documents", handle, methods=["GET", "POST"])
router.add_api_route(f"{PREFIX}/documents/{{document_id:uuid}}", handle, methods=["GET"])
router.add_api_route(f"{PREFIX}/documents/{{document_id:uuid}}/versions", handle, methods=["GET"])
# UUID converter 的值在处理时规范化为 str。
for action in ("enable", "archive", "retry"):
    router.add_api_route(
        f"{PREFIX}/documents/{{document_id:uuid}}/{action}",
        handle,
        methods=["POST"],
        name=action,
    )
router.add_api_route(f"{PREFIX}/search", handle, methods=["POST"])
router.add_api_route(f"{PREFIX}/preview", handle, methods=["POST"])
router.add_api_route(f"{PREFIX}/runs/{{run_id:uuid}}/evidence", handle, methods=["GET"])
