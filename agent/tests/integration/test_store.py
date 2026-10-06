"""真实 MySQL 的归属、并发、事务和重启/过期收敛。"""

import asyncio
import json
import os
import subprocess
import sys
from collections.abc import AsyncIterator
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import pytest
from sqlalchemy import insert, select, text
from sqlalchemy.exc import DBAPIError
from sqlalchemy.ext.asyncio import AsyncEngine, create_async_engine

from app.core.runtime_config import DemoConfigReader
from app.core.settings import Settings
from app.graphs.runtime import FakeRuntime
from app.graphs.service import RunService
from app.models.runtime import ChatRequest, Principal
from app.storage.repository import (
    ActiveRunConflict,
    AgentStore,
    ConversationNotFound,
    RunNotFound,
    RunRecord,
    RunStateConflict,
    utc_now,
)
from app.storage.schema import agent_conversations, agent_messages, agent_runs

pytestmark = pytest.mark.integration
MIGRATION_ROOT = Path(__file__).resolve().parents[3] / "migrations" / "agent"


@pytest.fixture
async def engine() -> AsyncIterator[AsyncEngine]:
    url = os.environ.get("AGENT_TEST_DATABASE_URL")
    if not url:
        pytest.skip("AGENT_TEST_DATABASE_URL is not set")
    db = create_async_engine(url, pool_size=5, max_overflow=0)
    async with db.begin() as connection:
        for table in (agent_messages, agent_runs, agent_conversations):
            await connection.execute(table.delete())
    try:
        yield db
    finally:
        await db.dispose()


async def new_run(store: AgentStore, conversation_id: str | None = None) -> RunRecord:
    return await store.create_run_with_user_message(
        user_id=7,
        conversation_id=conversation_id,
        request_id="request",
        content="介绍二分查找",
        config_source="demo_environment",
        config_snapshot={"version": "pr2-demo-v1"},
        deadline_at=utc_now() + timedelta(seconds=30),
    )


async def test_owner_isolation_and_latest_history_order(engine: AsyncEngine) -> None:
    store = AgentStore(engine)
    conversation = await store.create_conversation(7)
    for answer in ("first", "second", "third"):
        run = await new_run(store, conversation.id)
        await store.finish_run(user_id=7, run_id=run.id, status="COMPLETED", answer=answer)
    messages = await store.list_messages(7, conversation.id, limit=3)
    assert [message.role for message in messages] == ["assistant", "user", "assistant"]
    assert [message.content for message in messages] == ["second", "介绍二分查找", "third"]
    assert [message.id for message in messages] == sorted(message.id for message in messages)
    for user_id in (8, 999):
        with pytest.raises(ConversationNotFound):
            await store.get_conversation(user_id, conversation.id)
        with pytest.raises(ConversationNotFound):
            await store.list_messages(user_id, conversation.id)
        with pytest.raises(ConversationNotFound):
            await store.create_run_with_user_message(
                user_id=user_id,
                conversation_id=conversation.id,
                request_id="request",
                content="hi",
                config_source="demo_environment",
                config_snapshot={},
                deadline_at=utc_now() + timedelta(seconds=30),
            )
        with pytest.raises(RunNotFound):
            await store.finish_run(user_id=user_id, run_id=run.id, status="FAILED")
    with pytest.raises(ConversationNotFound):
        await store.get_conversation(7, uuid4())


async def test_same_conversation_concurrent_run_is_unique(engine: AsyncEngine) -> None:
    store = AgentStore(engine)
    conversation = await store.create_conversation(7)
    results = await asyncio.gather(
        *(new_run(store, conversation.id) for _ in range(10)), return_exceptions=True
    )
    accepted = [result for result in results if not isinstance(result, BaseException)]
    assert len(accepted) == 1
    assert sum(isinstance(result, ActiveRunConflict) for result in results) == 9
    assert len(await store.list_messages(7, conversation.id)) == 1
    await store.finish_run(user_id=7, run_id=accepted[0].id, status="CANCELLED")
    next_run = await new_run(store, conversation.id)
    assert next_run.id != accepted[0].id


