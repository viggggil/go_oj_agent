"""可信 Chat 入口与有界 SSE；响应结束时关闭 AcceptedRun。"""

import asyncio
from typing import Annotated

from fastapi import APIRouter, Depends, Request
from fastapi.responses import JSONResponse, Response
from pydantic import ValidationError
from sqlalchemy.exc import SQLAlchemyError
from starlette.types import Receive, Scope, Send

from app.core.auth import DelegationVerifier, InvalidDelegation
from app.core.resources import AppResources, get_resources
from app.core.settings import Settings
from app.graphs.service import AcceptedRun, RunService, RuntimeFailure
from app.models.runtime import ChatRequest, StreamEvent
from app.storage.repository import ActiveRunConflict, ConversationNotFound

router = APIRouter()


def encode_event(event: StreamEvent) -> bytes:
    return f"event: {event.type}\ndata: {event.model_dump_json()}\n\n".encode()


class ChatRejected(Exception):
    def __init__(self, status: int, code: str) -> None:
        self.status = status
        self.code = code


class RunLease:
    def __init__(self, controller: "ChatController") -> None:
        self.controller = controller
        self.task = asyncio.current_task()

    def release(self) -> None:
        self.controller.leases.discard(self)


class ChatController:
    def __init__(
        self, settings: Settings, verifier: DelegationVerifier, service: RunService
    ) -> None:
        self.settings = settings
        self.verifier = verifier
        self.service = service
        self.leases: set[RunLease] = set()
        self.closing = False

    def acquire(self) -> RunLease:
        if self.closing or len(self.leases) >= self.settings.max_concurrent_runs:
            raise ChatRejected(503, "AGENT_CAPACITY_EXCEEDED")
        lease = RunLease(self)
        self.leases.add(lease)
        return lease

    async def close(self) -> None:
        self.closing = True
        tasks = {lease.task for lease in self.leases if lease.task is not None}
        tasks.discard(asyncio.current_task())
        for task in tasks:
            task.cancel()
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)


class ChatStreamResponse(Response):
    """独立监听断连，心跳不会取消正在等待的 Runtime 事件。"""

    def __init__(self, accepted: AcceptedRun, lease: RunLease, settings: Settings) -> None:
        super().__init__(
            media_type="text/event-stream",
            headers={
                "Cache-Control": "no-cache, no-transform",
                "X-Accel-Buffering": "no",
                "X-Agent-Run-ID": str(accepted.state.run_id),
                "X-Agent-Conversation-ID": str(accepted.state.conversation_id),
            },
        )
        del self.headers["content-length"]
        self.accepted = accepted
        self.lease = lease
        self.settings = settings

    async def __call__(self, scope: Scope, receive: Receive, send: Send) -> None:
        async def disconnect() -> None:
            while (await receive())["type"] != "http.disconnect":
                pass

        body = asyncio.create_task(self._stream(send))
        disconnected = asyncio.create_task(disconnect())
        try:
            completed, _ = await asyncio.wait(
                {body, disconnected}, return_when=asyncio.FIRST_COMPLETED
            )
            if body in completed:
                # OSError 是 ASGI 服务器通知断连的另一条路径。
                try:
                    await body
                except (OSError, TimeoutError):
                    pass
        finally:

            async def cleanup() -> None:
                try:
                    async with asyncio.timeout(self.settings.shutdown_timeout_seconds):
                        body.cancel()
                        disconnected.cancel()
                        await asyncio.gather(body, disconnected, return_exceptions=True)
                        await self.accepted.aclose()
                finally:
                    self.lease.release()

            closing = asyncio.create_task(cleanup())
            try:
                await asyncio.shield(closing)
            except asyncio.CancelledError:
                # cleanup 有独立预算，外层再次取消也不能跳过 Run 回收。
                await asyncio.shield(closing)

    async def _stream(self, send: Send) -> None:
        next_event: asyncio.Future[StreamEvent] | None = None
        try:
            await asyncio.wait_for(
                send({"type": "http.response.start", "status": 200, "headers": self.raw_headers}),
                self.settings.sse_write_timeout_seconds,
            )
            while True:
                next_event = asyncio.ensure_future(anext(self.accepted.events))
                while not next_event.done():
                    completed, _ = await asyncio.wait(
                        {next_event}, timeout=self.settings.sse_heartbeat_seconds
                    )
                    if not completed:
                        await self._send(send, b": heartbeat\n\n")
                event = await next_event
                await self._send(send, encode_event(event))
                if event.type in {"done", "error"}:
                    await asyncio.wait_for(
                        send({"type": "http.response.body", "body": b"", "more_body": False}),
                        self.settings.sse_write_timeout_seconds,
                    )
                    return
        finally:
            if next_event is not None:
                next_event.cancel()
                await asyncio.gather(next_event, return_exceptions=True)

    async def _send(self, send: Send, content: bytes) -> None:
        await asyncio.wait_for(
            send({"type": "http.response.body", "body": content, "more_body": True}),
            self.settings.sse_write_timeout_seconds,
        )


async def read_chat_request(request: Request, maximum: int) -> ChatRequest:
    if request.headers.get("content-encoding", "identity") != "identity":
        raise ChatRejected(415, "AGENT_UNSUPPORTED_MEDIA_TYPE")
    if (
        request.headers.get("content-type", "").split(";", 1)[0].strip().lower()
        != "application/json"
    ):
        raise ChatRejected(415, "AGENT_UNSUPPORTED_MEDIA_TYPE")
    body = bytearray()
    async for chunk in request.stream():
        if len(body) + len(chunk) > maximum:
            raise ChatRejected(413, "AGENT_REQUEST_TOO_LARGE")
        body.extend(chunk)
    try:
        return ChatRequest.model_validate_json(body)
    except ValidationError:
        # 校验错误不回显用户消息、源码或身份字段。
        raise ChatRejected(400, "AGENT_INVALID_ARGUMENT") from None


@router.post("/api/v1/agent/chat")
async def chat(
    request: Request, resources: Annotated[AppResources, Depends(get_resources)]
) -> Response:
    controller = resources.chat
    if controller is None:
        return JSONResponse(status_code=503, content={"code": "AGENT_CHAT_DISABLED"})
    lease: RunLease | None = None
    transferred = False
    try:
        principal = controller.verifier.verify(request.headers.get("authorization"))
        lease = controller.acquire()
        async with asyncio.timeout(resources.settings.preflight_timeout_seconds):
            chat_request = await read_chat_request(request, resources.settings.max_request_bytes)
            accepted = await controller.service.accept(chat_request, principal)
        # 首次事件前断连也必须经过 Response 的 finally 释放 Run。
        response = ChatStreamResponse(accepted, lease, resources.settings)
        transferred = True
        return response
    except InvalidDelegation:
        return JSONResponse(status_code=401, content={"code": "AGENT_UNAUTHENTICATED"})
    except ConversationNotFound:
        return JSONResponse(status_code=404, content={"code": "AGENT_CONVERSATION_NOT_FOUND"})
    except ActiveRunConflict:
        return JSONResponse(status_code=409, content={"code": "AGENT_ACTIVE_RUN_CONFLICT"})
    except RuntimeFailure as exc:
        status = 404 if exc.code == "AGENT_CONFIGURATION_NOT_FOUND" else 409
        return JSONResponse(status_code=status, content={"code": exc.code})
    except ChatRejected as exc:
        return JSONResponse(status_code=exc.status, content={"code": exc.code})
    except (SQLAlchemyError, TimeoutError):
        return JSONResponse(status_code=503, content={"code": "AGENT_UNAVAILABLE"})
    finally:
        if lease is not None and not transferred:
            lease.release()
