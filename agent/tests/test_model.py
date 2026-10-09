"""Responses 的流式可见行为、预算、脱敏及取消负例。"""

import asyncio
import json
from collections.abc import AsyncIterator
from typing import Any
from uuid import uuid4

import httpx
import pytest
from pydantic import SecretStr

from app.clients.model import ResponsesClient
from app.core.credentials import CredentialCipher
from app.core.provider_policy import ProviderPolicy
from app.core.settings import Settings
from app.graphs.model_runtime import ModelRuntime
from app.graphs.service import RunService
from app.models.configuration import ConfigError, ModelProfileConfig, ProviderConfig, RuntimeBudget
from app.models.runtime import AgentState, ChatRequest, HistoryMessage
from tests.test_runtime import MemoryStore, Reader, state


def frame(event: dict[str, Any]) -> bytes:
    kind = str(event["type"])
    encoded = json.dumps(event, ensure_ascii=False)
    return f"event: {kind}\ndata: {encoded}\n\n".encode()


def delta(text: str = "二分查找") -> dict[str, Any]:
    return {"type": "response.output_text.delta", "delta": text, "output_index": 0}


def complete(text: str = "二分查找", *, usage: bool = True) -> dict[str, Any]:
    response: dict[str, Any] = {
        "status": "completed",
        "model": "deepseek-v4-flash",
        "output": [
            {
                "type": "message",
                "role": "assistant",
                "content": [{"type": "output_text", "text": text}],
            }
        ],
    }
    if usage:
        response["usage"] = {"input_tokens": 300, "output_tokens": 8}
    return {"type": "response.completed", "response": response}


def configured() -> AgentState:
    value = state()
    provider_id, profile_id = uuid4(), uuid4()
    provider = ProviderConfig(
        name="relay", base_url="https://relay.example/v1", credential_id=uuid4()
    )
    profile = ModelProfileConfig(
        provider="responses", provider_id=provider_id, model="deepseek-v4-flash"
    )
    value.config_snapshot = {
        "execution_mode": "direct",
        "prompt_text": "解释算法",
        "skill_prompt_text": "用中文",
        "budget": RuntimeBudget().model_dump(),
        "provider_id": str(provider_id),
        "model_profile_id": str(profile_id),
        "provider_config": provider.model_dump(mode="json"),
        "model_profile": profile.model_dump(mode="json"),
    }
    value.history = (HistoryMessage(role="user", content="history-must-not-be-sent"),)
    return value


class Credentials:
    def __init__(self, *, fail: bool = False) -> None:
        self.reads = 0
        self.fail = fail

    async def read(self, identifier: str) -> SecretStr:
        self.reads += 1
        if self.fail:
            raise ConfigError("AGENT_CREDENTIAL_UNAVAILABLE", 503)
        return SecretStr("private-api-key")


class Stream(httpx.AsyncByteStream):
    def __init__(self, chunks: list[bytes], *, gate: asyncio.Event | None = None) -> None:
        self.chunks = chunks
        self.gate = gate
        self.closed = False

    async def __aiter__(self) -> AsyncIterator[bytes]:
        for index, chunk in enumerate(self.chunks):
            if index and self.gate is not None:
                await self.gate.wait()
            yield chunk

    async def aclose(self) -> None:
        self.closed = True


def client_for(handler: Any) -> ResponsesClient:
    return ResponsesClient(
        ProviderPolicy(Settings(provider_allowed_origins=("https://relay.example",))),
        client=httpx.AsyncClient(transport=httpx.MockTransport(handler)),
    )


