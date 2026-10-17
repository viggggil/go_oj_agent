"""只读工具循环的协议、权限、累计预算及取消回归。"""

import asyncio
import json
from typing import Any

import httpx
import pytest
from pydantic import BaseModel, ConfigDict

from app.graphs.model_runtime import ModelRuntime
from app.models.provider import ModelFailure
from app.models.runtime import Principal
from app.tools.registry import ToolDefinition, ToolRegistry, ToolSpec
from tests.test_model import Credentials, Stream, client_for, complete, configured, delta, frame


class Empty(BaseModel):
    model_config = ConfigDict(extra="forbid")


class Evidence(BaseModel):
    number: int


def registry(handler: Any) -> ToolRegistry:
    value = ToolRegistry()
    value.register(
        ToolDefinition(
            spec=ToolSpec(
                name="read_number", description="读取测试数字", read_scope="current_user"
            ),
            input_model=Empty,
            output_model=Evidence,
            handler=handler,
        )
    )
    return value


def call_output(name: str = "read_number", arguments: str = "{}", call_id: str = "call_1") -> bytes:
    return frame(
        {
            "type": "response.completed",
            "response": {
                "status": "completed",
                "output": [
                    {"type": "reasoning", "text": "hidden-reasoning"},
                    {
                        "type": "function_call",
                        "call_id": call_id,
                        "name": name,
                        "arguments": arguments,
                        "status": "completed",
                    },
                ],
                "usage": {"input_tokens": 1, "output_tokens": 1},
            },
        }
    )


@pytest.mark.parametrize(
    "failure",
    [None, "unknown", "arguments", "tool_budget", "model_budget", "input_budget", "output_budget"],
)
async def test_tool_loop_keeps_trusted_identity_and_bounded_protocol(failure: str | None) -> None:
    invoked: list[Principal] = []
    requests: list[dict[str, Any]] = []

    async def read(_: BaseModel, principal: Principal) -> BaseModel:
        invoked.append(principal)
        return Evidence(number=739216)

    def respond(request: httpx.Request) -> httpx.Response:
        body = json.loads(request.content)
        requests.append(body)
        if len(requests) == 1 or failure == "tool_budget":
            data = call_output(
                name="forbidden_tool" if failure == "unknown" else "read_number",
                arguments='{"actor_id":1,"actor_id":2}' if failure == "arguments" else "{}",
                call_id=f"call_{len(requests)}",
            )
        else:
            output = body["input"][-1]
            assert output["type"] == "function_call_output" and output["call_id"] == "call_1"
            assert json.loads(output["output"])["data"] == {"number": 739216}
            assert all(item.get("type") != "reasoning" for item in body["input"])
            assert set(body["input"][-2]) == {"type", "name", "arguments", "call_id"}
            data = frame(delta("739216")) + frame(complete("739216"))
        return httpx.Response(
            200, headers={"content-type": "text/event-stream"}, stream=Stream([data])
        )

    client = client_for(respond)
    value = configured()
    value.config_snapshot["execution_mode"] = "react"
    value.allowed_tools = ("read_number",)
    budget = value.config_snapshot["budget"]
    if failure == "tool_budget":
        budget["max_tool_calls"] = 1
    if failure == "model_budget":
        budget["max_model_calls"] = 1
    if failure == "input_budget":
        budget["max_input_tokens"] = 1400
    if failure == "output_budget":
        budget["max_output_tokens"] = 1
    try:
        events = []
        try:
            async for event in ModelRuntime(client, Credentials(), registry(read)).run(value):
                events.append(event)
        except ModelFailure as error:
            expected = {
                "unknown": "AGENT_MODEL_UNSUPPORTED_OUTPUT",
                "arguments": "AGENT_MODEL_INVALID_RESPONSE",
                "tool_budget": "AGENT_TOOL_CALL_LIMIT",
                "model_budget": "AGENT_MODEL_CALL_LIMIT",
                "input_budget": "AGENT_MODEL_INPUT_LIMIT",
                "output_budget": "AGENT_MODEL_OUTPUT_LIMIT",
            }
            assert failure and error.code == expected[failure]
        else:
            assert failure is None and events[-1].type == "done"
            assert (
                "".join(event.data.get("text", "") for event in events if event.type == "token")
                == "739216"
            )
        assert len(invoked) == (0 if failure in {"unknown", "arguments"} else 1)
        assert all(principal == value.principal for principal in invoked)
        assert "hidden-reasoning" not in repr(events) and "function_call_output" not in repr(events)
        assert len(requests) <= 2
        assert requests[0]["parallel_tool_calls"] is False
        assert [tool["name"] for tool in requests[0]["tools"]] == ["read_number"]
        if failure:
            assert all(event.type != "done" for event in events)
    finally:
        await client.close()