async def test_initial_message_failure_rolls_back_new_conversation_and_run(
    engine: AsyncEngine,
) -> None:
    # 强制用户消息插入失败，验证接受请求是一个事务，而不是三个独立提交。
    async with engine.begin() as connection:
        await connection.execute(
            text(
                "ALTER TABLE agent_messages ADD CONSTRAINT agent_test_fail_message "
                "CHECK (role <> 'user')"
            )
        )
    try:
        with pytest.raises(DBAPIError, match="agent_test_fail_message"):
            await new_run(AgentStore(engine))
        async with engine.connect() as connection:
            for table in (agent_conversations, agent_runs, agent_messages):
                assert (await connection.execute(select(table))).first() is None
    finally:
        async with engine.begin() as connection:
            await connection.execute(
                text("ALTER TABLE agent_messages DROP CHECK agent_test_fail_message")
            )


async def test_final_message_failure_rolls_back_completed_status(engine: AsyncEngine) -> None:
    store = AgentStore(engine)
    run = await new_run(store)
    async with engine.begin() as connection:
        await connection.execute(
            text(
                "ALTER TABLE agent_messages ADD CONSTRAINT agent_test_fail_answer "
                "CHECK (role <> 'assistant')"
            )
        )
    try:
        with pytest.raises(DBAPIError, match="agent_test_fail_answer"):
            await store.finish_run(user_id=7, run_id=run.id, status="COMPLETED", answer="answer")
        assert (await store.get_run(7, run.id)).status == "RUNNING"
        assert len(await store.list_messages(7, run.conversation_id)) == 1
    finally:
        async with engine.begin() as connection:
            await connection.execute(
                text("ALTER TABLE agent_messages DROP CHECK agent_test_fail_answer")
            )
    await store.finish_run(user_id=7, run_id=run.id, status="FAILED", error_code="AGENT_RUN_FAILED")
    assert (await store.get_run(7, run.id)).status == "FAILED"


async def test_terminal_state_cannot_be_overwritten_or_partial_answer_persisted(
    engine: AsyncEngine,
) -> None:
    store = AgentStore(engine)
    run = await new_run(store)
    with pytest.raises(RunStateConflict):
        await store.finish_run(user_id=7, run_id=run.id, status="FAILED", answer="partial")
    await store.finish_run(user_id=7, run_id=run.id, status="COMPLETED", answer="complete")
    with pytest.raises(RunStateConflict):
        await store.finish_run(user_id=7, run_id=run.id, status="COMPLETED", answer="duplicate")
    with pytest.raises(RunStateConflict):
        await store.finish_run(user_id=7, run_id=run.id, status="CANCELLED")
    messages = await store.list_messages(7, run.conversation_id)
    assert [message.content for message in messages] == ["介绍二分查找", "complete"]


async def test_restart_and_deadline_release_active_run(engine: AsyncEngine) -> None:
    store = AgentStore(engine)
    run = await new_run(store)
    restarted = AgentStore(engine)
    assert await restarted.interrupt_running_runs() == 1
    assert await restarted.interrupt_running_runs() == 0
    assert (await restarted.get_run(7, run.id)).status == "INTERRUPTED"
    recovered = await new_run(restarted, run.conversation_id)
    clock = recovered.deadline_at + timedelta(seconds=1)
    later = AgentStore(engine, clock=lambda: clock)
    assert await later.expire_running_runs() == 1
    assert (await later.get_run(7, recovered.id)).error_code == "AGENT_DEADLINE_EXCEEDED"
    assert len(await later.list_messages(7, run.conversation_id)) == 2
    # cleanup 释放的是该 Run 的生成列，不会清除后来新运行的关联。
    next_run = await new_run(store, run.conversation_id)
    assert (await store.get_run(7, next_run.id)).status == "RUNNING"


