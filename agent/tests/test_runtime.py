"""不依赖模型的图、预算、取消、事件和持久化边界测试。"""

import asyncio
from collections.abc import AsyncIterator
from dataclasses import replace
from datetime import timedelta
from typing import Any
from uuid import UUID, uuid4

import pytest
from pydantic import SecretStr, ValidationError

from app.core.runtime_config import ConfigSnapshot, DemoConfigReader, RuntimeBudget
from app.core.settings import Settings
from app.graphs.langgraph_runtime import LangGraphRuntime
from app.graphs.runtime import FakeModelClient, FakeRuntime, parse_demo_tool_request
from app.graphs.service import RunService, RuntimeFailure
from app.models.runtime import (
    AgentState,
    ChatRequest,
    Principal,
    RunStatus,
    StreamEvent,
    validate_run_transition,
)
from app.storage.repository import MessageRecord, RunRecord, utc_now


def state() -> AgentState:
    now = utc_now()
    return AgentState(
        user_id=7,
        principal=Principal(user_id=7, request_id="request-1"),
        run_id=uuid4(),
        conversation_id=uuid4(),
        user_message="介绍二分查找",
        started_at=now,
        deadline_at=now + timedelta(seconds=10),
    )


@pytest.mark.parametrize("mode", ["fake", "langgraph_fake"])
async def test_runtime_events_are_real_async_and_json_safe(mode: str) -> None:
    runtime = FakeRuntime("中文回答") if mode == "fake" else LangGraphRuntime(FakeModelClient())
    events = [event async for event in runtime.run(state())]
    assert [event.type for event in events] == ["thinking", "token", "done"]
    assert [event.sequence for event in events] == [1, 2, 3]
    assert "演示回答" in events[1].data["text"] or events[1].data["text"] == "中文回答"
    assert StreamEvent.model_validate_json(events[1].model_dump_json()) == events[1]


def test_chat_rejects_identity_override_and_blank_message() -> None:
    for value in (
        {"message": "   "},
        {"message": "hi", "user_id": 1},
        {"message": "hi", "conversation_id": "invalid"},
        {"message": "hi", "context": {"submission_id": 0}},
        {"message": "hi", "context": {"user_id": 1}},
    ):
        with pytest.raises(ValidationError):
            ChatRequest.model_validate(value)
    assert ChatRequest(message="介绍算法").context.submission_id is None


def test_demo_tool_parser_requires_explicit_json_object() -> None:
    assert parse_demo_tool_request("介绍算法") is None
    assert parse_demo_tool_request("/tool list_problems {broken") == (
        "list_problems",
        {"__invalid_json__": "{broken"},
    )
    assert parse_demo_tool_request("/tool list_problems []") == (
        "list_problems",
        {"__invalid_arguments__": []},
    )


def test_terminal_run_cannot_transition_again() -> None:
    for target in ("COMPLETED", "FAILED", "CANCELLED", "INTERRUPTED"):
        validate_run_transition("RUNNING", target)
        with pytest.raises(ValueError):
            validate_run_transition(target, "COMPLETED")


def test_demo_must_be_explicit_and_cannot_be_production() -> None:
    with pytest.raises(ValueError):
        DemoConfigReader(Settings())
    with pytest.raises(ValidationError):
        Settings(
            environment="production",
            runtime_mode="fake",
            database_url=SecretStr("mysql+asyncmy://agent:secret@db/oj_agent"),
        )


