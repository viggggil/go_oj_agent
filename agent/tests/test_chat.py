"""HTTP 委托、SSE 增量/心跳/取消/写超时，不依赖真实 Provider。"""

import asyncio
import json
import time
from collections.abc import AsyncIterator
from pathlib import Path
from typing import Any

import jwt
import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from fastapi.testclient import TestClient
from pydantic import SecretStr
from sqlalchemy.exc import SQLAlchemyError
from starlette.types import Message

from app.api.chat import ChatController, ChatStreamResponse, encode_event
from app.core.auth import CHAT_OPERATION, DelegationVerifier, InvalidDelegation
from app.core.settings import ConfigurationError, Settings
from app.graphs.runtime import FakeRuntime
from app.graphs.service import RunService
from app.main import create_app
from app.models.runtime import AgentState, ChatRequest, Principal, StreamEvent
from app.storage.repository import ActiveRunConflict, ConversationNotFound
from tests.test_health import FakeProbe
from tests.test_runtime import MemoryStore, Reader


@pytest.fixture(scope="session")
def key() -> rsa.RSAPrivateKey:
    return rsa.generate_private_key(public_exponent=65537, key_size=2048)


@pytest.fixture
def settings(key: rsa.RSAPrivateKey, tmp_path: Path) -> Settings:
    path = tmp_path / "public.pem"
    path.write_bytes(
        key.public_key().public_bytes(
            serialization.Encoding.PEM, serialization.PublicFormat.SubjectPublicKeyInfo
        )
    )
    return Settings(
        chat_enabled=True,
        runtime_mode="fake",
        database_url=SecretStr("mysql+asyncmy://agent:secret@db/oj_agent"),
        gateway_public_key_file=path,
        sse_heartbeat_seconds=0.01,
        sse_write_timeout_seconds=0.05,
    )


def token(key: rsa.RSAPrivateKey, **updates: Any) -> str:
    now = int(time.time())
    claims = {
        "iss": "go-oj-gateway",
        "aud": "agent-service",
        "sub": "gateway-service",
        "iat": now,
        "exp": now + 30,
        "jti": "token-id",
        "actor_id": 7,
        "actor_roles": ["user"],
        "request_id": "request-id",
        "rpc": CHAT_OPERATION,
    }
    claims.update(updates)
    return jwt.encode(claims, key, algorithm="RS256", headers={"kid": "gateway-internal-2026-09"})


def test_trusted_delegation(settings: Settings, key: rsa.RSAPrivateKey) -> None:
    principal = DelegationVerifier(settings).verify("Bearer " + token(key))
    assert principal == Principal(user_id=7, roles=frozenset({"user"}), request_id="request-id")


@pytest.mark.parametrize(
    "updates",
    [
        {"iss": "other"},
        {"aud": "judge-service"},
        {"aud": ["agent-service"]},
        {"sub": "agent-service"},
        {"rpc": "other"},
        {"actor_id": 0},
        {"actor_id": True},
        {"actor_id": "7"},
        {"actor_id": 2**63},
        {"actor_roles": "admin"},
        {"actor_roles": [1]},
        {"actor_roles": ["x" * 65]},
        {"actor_roles": ["\ud800"]},
        {"jti": ""},
        {"request_id": ""},
        {"request_id": 7},
        {"request_id": "x" * 129},
        {"request_id": "\ud800"},
        {"iat": int(time.time()) + 30},
        {"exp": int(time.time()) - 1},
        {"exp": int(time.time()) + 120},
        {"iat": str(int(time.time()))},
    ],
)
def test_invalid_claims_are_rejected(
    settings: Settings, key: rsa.RSAPrivateKey, updates: dict[str, Any]
) -> None:
    with pytest.raises(InvalidDelegation, match="Invalid Gateway delegation"):
        DelegationVerifier(settings).verify("Bearer " + token(key, **updates))


def test_signature_algorithm_kid_and_required_claims(
    settings: Settings, key: rsa.RSAPrivateKey
) -> None:
    verifier = DelegationVerifier(settings)
    valid = token(key)
    claims = jwt.decode(valid, options={"verify_signature": False})
    invalid = [
        None,
        "",
        "Bearer ",
        "Bearer " + valid + " ",
        "Basic " + valid,
        "Bearer " + token(rsa.generate_private_key(public_exponent=65537, key_size=2048)),
        "Bearer " + jwt.encode(claims, key, algorithm="RS256", headers={"kid": "unknown"}),
        "Bearer "
        + jwt.encode(claims, "x" * 32, algorithm="HS256", headers={"kid": settings.gateway_key_id}),
        "Bearer "
        + jwt.encode(claims, "", algorithm="none", headers={"kid": settings.gateway_key_id}),
    ]
    for required in ("iss", "aud", "sub", "iat", "exp", "jti", "rpc", "actor_id", "request_id"):
        copy = dict(claims)
        del copy[required]
        invalid.append(
            "Bearer "
            + jwt.encode(copy, key, algorithm="RS256", headers={"kid": settings.gateway_key_id})
        )
    for authorization in invalid:
        with pytest.raises(InvalidDelegation):
            verifier.verify(authorization)