async def test_cancellation_closes_pending_tool_and_never_starts_next_model_round() -> None:
    started, closed = asyncio.Event(), asyncio.Event()
    calls = 0

    async def waiting(_: BaseModel, principal: Principal) -> BaseModel:
        started.set()
        try:
            await asyncio.Event().wait()
        finally:
            closed.set()
        return Evidence(number=1)

    def respond(request: httpx.Request) -> httpx.Response:
        nonlocal calls
        calls += 1
        return httpx.Response(
            200, headers={"content-type": "text/event-stream"}, stream=Stream([call_output()])
        )

    client = client_for(respond)
    value = configured()
    value.config_snapshot["execution_mode"] = "react"
    value.allowed_tools = ("read_number",)
    iterator = ModelRuntime(client, Credentials(), registry(waiting)).run(value)
    try:
        assert (await anext(iterator)).data["phase"] == "preparing"
        assert (await anext(iterator)).data["phase"] == "analyzing"
        assert (await anext(iterator)).data["phase"] == "executing_tool"
        assert (await anext(iterator)).type == "tool_call"
        task = asyncio.create_task(anext(iterator))
        await asyncio.wait_for(started.wait(), 1)
        task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await task
        await asyncio.wait_for(closed.wait(), 1)
        assert calls == 1
    finally:
        await iterator.aclose()
        await client.close()


async def test_tool_argument_progress_switches_first_output_timeout_to_idle_timeout() -> None:
    class Progress(Stream):
        async def __aiter__(self):  # type: ignore[no-untyped-def]
            for index in range(7):
                await asyncio.sleep(0.015)
                yield frame(
                    {
                        "type": "response.function_call_arguments.delta",
                        "delta": "{}" if index == 0 else " ",
                        "output_index": 0,
                    }
                )
            yield call_output()

    async def read(_: BaseModel, principal: Principal) -> BaseModel:
        return Evidence(number=739216)

    requests = 0

    def respond(request: httpx.Request) -> httpx.Response:
        nonlocal requests
        requests += 1
        stream = (
            Progress([])
            if requests == 1
            else Stream([frame(delta("739216")), frame(complete("739216"))])
        )
        return httpx.Response(200, headers={"content-type": "text/event-stream"}, stream=stream)

    client = client_for(respond)
    value = configured()
    value.config_snapshot["execution_mode"] = "react"
    value.config_snapshot["provider_config"].update(
        first_token_timeout_seconds=0.05, idle_timeout_seconds=1
    )
    value.allowed_tools = ("read_number",)
    try:
        events = [
            event async for event in ModelRuntime(client, Credentials(), registry(read)).run(value)
        ]
        assert events[-1].type == "done" and requests == 2
        assert value.model_summary and value.model_summary.attempts[0].first_token_ms is not None
    finally:
        await client.close()


