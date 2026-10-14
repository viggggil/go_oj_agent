"""本人会话读取；沿用具体操作委托，禁止暴露运行快照及工具正文。"""

import asyncio

from fastapi import APIRouter, Request
from fastapi.encoders import jsonable_encoder
from fastapi.responses import JSONResponse
from pydantic import BaseModel, ConfigDict, Field, ValidationError
from sqlalchemy.exc import SQLAlchemyError

from app.core.auth import InvalidDelegation
from app.core.operations import operation
from app.core.resources import get_resources
from app.models.configuration import ConfigError
from app.storage.repository import ConversationNotFound

router = APIRouter()


class ConversationQuery(BaseModel):
    model_config = ConfigDict(extra="forbid")
    page: int = Field(default=1, ge=1, le=10000)


class MessageQuery(BaseModel):
    model_config = ConfigDict(extra="forbid")
    before: int | None = Field(default=None, ge=1, le=9223372036854775807)


async def handle(request: Request) -> JSONResponse:
    resources = get_resources(request)
    try:
        if resources.verifier is None or resources.store is None:
            raise ConfigError("AGENT_UNAVAILABLE", 503)
        principal = resources.verifier.verify(
            request.headers.get("authorization"), operation("GET", request.url.path)
        )
        parameters = dict(request.query_params)
        if len(parameters) != len(request.query_params.multi_items()):
            raise ConfigError("AGENT_INVALID_ARGUMENT")
        identifier = request.path_params.get("conversation_id")
        query = (
            MessageQuery.model_validate(parameters)
            if identifier
            else ConversationQuery.model_validate(parameters)
        )
        async with asyncio.timeout(resources.settings.preflight_timeout_seconds):
            async for chunk in request.stream():
                if chunk:
                    raise ConfigError("AGENT_INVALID_ARGUMENT")
            if identifier:
                assert isinstance(query, MessageQuery)
                result = await resources.store.conversation_messages(
                    principal.user_id, identifier, query.before
                )
            else:
                assert isinstance(query, ConversationQuery)
                result = await resources.store.list_conversations(principal.user_id, query.page)
        return JSONResponse(content=jsonable_encoder(result), headers={"Cache-Control": "no-store"})
    except InvalidDelegation:
        return JSONResponse(status_code=401, content={"code": "AGENT_UNAUTHENTICATED"})
    except ConversationNotFound:
        return JSONResponse(status_code=404, content={"code": "AGENT_CONVERSATION_NOT_FOUND"})
    except ConfigError as exc:
        return JSONResponse(status_code=exc.status, content={"code": exc.code})
    except (ValidationError, ValueError):
        return JSONResponse(status_code=400, content={"code": "AGENT_INVALID_ARGUMENT"})
    except (SQLAlchemyError, TimeoutError):
        return JSONResponse(status_code=503, content={"code": "AGENT_UNAVAILABLE"})


router.add_api_route("/api/v1/agent/conversations", handle, methods=["GET"])
router.add_api_route(
    "/api/v1/agent/conversations/{conversation_id}/messages", handle, methods=["GET"]
)