def test_missing_public_key_is_safe(settings: Settings) -> None:
    settings.gateway_public_key_file = Path("/private-secret/missing")
    with pytest.raises(ConfigurationError, match="^Invalid Gateway delegation public key$"):
        DelegationVerifier(settings)


def test_http_auth_validation_and_sse(settings: Settings, key: rsa.RSAPrivateKey) -> None:
    store = MemoryStore()
    service = RunService(store, FakeRuntime("中文\n多行"), Reader())
    application = create_app(
        settings, probe_factory=lambda _: FakeProbe(), run_service_factory=lambda *_: service
    )
    with TestClient(application) as http:
        assert (
            http.post(
                "/api/v1/agent/chat", json={"message": "hello"}, headers={"X-User-ID": "7"}
            ).status_code
            == 401
        )
        headers = {"Authorization": "Bearer " + token(key)}
        for payload in (
            {"message": " "},
            {"message": "hello", "user_id": 1},
            {"message": "hello", "context": {"roles": ["admin"]}},
        ):
            response = http.post("/api/v1/agent/chat", json=payload, headers=headers)
            assert response.status_code == 400
            assert response.json() == {"code": "AGENT_INVALID_ARGUMENT"}
        assert store.record is None
        response = http.post("/api/v1/agent/chat", json={"message": "介绍算法"}, headers=headers)
        assert response.status_code == 200
        assert response.headers["content-type"].startswith("text/event-stream")
        assert "content-length" not in response.headers
        frames = [
            json.loads(frame.split("data: ", 1)[1]) for frame in response.text.strip().split("\n\n")
        ]
        assert [frame["type"] for frame in frames] == ["thinking", "token", "done"]
        assert frames[1]["data"]["text"] == "中文\n多行"
        assert store.answers == ["中文\n多行"]
        assert frames[-1]["run_id"] == response.headers["x-agent-run-id"]
        assert store.record is not None and store.record.status == "COMPLETED"
        assert not application.state.resources.chat.leases


def test_body_limit_and_media_type(settings: Settings, key: rsa.RSAPrivateKey) -> None:
    settings.max_request_bytes = 1024
    store = MemoryStore()
    service = RunService(store, FakeRuntime(), Reader())
    with TestClient(
        create_app(
            settings, probe_factory=lambda _: FakeProbe(), run_service_factory=lambda *_: service
        )
    ) as http:
        headers = {"Authorization": "Bearer " + token(key)}
        assert (
            http.post(
                "/api/v1/agent/chat", json={"message": "x" * 1024}, headers=headers
            ).status_code
            == 413
        )
        assert http.post("/api/v1/agent/chat", content="{}", headers=headers).status_code == 415
        assert store.record is None


async def test_sse_first_token_heartbeat_and_disconnect(settings: Settings) -> None:
    gate = asyncio.Event()
    closed = asyncio.Event()

    class WaitingRuntime:
        async def run(self, state: AgentState) -> AsyncIterator[StreamEvent]:
            try:
                yield StreamEvent.token(state, "中文", 1)
                await gate.wait()
                yield StreamEvent.done(state, 2)
            finally:
                closed.set()

    store = MemoryStore()
    service = RunService(store, WaitingRuntime(), Reader())
    accepted = await service.accept(
        ChatRequest(message="介绍算法"), Principal(user_id=7, request_id="id")
    )
    controller = ChatController(settings, DelegationVerifier(settings), service)
    lease = controller.acquire()
    receive_messages: asyncio.Queue[Message] = asyncio.Queue()
    sent: asyncio.Queue[Message] = asyncio.Queue()

    async def send(message: Message) -> None:
        await sent.put(message)

    response = ChatStreamResponse(accepted, lease, settings)
    task = asyncio.create_task(response({}, receive_messages.get, send))
    assert (await asyncio.wait_for(sent.get(), 1))["type"] == "http.response.start"
    first = await asyncio.wait_for(sent.get(), 1)
    assert first["body"] == encode_event(StreamEvent.token(accepted.state, "中文", 1))
    heartbeat = await asyncio.wait_for(sent.get(), 1)
    assert heartbeat["body"] == b": heartbeat\n\n"
    assert not gate.is_set() and store.answers == []
    await receive_messages.put({"type": "http.disconnect"})
    await asyncio.wait_for(task, 1)
    assert closed.is_set()
    assert store.record is not None and store.record.status == "CANCELLED"
    assert not controller.leases and store.answers == []