@pytest.mark.parametrize(
    "scenario", ["multi", "failed_tool", "repeat", "duplicate_id", "schema", "json", "mixed"]
)
async def test_unified_react_decisions_and_rejected_calls(scenario: str) -> None:
    class Input(BaseModel):
        model_config = ConfigDict(extra="forbid", strict=True)
        number: int

    invoked: list[int] = []
    requests: list[dict[str, Any]] = []

    async def read(parsed: BaseModel, principal: Principal) -> BaseModel:
        assert isinstance(parsed, Input)
        invoked.append(parsed.number)
        if scenario == "failed_tool":
            raise TimeoutError
        return Evidence(number=parsed.number)

    tools = ToolRegistry()
    tools.register(
        ToolDefinition(
            spec=ToolSpec(name="read_number", description="读取数字", read_scope="current_user"),
            input_model=Input,
            output_model=Evidence,
            handler=read,
        )
    )

    def respond(request: httpx.Request) -> httpx.Response:
        body = json.loads(request.content)
        requests.append(body)
        count = len(requests)
        if count == 1:
            arguments = (
                '{"number":"bad"}'
                if scenario == "schema"
                else "{"
                if scenario == "json"
                else '{"number":1}'
            )
            payload = call_output(arguments=arguments)
            if scenario == "mixed":
                event = json.loads(payload.decode().split("data: ")[1].strip())
                event["response"]["output"].append(
                    {
                        "type": "message",
                        "role": "assistant",
                        "content": [{"type": "output_text", "text": "unconfirmed-private-text"}],
                    }
                )
                payload = frame(delta("unconfirmed-private-text")) + frame(event)
        elif scenario in {"multi", "repeat", "duplicate_id"} and count == 2:
            payload = call_output(
                arguments='{"number":2}' if scenario != "repeat" else '{"number":1}',
                call_id="call_1" if scenario == "duplicate_id" else "call_2",
            )
        else:
            observation = json.loads(body["input"][-1]["output"])
            assert observation["ok"] is (scenario != "failed_tool")
            if scenario == "failed_tool":
                assert observation["error_code"] == "TOOL_TIMEOUT"
            payload = frame(delta("已依据实际观察回答")) + frame(complete("已依据实际观察回答"))
        return httpx.Response(
            200, headers={"content-type": "text/event-stream"}, stream=Stream([payload])
        )

    client = client_for(respond)
    value = configured()
    # A historical Direct snapshot now uses the same graph and authorized tools.
    value.allowed_tools = ("read_number",)
    events = []
    try:
        if scenario in {"multi", "failed_tool"}:
            events = [
                event async for event in ModelRuntime(client, Credentials(), tools).run(value)
            ]
            assert events[-1].type == "done"
            assert invoked == ([1, 2] if scenario == "multi" else [1])
            assert value.model_summary is not None
            assert len(value.model_summary.tool_steps) == len(invoked)
            assert value.model_summary.tool_steps[-1].status == (
                "failed" if scenario == "failed_tool" else "succeeded"
            )
            assert not any("number" in event.data or "output" in event.data for event in events)
        else:
            with pytest.raises(ModelFailure):
                async for event in ModelRuntime(client, Credentials(), tools).run(value):
                    events.append(event)
            assert invoked == ([1] if scenario in {"repeat", "duplicate_id"} else [])
            assert not any(event.type in {"token", "done"} for event in events)
            if scenario != "mixed":
                assert (
                    value.model_summary and value.model_summary.tool_steps[-1].status == "rejected"
                )
    finally:
        await client.close()


@pytest.mark.parametrize("supported", [False, True])
async def test_public_summary_and_opaque_continuation_are_separate(supported: bool) -> None:
    requests = []

    async def read(_: BaseModel, principal: Principal) -> BaseModel:
        return Evidence(number=1)

    def respond(request: httpx.Request) -> httpx.Response:
        body = json.loads(request.content)
        requests.append(body)
        if supported:
            assert body["reasoning"] == {"summary": "auto"}
            assert body["include"] == ["reasoning.encrypted_content"]
        else:
            assert "reasoning" not in body and "include" not in body
        if len(requests) == 1:
            event = json.loads(call_output().decode().split("data: ")[1].strip())
            event["response"]["output"][0] = {
                "type": "reasoning",
                "id": "rs_1",
                "text": "secret-internal-reasoning",
                "encrypted_content": "opaque-secret",
                "summary": [{"type": "summary_text", "text": "需要读取授权信息"}],
            }
            payload = frame(event)
        else:
            assert "secret-internal-reasoning" not in json.dumps(body)
            payload = frame(delta("完成")) + frame(complete("完成"))
        return httpx.Response(
            200, headers={"content-type": "text/event-stream"}, stream=Stream([payload])
        )

    client = client_for(respond)
    value = configured()
    value.allowed_tools = ("read_number",)
    value.config_snapshot["provider_config"]["reasoning_summary_supported"] = supported
    try:
        events = [
            event async for event in ModelRuntime(client, Credentials(), registry(read)).run(value)
        ]
        assert "secret-internal-reasoning" not in repr(events)
        assert value.model_summary is not None
        assert (
            "opaque-secret" not in repr(events)
            and "opaque-secret" not in value.model_summary.model_dump_json()
        )
        assert any(event.type == "reasoning_summary" for event in events) is supported
    finally:
        await client.close()
