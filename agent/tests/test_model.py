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


@pytest.mark.parametrize("native_verbosity", [True, False])
async def test_first_token_precedes_completion_and_incomplete_history_is_excluded(
    native_verbosity: bool,
) -> None:
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
    value.config_snapshot["provider_config"]["text_verbosity_supported"] = native_verbosity
    value.config_snapshot["model_profile"]["verbosity"] = "high"
    iterator = ModelRuntime(client, Credentials()).run(value)
    try:
        assert (await anext(iterator)).type == "thinking"
        token = await asyncio.wait_for(anext(iterator), 1)
        assert token.type == "token" and not gate.is_set()
        if native_verbosity:
            assert calls[0]["text"] == {"verbosity": "high"}
        else:
            assert "text" not in calls[0] and "回答详尽" in calls[0]["instructions"]
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
    inputs: list[list[dict[str, str]]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        calls.append(str(request.url))
        inputs.append(json.loads(request.content)["input"])
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
    value.history = (
        HistoryMessage(role="user", content="之前的问题"),
        HistoryMessage(role="assistant", content="之前的回答"),
    )
    value.config_snapshot["provider_config"]["max_retries"] = 1
    credentials = Credentials()
    try:
        events = [event async for event in ModelRuntime(client, credentials).run(value)]
        assert events[-1].type == "done" and len(calls) == 2 and credentials.reads == 2
        assert len(set(calls)) == 1
        assert value.model_summary is not None and len(value.model_summary.attempts) == 2
        assert (
            inputs[0]
            == inputs[1]
            == [
                *(item.model_dump() for item in value.history),
                {"role": "user", "content": value.user_message},
            ]
        )
    finally:
        await client.close()


@pytest.mark.parametrize("constraint", ["input", "context"])
async def test_history_keeps_recent_whole_turns_within_input_and_context_limits(
    constraint: str,
) -> None:
    from app.graphs.direct_answer import DirectAnswer

    calls: list[dict[str, Any]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        calls.append(json.loads(request.content))
        return httpx.Response(
            200,
            headers={"content-type": "text/event-stream"},
            stream=Stream([frame(delta()), frame(complete(usage=False))]),
        )

    client = client_for(handler)
    value = configured()
    value.history = ()
    base = DirectAnswer(client, Credentials()).prepare(value)
    older = (
        HistoryMessage(role="user", content="旧问题"),
        HistoryMessage(role="assistant", content="旧回答"),
    )
    recent = (
        HistoryMessage(role="user", content="记住数字 314159"),
        HistoryMessage(role="assistant", content="已记住 314159"),
    )
    cost = sum(len(item.content.encode()) + 128 for item in recent)
    limit = base.input_estimate + cost
    if constraint == "input":
        value.config_snapshot["budget"]["max_input_tokens"] = limit
    else:
        value.config_snapshot["model_profile"]["context_window_tokens"] = limit + base.max_output
    value.history = (*older, *recent)
    try:
        events = [event async for event in ModelRuntime(client, Credentials()).run(value)]
        assert events[-1].type == "done"
        expected = [item.model_dump() for item in recent] + [
            {"role": "user", "content": value.user_message}
        ]
        assert calls[0]["input"] == expected
        assert "314159" not in calls[0]["instructions"]
        assert calls[0]["store"] is False and "previous_response_id" not in calls[0]
        assert value.model_summary is not None
        assert value.model_summary.attempts[0].input_tokens == limit
        # 少一个估算 Token 时整轮退出，不截断成单条 assistant；当前输入保留。
        if constraint == "input":
            value.config_snapshot["budget"]["max_input_tokens"] -= 1
        else:
            value.config_snapshot["model_profile"]["context_window_tokens"] -= 1
        _ = [event async for event in ModelRuntime(client, Credentials()).run(value)]
        assert calls[1]["input"] == [{"role": "user", "content": value.user_message}]
        assert value.model_summary.attempts[0].input_tokens == base.input_estimate
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


async def test_graph_backpressure_and_tracing_stay_local(monkeypatch: pytest.MonkeyPatch) -> None:
    from langsmith import get_tracing_context, tracing_context

    from app.graphs.direct_answer import DirectAnswer

    stages: list[str] = []
    prepare = DirectAnswer.prepare
    validate = DirectAnswer.validate_answer

    def traced_prepare(self: DirectAnswer, value: AgentState) -> Any:
        assert get_tracing_context()["enabled"] is False
        stages.append("prepare")
        return prepare(self, value)

    def traced_validate(self: DirectAnswer, value: AgentState, answer: str) -> None:
        assert get_tracing_context()["enabled"] is False
        stages.append("validate")
        validate(self, value, answer)

    monkeypatch.setattr(DirectAnswer, "prepare", traced_prepare)
    monkeypatch.setattr(DirectAnswer, "validate_answer", traced_validate)

    class ObservedStream(Stream):
        async def __aiter__(self) -> AsyncIterator[bytes]:
            async for chunk in super().__aiter__():
                stages.append("read")
                yield chunk

    stream = ObservedStream([frame(delta("前")), frame(delta("后")), frame(complete("前后"))])

    def handler(request: httpx.Request) -> httpx.Response:
        assert get_tracing_context()["enabled"] is False
        return httpx.Response(200, headers={"content-type": "text/event-stream"}, stream=stream)

    client = client_for(handler)
    iterator = ModelRuntime(client, Credentials()).run(configured())
    try:
        with tracing_context(enabled=True):
            assert (await anext(iterator)).type == "thinking"
            # 消费者还没请求下一事件，不能读取供应商。
            assert stages == ["prepare"]
            assert (await anext(iterator)).data["text"] == "前"
            await asyncio.sleep(0)
            assert stages == ["prepare", "read"]
            assert (await anext(iterator)).data["text"] == "后"
            assert (await anext(iterator)).type == "done"
            assert stages == ["prepare", "read", "read", "read", "validate"]
            assert get_tracing_context()["enabled"] is True
    finally:
        await iterator.aclose()
        await client.close()
    assert stream.closed


async def test_shared_graph_keeps_concurrent_runs_isolated() -> None:
    streams: dict[str, Stream] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        message = json.loads(request.content)["input"][0]["content"]
        stream = Stream([frame(delta(message)), frame(complete(message))])
        streams[message] = stream
        return httpx.Response(200, headers={"content-type": "text/event-stream"}, stream=stream)

    client = client_for(handler)
    runtime = ModelRuntime(client, Credentials())
    first, second = configured(), configured()
    first.user_message, second.user_message = "第一轮", "第二轮"
    iterator = runtime.run(first)
    try:
        assert (await anext(iterator)).type == "thinking"
        assert (await anext(iterator)).data["text"] == "第一轮"
        # 第一条流暂停时，同一个编译图上的另一条流可以独立完成。
        events = [event async for event in runtime.run(second)]
        assert [event.sequence for event in events] == [1, 2, 3]
        assert all(event.run_id == second.run_id for event in events)
        assert events[1].data["text"] == "第二轮" and streams["第二轮"].closed
        assert not streams["第一轮"].closed
        await iterator.aclose()
        assert streams["第一轮"].closed
        assert first.model_summary is not None and second.model_summary is not None
        assert first.model_summary.attempts[0].finish_reason == "cancelled"
        assert second.model_summary.attempts[0].finish_reason == "completed"
        assert (
            first.model_summary.attempts[0].provider_id
            != second.model_summary.attempts[0].provider_id
        )
    finally:
        await iterator.aclose()
        await client.close()


@pytest.mark.parametrize("cancel", [False, True])
async def test_deadline_and_cancel_close_graph_waiting_for_first_token(cancel: bool) -> None:
    started = asyncio.Event()

    class WaitingStream(Stream):
        async def __aiter__(self) -> AsyncIterator[bytes]:
            started.set()
            await asyncio.Event().wait()
            yield b"unreachable"

    stream = WaitingStream([])
    client = client_for(
        lambda request: httpx.Response(
            200, headers={"content-type": "text/event-stream"}, stream=stream
        )
    )
    store, reader = MemoryStore(), Reader()
    snapshot = configured().config_snapshot
    snapshot["budget"]["max_run_seconds"] = 30 if cancel else 1
    reader.snapshot = reader.snapshot.model_validate({**reader.snapshot.model_dump(), **snapshot})
    service = RunService(store, ModelRuntime(client, Credentials()), reader)
    accepted = await service.accept(ChatRequest(message="算法"), state().principal)
    try:
        async with accepted:
            assert (await anext(accepted.events)).type == "thinking"
            task = asyncio.ensure_future(anext(accepted.events))
            await asyncio.wait_for(started.wait(), 1)
            if cancel:
                task.cancel()
                with pytest.raises(asyncio.CancelledError):
                    await task
            else:
                event = await asyncio.wait_for(task, 3)
                assert event.type == "error" and event.data["code"] == "AGENT_DEADLINE_EXCEEDED"
        assert stream.closed and not store.answers
        assert store.record is not None and store.record.status == (
            "CANCELLED" if cancel else "FAILED"
        )
    finally:
        await client.close()


async def test_provider_timeout_while_token_is_paused_keeps_failure_classification() -> None:
    closed = asyncio.Event()

    class ClosingStream(Stream):
        async def aclose(self) -> None:
            await super().aclose()
            closed.set()

    stream = ClosingStream([frame(delta()), frame(complete())])
    client = client_for(
        lambda request: httpx.Response(
            200, headers={"content-type": "text/event-stream"}, stream=stream
        )
    )
    store, reader = MemoryStore(), Reader()
    snapshot = configured().config_snapshot
    snapshot["provider_config"]["request_timeout_seconds"] = 0.2
    reader.snapshot = reader.snapshot.model_validate({**reader.snapshot.model_dump(), **snapshot})
    service = RunService(store, ModelRuntime(client, Credentials()), reader)
    accepted = await service.accept(ChatRequest(message="算法"), state().principal)
    try:
        async with accepted:
            assert (await anext(accepted.events)).type == "thinking"
            assert (await anext(accepted.events)).type == "token"
            # 不确认 token，等 Provider 总超时主动关闭上游，再继续消费。
            await asyncio.wait_for(closed.wait(), 2)
            event = await anext(accepted.events)
            assert event.type == "error" and event.data["code"] == "AGENT_MODEL_TIMEOUT"
        assert not store.answers
        assert store.record is not None and store.record.status == "FAILED"
        assert store.record.model_summary is not None
        attempt = store.record.model_summary["attempts"][-1]
        assert (
            attempt["finish_reason"] == "failed" and attempt["error_code"] == "AGENT_MODEL_TIMEOUT"
        )
    finally:
        await client.close()


async def test_stream_disconnect_after_200_is_visible_without_replay(
    caplog: pytest.LogCaptureFixture,
) -> None:
    class BrokenStream(httpx.AsyncByteStream):
        async def __aiter__(self) -> AsyncIterator[bytes]:
            yield b": connected\n\n"
            raise httpx.RemoteProtocolError("private-api-key provider-internal-details")

    calls = 0

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal calls
        calls += 1
        return httpx.Response(
            200, headers={"content-type": "text/event-stream"}, stream=BrokenStream()
        )

    client = client_for(handler)
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
        assert events[-1].type == "error"
        assert events[-1].data["code"] == "AGENT_MODEL_UNAVAILABLE"
        assert calls == 1 and not store.answers
        assert "RemoteProtocolError" in caplog.text and "headers_received=True" in caplog.text
        assert (
            "private-api-key" not in caplog.text and "provider-internal-details" not in caplog.text
        )
    finally:
        await client.close()


async def test_automatic_skill_references_reach_model_without_extra_call() -> None:
    calls: list[dict[str, Any]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        calls.append(json.loads(request.content))
        return httpx.Response(
            200,
            headers={"content-type": "text/event-stream"},
            stream=Stream([frame(delta()), frame(complete())]),
        )

    value = configured()
    value.config_snapshot.update(
        skill_selection="automatic",
        skill_prompt_text=None,
        skill_references=[
            {
                "id": str(uuid4()),
                "key": "explain_algorithm",
                "name": "算法讲解",
                "description": "理解算法与复杂度",
                "text": "从直觉、步骤、复杂度解释算法。",
            },
            {
                "id": str(uuid4()),
                "key": "problem_hint",
                "name": "题目提示",
                "description": "按需给提示",
                "text": "只给提示，避免直接公布完整代码。",
            },
        ],
    )
    client = client_for(handler)
    try:
        events = [event async for event in ModelRuntime(client, Credentials()).run(value)]
        assert events[-1].type == "done" and len(calls) == 1
        instructions = calls[0]["instructions"]
        assert "自行采用适用的技能说明" in instructions
        assert "从直觉、步骤、复杂度解释算法。" in instructions
        assert "只给提示，避免直接公布完整代码。" in instructions
        assert "本轮没有访问题目" in instructions and "tools" not in calls[0]
    finally:
        await client.close()
