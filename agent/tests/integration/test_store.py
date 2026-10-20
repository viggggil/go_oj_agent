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
from app.models.memory import ConversationSummary
from app.models.runtime import ChatRequest, Principal
from app.storage.repository import (
    ActiveRunConflict,
    AgentStore,
    ConversationNotFound,
    RunNotFound,
    RunRecord,
    RunStateConflict,
    SummaryVersionConflict,
    utc_now,
)
from app.storage.schema import (
    agent_conversation_summaries,
    agent_conversations,
    agent_messages,
    agent_runs,
)

pytestmark = pytest.mark.integration
MIGRATION_ROOT = Path(__file__).resolve().parents[3] / "migrations" / "agent"


@pytest.fixture
async def engine() -> AsyncIterator[AsyncEngine]:
    url = os.environ.get("AGENT_TEST_DATABASE_URL")
    if not url:
        pytest.skip("AGENT_TEST_DATABASE_URL is not set")
    db = create_async_engine(url, pool_size=5, max_overflow=0)
    async with db.begin() as connection:
        for table in (
            agent_conversation_summaries,
            agent_messages,
            agent_runs,
            agent_conversations,
        ):
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
    recent = await store.conversation_messages(7, conversation.id)
    assert [item["content"] for item in recent["items"]] == [
        "介绍二分查找",
        "second",
        "介绍二分查找",
        "third",
    ]
    older = await store.conversation_messages(7, conversation.id, recent["next_before"])
    assert [item["content"] for item in older["items"]] == ["介绍二分查找", "first"]
    assert older["next_before"] is None
    assert (await store.list_conversations(7, 1))["items"][0]["title"] == "介绍二分查找"
    for user_id in (8, 999):
        assert (await store.list_conversations(user_id, 1))["items"] == []
        with pytest.raises(ConversationNotFound):
            await store.conversation_messages(user_id, conversation.id)
        with pytest.raises(ConversationNotFound):
            await store.get_conversation(user_id, conversation.id)
        with pytest.raises(ConversationNotFound):
            await store.list_messages(user_id, conversation.id)
        with pytest.raises(ConversationNotFound):
            await store.list_completed_turns(user_id, conversation.id)
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


async def test_model_history_pairs_only_completed_runs_and_caps_recent_turns(
    engine: AsyncEngine,
) -> None:
    store = AgentStore(engine)
    conversation = await store.create_conversation(7)
    for index in range(22):
        run = await new_run(store, conversation.id)
        await store.finish_run(
            user_id=7, run_id=run.id, status="COMPLETED", answer=f"answer {index}"
        )
    for status in ("FAILED", "CANCELLED", "INTERRUPTED"):
        run = await new_run(store, conversation.id)
        await store.finish_run(user_id=7, run_id=run.id, status=status)
        # 即便旧数据错误地留有部分 assistant，也必须按 Run 状态排除。
        async with engine.begin() as connection:
            await connection.execute(
                insert(agent_messages).values(
                    conversation_id=conversation.id,
                    run_id=run.id,
                    role="assistant",
                    content="invalid partial answer",
                    created_at=utc_now(),
                )
            )
    # 模拟旧数据中的 tool/无 Run 消息，以及成功 Run 的残缺问答。
    incomplete = await new_run(store, conversation.id)
    await store.finish_run(user_id=7, run_id=incomplete.id, status="COMPLETED", answer="deleted")
    async with engine.begin() as connection:
        await connection.execute(
            agent_messages.delete().where(
                (agent_messages.c.run_id == incomplete.id) & (agent_messages.c.role == "assistant")
            )
        )
        await connection.execute(
            insert(agent_messages).values(
                conversation_id=conversation.id,
                role="tool",
                content="not model history",
                created_at=utc_now(),
            )
        )
    await new_run(store, conversation.id)  # 本轮 RUNNING 用户消息不进入历史。
    turns = await store.list_completed_turns(7, conversation.id, limit=200)
    assert turns == [("介绍二分查找", f"answer {index}") for index in range(2, 22)]
    assert await store.list_completed_turns(7, conversation.id, limit=1) == turns[-1:]
    other = await store.create_conversation(7)
    assert await store.list_completed_turns(7, other.id) == []