class MemoryStore:
    def __init__(self) -> None:
        self.record: RunRecord | None = None
        self.answers: list[str] = []
        self.fail_completion = False

    async def create_run_with_user_message(
        self,
        *,
        user_id: int,
        conversation_id: str | UUID | None,
        request_id: str,
        content: str,
        config_source: str,
        config_snapshot: dict[str, Any],
        deadline_at: object,
    ) -> RunRecord:
        from datetime import datetime

        assert isinstance(deadline_at, datetime)
        self.record = RunRecord(
            id=str(uuid4()),
            conversation_id=str(conversation_id or uuid4()),
            user_id=user_id,
            request_id=request_id,
            config_source=config_source,
            config_snapshot=config_snapshot,
            status="RUNNING",
            started_at=utc_now(),
            finished_at=None,
            deadline_at=deadline_at,
            error_code=None,
        )
        return self.record

    async def finish_run(
        self,
        *,
        user_id: int,
        run_id: str | UUID,
        status: RunStatus,
        answer: str | None = None,
        error_code: str | None = None,
    ) -> RunRecord:
        assert self.record is not None
        if status == "COMPLETED" and self.fail_completion:
            raise RuntimeError("private-db-password")
        assert self.record.status == "RUNNING"
        self.record = replace(
            self.record, status=status, finished_at=utc_now(), error_code=error_code
        )
        if answer:
            self.answers.append(answer)
        return self.record

    async def interrupt_running_runs(self) -> int:
        return 0

    async def expire_running_runs(self) -> int:
        return 0

    async def list_messages(
        self, user_id: int, conversation_id: str | UUID, *, limit: int = 100
    ) -> list[MessageRecord]:
        return []


class Reader:
    def __init__(self, budget: RuntimeBudget | None = None) -> None:
        self.snapshot = ConfigSnapshot(runtime="fake", budget=budget or RuntimeBudget())

    async def read(self) -> ConfigSnapshot:
        return self.snapshot.model_copy(deep=True)


async def test_done_follows_persistence_and_config_snapshot_is_fixed() -> None:
    store = MemoryStore()
    reader = Reader()
    service = RunService(store, FakeRuntime("测试答案"), reader)
    accepted = await service.accept(ChatRequest(message="hello"), state().principal)
    reader.snapshot = ConfigSnapshot(runtime="fake", budget=RuntimeBudget(max_output_chars=1))
    async with accepted:
        events = []
        async for event in accepted.events:
            if event.type == "done":
                assert store.answers == ["测试答案"]
                assert store.record is not None and store.record.status == "COMPLETED"
            events.append(event)
    assert [event.type for event in events] == ["thinking", "token", "done"]


@pytest.mark.parametrize("failure", ["persist", "output", "incomplete", "exception"])
async def test_failure_never_sends_done_or_persists_partial_answer(failure: str) -> None:
    class BrokenRuntime:
        async def run(self, run_state: AgentState) -> AsyncIterator[StreamEvent]:
            yield StreamEvent.token(run_state, "partial", 1)
            if failure == "exception":
                raise RuntimeError("private-provider-key")
            if failure != "incomplete":
                yield StreamEvent.done(run_state, 2)

    store = MemoryStore()
    store.fail_completion = failure == "persist"
    reader = Reader(RuntimeBudget(max_output_chars=1) if failure == "output" else None)
    accepted = await RunService(store, BrokenRuntime(), reader).accept(
        ChatRequest(message="hello"), state().principal
    )
    async with accepted:
        events = [event async for event in accepted.events]
    assert events[-1].type == "error"
    assert "done" not in [event.type for event in events]
    assert store.answers == []
    assert store.record is not None and store.record.status == "FAILED"
    assert "private" not in "".join(event.model_dump_json() for event in events)


@pytest.mark.parametrize("code", ["PROVIDER_SECRET_123", "private-provider-key"])
async def test_unknown_runtime_failure_code_is_redacted_before_persistence(code: str) -> None:
    class BrokenRuntime:
        async def run(self, run_state: AgentState) -> AsyncIterator[StreamEvent]:
            yield StreamEvent.token(run_state, "partial", 1)
            raise RuntimeFailure(code)

    store = MemoryStore()
    accepted = await RunService(store, BrokenRuntime(), Reader()).accept(
        ChatRequest(message="hello"), state().principal
    )
    async with accepted:
        events = [event async for event in accepted.events]
    assert [event.type for event in events] == ["token", "error"]
    assert events[-1].data == {"code": "AGENT_RUN_FAILED"}
    assert code not in "".join(event.model_dump_json() for event in events)
    assert store.record is not None and store.record.status == "FAILED"
    assert store.record.error_code == "AGENT_RUN_FAILED"
    assert store.answers == []