async def test_sse_write_timeout_releases_run(settings: Settings) -> None:
    store = MemoryStore()
    service = RunService(store, FakeRuntime(), Reader())
    accepted = await service.accept(
        ChatRequest(message="hello"), Principal(user_id=7, request_id="id")
    )
    controller = ChatController(settings, DelegationVerifier(settings), service)
    calls = 0

    async def send(message: Message) -> None:
        nonlocal calls
        calls += 1
        if message["type"] == "http.response.body":
            await asyncio.Event().wait()

    async def receive() -> Message:
        await asyncio.Event().wait()
        return {"type": "http.disconnect"}

    await asyncio.wait_for(
        ChatStreamResponse(accepted, controller.acquire(), settings)({}, receive, send), 1
    )
    assert calls == 2
    assert store.record is not None and store.record.status == "CANCELLED"
    assert not controller.leases


async def test_capacity_and_shutdown_cancel_active_request(settings: Settings) -> None:
    settings.max_concurrent_runs = 1
    store = MemoryStore()
    service = RunService(store, FakeRuntime(), Reader())
    controller = ChatController(settings, DelegationVerifier(settings), service)
    started = asyncio.Event()

    async def consume() -> None:
        accepted = await service.accept(
            ChatRequest(message="hello"), Principal(user_id=7, request_id="id")
        )
        lease = controller.acquire()

        async def send(message: Message) -> None:
            started.set()
            await asyncio.Event().wait()

        async def receive() -> Message:
            await asyncio.Event().wait()
            return {"type": "http.disconnect"}

        await ChatStreamResponse(accepted, lease, settings)({}, receive, send)

    task = asyncio.create_task(consume())
    await asyncio.wait_for(started.wait(), 1)
    from app.api.chat import ChatRejected

    with pytest.raises(ChatRejected):
        controller.acquire()
    await asyncio.wait_for(controller.close(), 1)
    assert task.cancelled()
    assert not controller.leases
    assert store.record is not None and store.record.status == "CANCELLED"


@pytest.mark.parametrize("failure", ["not-found", "conflict", "database", "timeout"])
def test_preflight_failure_releases_capacity_and_redacts_errors(
    settings: Settings, key: rsa.RSAPrivateKey, failure: str
) -> None:
    class RejectingStore(MemoryStore):
        async def expire_running_runs(self) -> int:
            if failure == "timeout":
                await asyncio.Event().wait()
            errors = {
                "not-found": ConversationNotFound("private-query"),
                "conflict": ActiveRunConflict("private-query"),
                "database": SQLAlchemyError("private-db-password"),
            }
            raise errors[failure]

    settings.preflight_timeout_seconds = 0.01
    settings.max_concurrent_runs = 1
    store = RejectingStore()
    service = RunService(store, FakeRuntime(), Reader())
    application = create_app(
        settings, probe_factory=lambda _: FakeProbe(), run_service_factory=lambda *_: service
    )
    with TestClient(application) as http:
        for _ in range(2):
            response = http.post(
                "/api/v1/agent/chat",
                json={"message": "private-user-message"},
                headers={"Authorization": "Bearer " + token(key)},
            )
            assert response.status_code == {"not-found": 404, "conflict": 409}.get(failure, 503)
            assert "private" not in response.text
            assert not application.state.resources.chat.leases
    assert store.record is None


def test_initialization_failure_closes_database(
    settings: Settings, caplog: pytest.LogCaptureFixture
) -> None:
    class BrokenStore(MemoryStore):
        async def interrupt_running_runs(self) -> int:
            raise SQLAlchemyError("private-db-password")

    probe = FakeProbe()
    with (
        pytest.raises(ConfigurationError, match="^Agent Chat initialization failed$"),
        TestClient(
            create_app(
                settings,
                probe_factory=lambda _: probe,
                run_service_factory=lambda *_: RunService(BrokenStore(), FakeRuntime(), Reader()),
            )
        ),
    ):
        pass
    assert probe.closed
    assert "private" not in caplog.text


def test_chat_shutdown_failure_still_closes_database(
    settings: Settings, monkeypatch: pytest.MonkeyPatch, caplog: pytest.LogCaptureFixture
) -> None:
    async def fail_close(self: ChatController) -> None:
        raise RuntimeError("private-shutdown-error")

    monkeypatch.setattr(ChatController, "close", fail_close)
    probe = FakeProbe()
    with TestClient(
        create_app(
            settings,
            probe_factory=lambda _: probe,
            run_service_factory=lambda *_: RunService(MemoryStore(), FakeRuntime(), Reader()),
        )
    ):
        pass
    assert probe.closed
    assert "private" not in caplog.text