async def test_first_token_precedes_completion_and_history_never_leaks() -> None:
    gate = asyncio.Event()
    stream = Stream([frame(delta()), frame(complete())], gate=gate)
    calls: list[dict[str, Any]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        calls.append(json.loads(request.content))
        assert str(request.url) == "https://relay.example/v1/responses"
        assert request.headers["authorization"] == "Bearer private-api-key"
        return httpx.Response(200, headers={"content-type": "text/event-stream"}, stream=stream)

    client = client_for(handler)
    value = configured()
    iterator = ModelRuntime(client, Credentials()).run(value)
    try:
        assert (await anext(iterator)).type == "thinking"
        token = await asyncio.wait_for(anext(iterator), 1)
        assert token.type == "token" and not gate.is_set()
        assert calls[0]["store"] is False and calls[0]["stream"] is True
        assert calls[0]["input"] == [{"role": "user", "content": value.user_message}]
        assert "history-must-not-be-sent" not in json.dumps(calls)
        assert "解释算法" in calls[0]["instructions"] and "用中文" in calls[0]["instructions"]
        gate.set()
        assert (await anext(iterator)).type == "done"
        assert value.model_summary is not None
        assert value.model_summary.attempts[0].usage_source == "provider"
        assert value.model_summary.attempts[0].output_tokens == 8
    finally:
        await iterator.aclose()
        await client.close()
    assert stream.closed


@pytest.mark.parametrize(
    ("status", "code"),
    [
        (401, "AGENT_MODEL_AUTH_FAILED"),
        (403, "AGENT_MODEL_AUTH_FAILED"),
        (402, "AGENT_MODEL_QUOTA"),
        (429, "AGENT_MODEL_RATE_LIMIT"),
        (503, "AGENT_MODEL_UNAVAILABLE"),
        (307, "AGENT_MODEL_REJECTED"),
    ],
)
async def test_http_failure_is_sanitized_and_never_completed(status: int, code: str) -> None:
    client = client_for(
        lambda request: httpx.Response(status, text="private-api-key internal-error")
    )
    store = MemoryStore()
    reader = Reader()
    reader.snapshot = reader.snapshot.model_validate(
        {**reader.snapshot.model_dump(), **configured().config_snapshot}
    )
    service = RunService(store, ModelRuntime(client, Credentials()), reader)
    try:
        accepted = await service.accept(ChatRequest(message="解释算法"), state().principal)
        async with accepted:
            events = [event async for event in accepted.events]
        assert events[-1].type == "error" and events[-1].data["code"] == code
        assert "private-api-key" not in repr(events)
        assert store.record is not None and store.record.status == "FAILED"
        assert store.record.model_summary is not None and not store.answers
    finally:
        await client.close()


@pytest.mark.parametrize(
    "body",
    [
        frame(delta()),
        frame(complete()),
        frame(delta()) + frame(complete("different")),
        frame({"type": "response.output_item.added", "item": {"type": "function_call"}}),
        frame({"type": "response.refusal.delta", "delta": "private refusal"}),
        b"data: \xff\n\n",
        b"data: " + b"x" * 262_145 + b"\n\n",
        frame(
            {
                "type": "response.incomplete",
                "response": {"incomplete_details": {"reason": "max_output_tokens"}},
            }
        ),
    ],
    ids=[
        "eof",
        "no-delta",
        "mismatch",
        "tool",
        "refusal",
        "invalid-utf8",
        "oversize",
        "incomplete",
    ],
)
async def test_invalid_or_partial_stream_is_not_saved_as_success(body: bytes) -> None:
    client = client_for(
        lambda request: httpx.Response(
            200, headers={"content-type": "text/event-stream"}, stream=Stream([body])
        )
    )
    store = MemoryStore()
    reader = Reader()
    reader.snapshot = reader.snapshot.model_validate(
        {**reader.snapshot.model_dump(), **configured().config_snapshot}
    )
    service = RunService(store, ModelRuntime(client, Credentials()), reader)
    try:
        accepted = await service.accept(ChatRequest(message="算法"), state().principal)
        async with accepted:
            events = [event async for event in accepted.events]
        assert events[-1].type == "error" and all(event.type != "done" for event in events)
        assert store.record is not None and store.record.status == "FAILED" and not store.answers
    finally:
        await client.close()


async def test_network_split_utf8_reasoning_is_hidden_and_missing_usage_is_estimated() -> None:
    raw = (
        frame({"type": "response.reasoning_text.delta", "delta": "secret-reasoning"})
        + frame(delta())
        + frame(complete(usage=False))
    )
    client = client_for(
        lambda request: httpx.Response(
            200,
            headers={"content-type": "text/event-stream"},
            stream=Stream([raw[i : i + 1] for i in range(len(raw))]),
        )
    )
    value = configured()
    try:
        events = [event async for event in ModelRuntime(client, Credentials()).run(value)]
        assert (
            "".join(event.data.get("text", "") for event in events if event.type == "token")
            == "二分查找"
        )
        assert "secret-reasoning" not in repr(events)
        assert (
            value.model_summary is not None
            and value.model_summary.attempts[0].usage_source == "estimated"
        )
    finally:
        await client.close()


async def test_retry_is_bounded_and_keeps_the_same_provider() -> None:
    calls: list[str] = []

    def handler(request: httpx.Request) -> httpx.Response:
        calls.append(str(request.url))
        return (
            httpx.Response(429)
            if len(calls) == 1
            else httpx.Response(
                200,
                headers={"content-type": "text/event-stream"},
                stream=Stream([frame(delta()), frame(complete())]),
            )
        )

    client = client_for(handler)
    value = configured()
    value.config_snapshot["provider_config"]["max_retries"] = 1
    credentials = Credentials()
    try:
        events = [event async for event in ModelRuntime(client, credentials).run(value)]
        assert events[-1].type == "done" and len(calls) == 2 and credentials.reads == 2
        assert len(set(calls)) == 1
        assert value.model_summary is not None and len(value.model_summary.attempts) == 2
    finally:
        await client.close()


async def test_output_started_then_failure_never_retries_and_closes_stream() -> None:
    stream = Stream([frame(delta())])
    calls = 0

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal calls
        calls += 1
        return httpx.Response(200, headers={"content-type": "text/event-stream"}, stream=stream)

    client = client_for(handler)
    value = configured()
    value.config_snapshot["provider_config"]["max_retries"] = 2
    with pytest.raises(Exception, match="AGENT_MODEL_INVALID_RESPONSE"):
        _ = [event async for event in ModelRuntime(client, Credentials()).run(value)]
    assert calls == 1 and stream.closed
    await client.close()


async def test_cancel_closes_upstream_and_persists_cancelled_run() -> None:
    gate = asyncio.Event()
    stream = Stream([frame(delta()), frame(complete())], gate=gate)
    client = client_for(
        lambda request: httpx.Response(
            200, headers={"content-type": "text/event-stream"}, stream=stream
        )
    )
    store = MemoryStore()
    reader = Reader()
    reader.snapshot = reader.snapshot.model_validate(
        {**reader.snapshot.model_dump(), **configured().config_snapshot}
    )
    service = RunService(store, ModelRuntime(client, Credentials()), reader)
    accepted = await service.accept(ChatRequest(message="hello"), state().principal)
    try:
        assert (await anext(accepted.events)).type == "thinking"
        assert (await anext(accepted.events)).type == "token"
        await accepted.aclose()
        assert stream.closed and store.record is not None and store.record.status == "CANCELLED"
        assert not store.answers
    finally:
        await client.close()


def test_authenticated_encryption_and_key_version_fail_closed() -> None:
    cipher = CredentialCipher("v1", {"v1": b"k" * 32})
    version, nonce, ciphertext = cipher.encrypt("credential-id", SecretStr("private-api-key"))
    assert b"private-api-key" not in ciphertext
    assert (
        cipher.decrypt("credential-id", version, nonce, ciphertext).get_secret_value()
        == "private-api-key"
    )
    for identifier, key_version, value in [
        ("other-id", version, ciphertext),
        ("credential-id", "missing", ciphertext),
        ("credential-id", version, ciphertext[:-1]),
    ]:
        with pytest.raises(ConfigError, match="UNAVAILABLE"):
            cipher.decrypt(identifier, key_version, nonce, value)


def test_keyring_file_requires_restricted_permissions_outside_test(tmp_path: Any) -> None:
    keyring = tmp_path / "keyring.json"
    keyring.write_text(
        '{"active":"v1","keys":{"v1":"' + __import__("base64").b64encode(b"k" * 32).decode() + '"}}'
    )
    keyring.chmod(0o644)
    with pytest.raises(ConfigError, match="KEYRING_INVALID"):
        CredentialCipher.from_file(keyring)
    assert CredentialCipher.from_file(keyring, allow_insecure_test_file=True)
    keyring.chmod(0o600)
    assert CredentialCipher.from_file(keyring)


@pytest.mark.parametrize(
    "address",
    [
        "https://other.example/v1",
        "http://relay.example/v1",
        "https://user:password@relay.example/v1",
        "https://relay.example/v1?secret=x",
        "https://relay.example:444/v1",
        "https://relay.example/v1/../internal",
    ],
)
def test_provider_origin_is_precise_and_no_credentials_in_url(address: str) -> None:
    policy = ProviderPolicy(Settings(provider_allowed_origins=("https://relay.example",)))
    with pytest.raises(ConfigError):
        policy.validate(ProviderConfig(name="bad", base_url=address, credential_id=uuid4()))


@pytest.mark.parametrize("setting", ["first_token_timeout_seconds", "idle_timeout_seconds"])
async def test_first_token_and_idle_deadlines_close_upstream(setting: str) -> None:
    first = (
        frame({"type": "response.created", "response": {}})
        if setting == "first_token_timeout_seconds"
        else frame(delta())
    )
    stream = Stream([first, frame(complete())], gate=asyncio.Event())
    client = client_for(
        lambda request: httpx.Response(
            200, headers={"content-type": "text/event-stream"}, stream=stream
        )
    )
    value = configured()
    value.config_snapshot["provider_config"][setting] = 0.03
    try:
        with pytest.raises(Exception, match="AGENT_MODEL_TIMEOUT"):
            _ = [event async for event in ModelRuntime(client, Credentials()).run(value)]
        assert stream.closed
    finally:
        await client.close()


@pytest.mark.parametrize("kind", ["input", "output", "credential"])
async def test_budget_and_revocation_fail_without_completed_answer(kind: str) -> None:
    calls = []

    def handler(request: httpx.Request) -> httpx.Response:
        calls.append(request)
        return httpx.Response(
            200,
            headers={"content-type": "text/event-stream"},
            stream=Stream([frame(delta()), frame(complete())]),
        )

    client = client_for(handler)
    value = configured()
    if kind == "input":
        value.config_snapshot["budget"]["max_input_tokens"] = 1
    elif kind == "output":
        value.config_snapshot["budget"]["max_output_chars"] = 1
    try:
        with pytest.raises(
            Exception,
            match={
                "input": "AGENT_MODEL_INPUT_LIMIT",
                "output": "AGENT_MODEL_OUTPUT_LIMIT",
                "credential": "AGENT_CREDENTIAL_UNAVAILABLE",
            }[kind],
        ):
            _ = [
                event
                async for event in ModelRuntime(client, Credentials(fail=kind == "credential")).run(
                    value
                )
            ]
        assert len(calls) == (1 if kind == "output" else 0)
    finally:
        await client.close()


async def test_final_storage_failure_does_not_emit_done() -> None:
    client = client_for(
        lambda request: httpx.Response(
            200,
            headers={"content-type": "text/event-stream"},
            stream=Stream([frame(delta()), frame(complete())]),
        )
    )
    store = MemoryStore()
    store.fail_completion = True
    reader = Reader()
    reader.snapshot = reader.snapshot.model_validate(
        {**reader.snapshot.model_dump(), **configured().config_snapshot}
    )
    try:
        service = RunService(store, ModelRuntime(client, Credentials()), reader)
        accepted = await service.accept(ChatRequest(message="算法"), state().principal)
        async with accepted:
            events = [event async for event in accepted.events]
        assert events[-1].type == "error" and all(event.type != "done" for event in events)
        assert store.record is not None and store.record.status == "FAILED" and not store.answers
    finally:
        await client.close()