async def test_timeout_and_cancellation_release_runtime_and_run() -> None:
    class WaitingRuntime:
        def __init__(self) -> None:
            self.started = asyncio.Event()
            self.closed = False

        async def run(self, run_state: AgentState) -> AsyncIterator[StreamEvent]:
            try:
                self.started.set()
                await asyncio.Event().wait()
                yield StreamEvent.done(run_state, 1)
            finally:
                self.closed = True

    for cancel in (False, True):
        store = MemoryStore()
        runtime = WaitingRuntime()
        budget = RuntimeBudget(max_run_seconds=0.05 if not cancel else 30)
        accepted = await RunService(store, runtime, Reader(budget)).accept(
            ChatRequest(message="hello"), state().principal
        )
        async with accepted:
            task = asyncio.ensure_future(anext(accepted.events))
            await asyncio.wait_for(runtime.started.wait(), 1)
            if cancel:
                task.cancel()
                with pytest.raises(asyncio.CancelledError):
                    await task
            else:
                assert (await asyncio.wait_for(task, 1)).type == "error"
        assert runtime.closed
        assert store.record is not None
        assert store.record.status == ("CANCELLED" if cancel else "FAILED")


async def test_close_before_first_event_cancels_accepted_run() -> None:
    store = MemoryStore()
    accepted = await RunService(store, FakeRuntime(), Reader()).accept(
        ChatRequest(message="hello"), state().principal
    )
    await accepted.aclose()
    assert store.record is not None and store.record.status == "CANCELLED"


async def test_close_after_partial_token_discards_answer() -> None:
    store = MemoryStore()
    accepted = await RunService(store, FakeRuntime(), Reader()).accept(
        ChatRequest(message="hello"), state().principal
    )
    async with accepted:
        assert (await anext(accepted.events)).type == "thinking"
        assert (await anext(accepted.events)).type == "token"
    assert store.answers == []
    assert store.record is not None and store.record.status == "CANCELLED"


async def test_event_budget_and_foreign_ids_are_rejected() -> None:
    class NoisyRuntime:
        async def run(self, run_state: AgentState) -> AsyncIterator[StreamEvent]:
            for sequence in range(1, 10):
                yield StreamEvent.thinking(run_state, "waiting", sequence)

    class ForeignRuntime:
        async def run(self, run_state: AgentState) -> AsyncIterator[StreamEvent]:
            yield StreamEvent.token(state(), "foreign answer", 1)

    for runtime, code in (
        (NoisyRuntime(), "AGENT_EVENT_LIMIT"),
        (ForeignRuntime(), "AGENT_RUNTIME_INVALID_EVENT"),
    ):
        store = MemoryStore()
        accepted = await RunService(store, runtime, Reader(RuntimeBudget(max_events=3))).accept(
            ChatRequest(message="hello"), state().principal
        )
        async with accepted:
            events = [event async for event in accepted.events]
        assert events[-1].data["code"] == code
        assert store.answers == []
        assert store.record is not None and store.record.status == "FAILED"


async def test_langgraph_does_not_enable_external_tracing(monkeypatch: pytest.MonkeyPatch) -> None:
    from langsmith import get_tracing_context

    monkeypatch.setenv("LANGSMITH_TRACING", "true")

    class Model:
        async def answer(self, run_state: AgentState) -> str:
            assert get_tracing_context()["enabled"] is False
            return "demo"

    events = [event async for event in LangGraphRuntime(Model()).run(state())]
    assert events[-1].type == "done"


async def test_langgraph_cancellation_reaches_model_client() -> None:
    class WaitingModel:
        def __init__(self) -> None:
            self.started = asyncio.Event()
            self.cancelled = False

        async def answer(self, state: AgentState) -> str:
            try:
                self.started.set()
                await asyncio.Event().wait()
                return "unreachable"
            finally:
                self.cancelled = True

    model = WaitingModel()
    iterator = LangGraphRuntime(model).run(state())
    assert (await anext(iterator)).type == "thinking"
    task = asyncio.ensure_future(anext(iterator))
    await asyncio.wait_for(model.started.wait(), 1)
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task
    await iterator.aclose()
    assert model.cancelled


async def test_store_cannot_target_another_service_schema() -> None:
    from sqlalchemy.ext.asyncio import create_async_engine

    from app.storage.repository import AgentStore

    engine = create_async_engine("mysql+asyncmy://agent:secret@localhost/oj_submission")
    try:
        with pytest.raises(ValueError, match="own oj_agent"):
            AgentStore(engine)
    finally:
        await engine.dispose()