async def test_summary_is_owner_scoped_and_advances_only_completed_boundaries(
    engine: AsyncEngine,
) -> None:
    store = AgentStore(engine)
    conversation = await store.create_conversation(7)
    run = await new_run(store, conversation.id)
    await store.finish_run(user_id=7, run_id=run.id, status="COMPLETED", answer="answer")
    turns = await store.list_completed_turn_records(7, conversation.id)
    summary = await store.save_conversation_summary(
        user_id=7,
        conversation_id=conversation.id,
        summary=ConversationSummary(goal="定位问题"),
        covered_until_message_id=turns[0].assistant_message_id,
        expected_version=None,
    )
    assert summary.version == 1
    loaded = await store.get_conversation_summary(7, conversation.id)
    assert loaded is not None
    assert loaded.summary == summary.summary
    assert loaded.covered_until_message_id == summary.covered_until_message_id
    assert loaded.version == summary.version
    for index in range(2, 5):
        later = await new_run(store, conversation.id)
        await store.finish_run(
            user_id=7, run_id=later.id, status="COMPLETED", answer=f"answer {index}"
        )
    assert [
        turn.assistant_content
        for turn in await store.list_completed_turn_records(
            7, conversation.id, after_message_id=summary.covered_until_message_id
        )
    ] == ["answer 2", "answer 3", "answer 4"]
    assert [
        turn.assistant_content
        for turn in await store.list_completed_turn_records(
            7,
            conversation.id,
            after_message_id=summary.covered_until_message_id,
            oldest_first=True,
        )
    ] == ["answer 2", "answer 3", "answer 4"]
    with pytest.raises(SummaryVersionConflict):
        await store.save_conversation_summary(
            user_id=7,
            conversation_id=conversation.id,
            summary=ConversationSummary(goal="回退"),
            covered_until_message_id=turns[0].user_message_id,
            expected_version=summary.version,
        )
    with pytest.raises(ConversationNotFound):
        await store.get_conversation_summary(8, conversation.id)


async def test_failed_run_restores_only_safe_tool_trace(engine: AsyncEngine) -> None:
    store = AgentStore(engine)
    run = await new_run(store)
    trace = {
        "run_id": run.id,
        "step": 1,
        "call_id": "call_problem",
        "tool_name": "get_problem",
        "status": "succeeded",
        "parameter_validation": "valid",
        "duration_ms": 12,
        "summary": "已读取题目",
        "evidence_refs": ["internal-evidence"],
    }
    await store.finish_run(
        user_id=7,
        run_id=run.id,
        status="FAILED",
        error_code="AGENT_MODEL_TIMEOUT",
        model_summary={"tool_steps": [trace]},
    )
    history = await store.conversation_messages(7, run.conversation_id)
    assert len(history["items"]) == 1
    message = history["items"][0]
    assert message["role"] == "user" and message["status"] == "FAILED"
    assert message["tool_steps"] == [
        {
            key: trace[key]
            for key in ("step", "call_id", "tool_name", "status", "duration_ms", "summary")
        }
    ]
    assert await store.list_completed_turns(7, run.conversation_id) == []
    with pytest.raises(ConversationNotFound):
        await store.conversation_messages(8, run.conversation_id)


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
        # 此测试明确使用空库；先清理所有配置引用后验证 down/up。
        for statement in (
            "DELETE FROM agent_conversation_summaries",
            "UPDATE agent_config_resources SET current_id=NULL",
            "DELETE FROM agent_provider_credentials",
            "DELETE FROM agent_config_links",
            "DELETE FROM agent_config_audits",
            "UPDATE agent_config_versions SET previous_id=NULL",
            "DELETE FROM agent_config_versions",
            "DELETE FROM agent_config_resources",
            "DELETE FROM agent_credential_audits",
            "DELETE FROM agent_credentials",
        ):
            await connection.execute(text(statement))
        for name, direction in (
            ("000009_create_conversation_summaries", "down"),
            ("000008_create_eval_model_reviews", "down"),
            ("000007_create_eval_reviews", "down"),
            ("000006_create_eval_runs", "down"),
            ("000005_create_eval", "down"),
            ("000004_create_knowledge", "down"),
            ("000003_create_model_provider", "down"),
            ("000002_create_agent_configuration", "down"),
            ("000001_create_agent_runtime", "down"),
            ("000001_create_agent_runtime", "up"),
            ("000002_create_agent_configuration", "up"),
            ("000003_create_model_provider", "up"),
            ("000004_create_knowledge", "up"),
            ("000005_create_eval", "up"),
            ("000006_create_eval_runs", "up"),
            ("000007_create_eval_reviews", "up"),
            ("000008_create_eval_model_reviews", "up"),
            ("000009_create_conversation_summaries", "up"),
        ):
            sql = (MIGRATION_ROOT / f"{name}.{direction}.sql").read_text()
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