async def test_database_unique_index_enforces_active_run_without_process_lock(
    engine: AsyncEngine,
) -> None:
    store = AgentStore(engine)
    run = await new_run(store)
    from sqlalchemy.exc import IntegrityError

    async with engine.begin() as connection:
        with pytest.raises(IntegrityError):
            await connection.execute(
                insert(agent_runs).values(
                    id=str(uuid4()),
                    conversation_id=run.conversation_id,
                    user_id=7,
                    request_id="other",
                    config_source="demo_environment",
                    config_snapshot={},
                    status="RUNNING",
                    started_at=utc_now(),
                    deadline_at=utc_now() + timedelta(seconds=30),
                )
            )


async def test_fake_service_persists_and_restores_conversation(engine: AsyncEngine) -> None:
    store = AgentStore(engine)
    service = RunService(
        store, FakeRuntime("演示回答"), DemoConfigReader(Settings(runtime_mode="fake"))
    )
    assert await service.initialize() == 0
    principal = Principal(user_id=7, request_id="request")
    accepted = await service.accept(ChatRequest(message="解释算法"), principal)
    async with accepted:
        events = [event async for event in accepted.events]
    assert events[-1].type == "done"
    conversation_id = accepted.state.conversation_id
    second = await service.accept(
        ChatRequest(conversation_id=conversation_id, message="继续"), principal
    )
    assert [message.content for message in second.state.history] == ["解释算法", "演示回答"]
    async with second:
        events = [event async for event in second.events]
    assert events[-1].type == "done"
    assert [message.role for message in await store.list_messages(7, conversation_id)] == [
        "user",
        "assistant",
        "user",
        "assistant",
    ]


async def test_down_and_up_migration_are_reversible_in_empty_test_database(
    engine: AsyncEngine,
) -> None:
    async with engine.begin() as connection:
        for direction in ("down", "up"):
            sql = (MIGRATION_ROOT / f"000001_create_agent_runtime.{direction}.sql").read_text()
            for statement in sql.split(";"):
                if statement.strip():
                    await connection.execute(text(statement))
    run = await new_run(AgentStore(engine))
    assert run.status == "RUNNING"


async def test_demo_cli_explicit_fake_and_conversation_restore(engine: AsyncEngine) -> None:
    environment = dict(os.environ)
    environment.update(
        AGENT_DATABASE_URL=os.environ["AGENT_TEST_DATABASE_URL"],
        AGENT_RUNTIME_MODE="langgraph_fake",
    )
    command = [sys.executable, "-m", "app.demo", "--user-id", "7", "--message", "介绍算法"]
    result = await asyncio.to_thread(
        subprocess.run,
        command,
        env=environment,
        capture_output=True,
        text=True,
        timeout=15,
    )
    assert result.returncode == 0, result.stderr
    events = [json.loads(line) for line in result.stdout.splitlines()]
    assert [event["type"] for event in events] == ["thinking", "token", "done"]
    conversation_id = events[-1]["conversation_id"]
    assert "演示回答" in events[1]["data"]["text"]
    restored = await asyncio.to_thread(
        subprocess.run,
        [*command, "--conversation-id", conversation_id],
        env=environment,
        capture_output=True,
        text=True,
        timeout=15,
    )
    assert restored.returncode == 0, restored.stderr
    assert len(await AgentStore(engine).list_messages(7, conversation_id)) == 4
    environment["AGENT_MAX_OUTPUT_CHARS"] = "1"
    failed = await asyncio.to_thread(
        subprocess.run,
        command,
        env=environment,
        capture_output=True,
        text=True,
        timeout=15,
    )
    assert failed.returncode == 2
    assert json.loads(failed.stdout.splitlines()[-1])["type"] == "error"
    assert "Traceback" not in failed.stderr
    assert "agent-password" not in failed.stderr
