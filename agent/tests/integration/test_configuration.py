"""配置的真实 MySQL 事务、并发、版本引用、鉴权与运行快照。"""

import asyncio
import json
import os
import subprocess
import sys
from collections.abc import AsyncIterator
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any
from uuid import UUID, uuid4

import pytest
from pydantic import BaseModel
from sqlalchemy import text
from sqlalchemy.exc import DBAPIError
from sqlalchemy.ext.asyncio import AsyncEngine, create_async_engine

from app.core.runtime_config import DatabaseConfigReader
from app.core.settings import Settings
from app.graphs.runtime import FakeRuntime
from app.graphs.service import RunService
from app.models.configuration import AgentConfig, ConfigError, ConfigWrite, PromptConfig
from app.models.runtime import ChatContext, ChatRequest, Principal
from app.storage.configuration import ConfigurationStore, ConfigVersion
from app.storage.repository import AgentStore, ConversationAgentConflict, ConversationNotFound
from app.tools.registry import ToolDefinition, ToolRegistry, ToolSpec

pytestmark = pytest.mark.integration
EXAMPLE_PATH = Path(__file__).resolve().parents[2] / "examples/learning-assistant.json"


@pytest.fixture
async def config_engine() -> AsyncIterator[AsyncEngine]:
    url = os.environ.get("AGENT_TEST_DATABASE_URL")
    if not url:
        pytest.skip("AGENT_TEST_DATABASE_URL is not set")
    engine = create_async_engine(url, pool_size=10, max_overflow=0)
    try:
        yield engine
    finally:
        await engine.dispose()


def registry() -> ToolRegistry:
    value = ToolRegistry()

    async def handle(arguments: BaseModel, principal: Principal) -> BaseModel:
        return arguments

    for name in ("read_first", "read_second", "read_third"):
        value.register(
            ToolDefinition(
                ToolSpec(name=name, description=name, read_scope="public"),
                BaseModel,
                BaseModel,
                handle,
            )
        )
    return value


def write(
    kind: str, key: str, expected: str | None = None, request: str | None = None
) -> ConfigWrite:
    return ConfigWrite.model_validate(
        {
            "kind": kind,
            "key": key,
            "expected_id": expected,
            "actor_id": 7,
            "request_id": request or str(uuid4()),
        }
    )


async def setup_agent(store: ConfigurationStore, *, admin: bool = False) -> ConfigVersion:
    prefix = "test_" + uuid4().hex
    prompt = await store.create_or_replace(write("prompt", prefix + "_p"), {"text": "旧提示"})
    model = await store.create_or_replace(write("model", prefix + "_m"), {})
    skill = await store.create_or_replace(
        write("skill", prefix + "_s"),
        {
            "name": "诊断",
            "prompt_id": prompt.id,
            "allowed_tools": ["read_first", "read_second"],
            "budget": {"max_run_seconds": 10, "max_tool_calls": 0},
        },
    )
    return await store.create_or_replace(
        write("agent", prefix),
        {
            "name": "学习助手",
            "prompt_id": prompt.id,
            "skill_ids": [skill.id],
            "default_skill_id": skill.id,
            "model_profile_id": model.id,
            "allowed_tools": ["read_second", "read_third"],
            "visibility": "admin" if admin else "user",
            "budget": {"max_output_chars": 200, "max_run_seconds": 20},
        },
    )


async def test_replace_cas_replay_and_retained_history(config_engine: AsyncEngine) -> None:
    store = ConfigurationStore(config_engine, registry())
    key = "test_" + uuid4().hex
    first_write = write("prompt", key)
    first = await store.create_or_replace(first_write, {"text": "one"})
    second = await store.create_or_replace(write("prompt", key, first.id), {"text": "two"})
    replay = await store.create_or_replace(first_write, {"text": "one"})
    assert replay.id == first.id and replay.content == PromptConfig(text="one")
    assert (await store.get_version(first.id)).archived
    assert (await store.resolve("prompt", key)).id == second.id
    with pytest.raises(ConfigError, match="REQUEST_CONFLICT"):
        await store.create_or_replace(first_write, {"text": "different"})
    with pytest.raises(ConfigError, match="REQUEST_CONFLICT"):
        await store.create_or_replace(
            write("prompt", key + "_other", request=first_write.request_id), {"text": "one"}
        )
    results = await asyncio.gather(
        *(
            store.create_or_replace(write("prompt", key, second.id), {"text": f"candidate-{i}"})
            for i in range(6)
        ),
        return_exceptions=True,
    )
    assert sum(isinstance(result, ConfigVersion) for result in results) == 1
    assert (
        sum(
            isinstance(result, ConfigError) and result.code == "AGENT_CONFIGURATION_STALE"
            for result in results
        )
        == 5
    )
    versions = await store.list_versions("prompt", key)
    assert len(versions) == 3 and len({version.id for version in versions}) == 3
    assert len(await store.list_versions("prompt", key, limit=1, offset=1)) == 1
    with pytest.raises(ConfigError):
        await store.list_versions("prompt", key, limit=101)


async def test_archived_bindings_and_dependency_disable(config_engine: AsyncEngine) -> None:
    store = ConfigurationStore(config_engine, registry())
    agent = await setup_agent(store)
    assert isinstance(agent.content, AgentConfig)
    old_prompt = await store.get_version(str(agent.content.prompt_id))
    new_prompt = await store.create_or_replace(
        write("prompt", old_prompt.key, old_prompt.id), {"text": "新提示"}
    )
    root, graph = await store.load_graph(agent.key)
    assert root.id == agent.id
    assert graph[old_prompt.id].archived
    assert graph[old_prompt.id].content == PromptConfig(text="旧提示")
    changed = await store.create_or_replace(
        write("agent", agent.key, agent.id), agent.content.model_dump(mode="json")
    )
    with pytest.raises(ConfigError, match="REFERENCE_INVALID"):
        await store.create_or_replace(
            write("agent", agent.key + "_copy"), changed.content.model_dump(mode="json")
        )
    state_write = write("prompt", new_prompt.key, new_prompt.id)
    await store.change_state(state_write, "disable")
    with pytest.raises(ConfigError, match="NOT_FOUND"):
        await store.load_graph(agent.key)
    await store.change_state(write("prompt", new_prompt.key, new_prompt.id), "enable")
    assert (await store.load_graph(agent.key))[0].id == changed.id
    await store.change_state(write("agent", agent.key, changed.id), "archive")
    with pytest.raises(ConfigError, match="NOT_FOUND"):
        await store.load_graph(agent.key)
    with pytest.raises(ConfigError, match="NOT_FOUND"):
        await store.resolve("agent", agent.key)
    assert (await store.get_version(changed.id)).archived
    restored_write = write("agent", agent.key, changed.id).model_copy(
        update={"source_id": UUID(agent.id)}
    )
    restored = await store.create_or_replace(restored_write, agent.content.model_dump(mode="json"))
    assert restored.id not in (agent.id, changed.id)
    assert (await store.load_graph(agent.key))[0].id == restored.id


async def test_failed_transaction_and_invalid_tool_or_reference(config_engine: AsyncEngine) -> None:
    store = ConfigurationStore(config_engine, registry())
    agent = await setup_agent(store)
    body = agent.content.model_dump(mode="json")
    for changes in (
        {"allowed_tools": ["not_registered"]},
        {"prompt_id": body["model_profile_id"]},
        {"skill_ids": [str(uuid4())], "default_skill_id": body["default_skill_id"]},
    ):
        with pytest.raises(ValueError):
            await store.create_or_replace(write("agent", agent.key, agent.id), {**body, **changes})
    assert (await store.resolve("agent", agent.key)).id == agent.id
    failing_write = write("agent", agent.key, agent.id)
    async with config_engine.begin() as connection:
        await connection.execute(
            text(
                "ALTER TABLE agent_config_audits ADD CONSTRAINT agent_test_fail_config_audit "
                f"CHECK (request_id <> '{failing_write.request_id}')"
            )
        )
    try:
        with pytest.raises(DBAPIError):
            await store.create_or_replace(failing_write, body)
        assert not (await store.get_version(agent.id)).archived
        assert (await store.resolve("agent", agent.key)).id == agent.id
        assert len(await store.list_versions("agent", agent.key)) == 1
    finally:
        async with config_engine.begin() as connection:
            await connection.execute(
                text("ALTER TABLE agent_config_audits DROP CHECK agent_test_fail_config_audit")
            )


async def test_runtime_auth_budget_snapshot_and_conversation_isolation(
    config_engine: AsyncEngine,
) -> None:
    tools = registry()
    store = ConfigurationStore(config_engine, tools)
    agent = await setup_agent(store)
    private = await setup_agent(store, admin=True)
    settings = Settings(runtime_mode="fake", default_agent_key=agent.key)
    reader = DatabaseConfigReader(store, settings, tools)
    principal = Principal(user_id=7, request_id="test-request")
    request = ChatRequest(message="介绍算法")
    snapshot = await reader.read(request, principal)
    assert snapshot.source == "database" and snapshot.version == agent.id
    assert snapshot.allowed_tools == ("read_second",)
    assert snapshot.budget.max_tool_calls == 0 and snapshot.budget.max_run_seconds == 10
    assert snapshot.budget.max_output_chars == 200 and snapshot.config_hash
    skill_key = snapshot.skill_key
    assert (
        await reader.read(ChatRequest(message="test", skill_key=skill_key), principal)
    ).skill_id == snapshot.skill_id
    with pytest.raises(ConfigError, match="NOT_FOUND"):
        await reader.read(ChatRequest(message="test", agent_key=private.key), principal)
    assert (
        await reader.read(
            ChatRequest(message="test", agent_key=private.key),
            principal.model_copy(update={"roles": frozenset({"agent_admin"})}),
        )
    ).version == private.id
    with pytest.raises(ConfigError, match="NOT_FOUND"):
        await reader.read(ChatRequest(message="test", skill_key="not_bound"), principal)
    runtime_store = AgentStore(config_engine)
    service = RunService(runtime_store, FakeRuntime(), reader)
    accepted = await service.accept(request, principal)
    assert accepted.state.agent_key == agent.key
    updated_body = agent.content.model_dump(mode="json")
    updated_body["name"] = "新的助手"
    updated = await store.create_or_replace(write("agent", agent.key, agent.id), updated_body)
    async with accepted:
        events = [event async for event in accepted.events]
    assert events[-1].type == "done"
    run = await runtime_store.get_run(7, accepted.state.run_id)
    assert run.config_snapshot["version"] == agent.id
    assert (await reader.read(request, principal)).version == updated.id
    with pytest.raises(ConversationAgentConflict):
        await service.accept(
            ChatRequest(
                message="test",
                agent_key=private.key,
                conversation_id=accepted.state.conversation_id,
            ),
            principal.model_copy(update={"roles": frozenset({"system_admin"})}),
        )
    with pytest.raises(ConversationNotFound):
        await service.accept(
            ChatRequest(message="test", conversation_id=accepted.state.conversation_id),
            principal.model_copy(update={"user_id": 8}),
        )


async def test_test_agent_expiration_and_prompt_variables(config_engine: AsyncEngine) -> None:
    tools = registry()
    store = ConfigurationStore(config_engine, tools)
    agent = await setup_agent(store)
    body = agent.content.model_dump(mode="json")
    body.update(
        visibility="admin",
        is_test=True,
        test_expires_at=(datetime.now(UTC) + timedelta(hours=1)).isoformat(),
    )
    test_agent = await store.create_or_replace(write("agent", agent.key, agent.id), body)
    reader = DatabaseConfigReader(store, Settings(runtime_mode="fake"), tools)
    principal = Principal(user_id=7, roles=frozenset({"system_admin"}), request_id="test")
    assert (await reader.read(ChatRequest(message="hi", agent_key=agent.key), principal)).is_test
    content = body.copy()
    content["test_expires_at"] = (datetime.now(UTC) - timedelta(hours=1)).isoformat()
    with pytest.raises(ConfigError, match="EXPIRED"):
        await store.create_or_replace(write("agent", agent.key, test_agent.id), content)
    # 时钟前进不修改配置；过期测试 Agent 对管理员同样拒绝。
    from unittest.mock import patch

    class Later(datetime):
        @classmethod
        def now(cls, tz: Any = None) -> "Later":
            return cls.fromtimestamp(datetime.now(UTC).timestamp() + 7200, tz=UTC)

    with patch("datetime.datetime", Later):
        with pytest.raises(ConfigError, match="NOT_FOUND"):
            await reader.read(ChatRequest(message="hi", agent_key=agent.key), principal)
    prompt = await store.create_or_replace(
        write("prompt", "test_" + uuid4().hex),
        {"text": "使用 {language}", "variables": ["language"]},
    )
    body.update(prompt_id=prompt.id)
    await store.create_or_replace(write("agent", agent.key, test_agent.id), body)
    with pytest.raises(ConfigError, match="CONTEXT_REQUIRED"):
        await reader.read(ChatRequest(message="hi", agent_key=agent.key), principal)
    snapshot = await reader.read(
        ChatRequest(message="hi", agent_key=agent.key, context=ChatContext(language="cpp")),
        principal,
    )
    assert snapshot.prompt_text == "使用 cpp"


async def test_configuration_cli_bootstrap_retry_and_secret_redaction(
    config_engine: AsyncEngine,
) -> None:
    document = json.loads(await asyncio.to_thread(EXAMPLE_PATH.read_text))
    prefix = "test_" + uuid4().hex
    for name, item in document.items():
        item["key"] = prefix + "_" + name
    from tempfile import TemporaryDirectory

    with TemporaryDirectory() as directory:
        path = Path(directory) / "bootstrap.json"
        path.write_text(json.dumps(document))
        env = {**os.environ, "AGENT_DATABASE_URL": os.environ["AGENT_TEST_DATABASE_URL"]}
        command = [
            sys.executable,
            "-m",
            "app.config_cli",
            "bootstrap",
            "--file",
            str(path),
            "--actor-id",
            "7",
            "--request-id",
            prefix,
        ]
        results = []
        for _ in range(2):
            result = await asyncio.to_thread(
                subprocess.run, command, env=env, capture_output=True, text=True, timeout=15
            )
            assert result.returncode == 0, result.stderr
            results.append(json.loads(result.stdout))
        assert results[0] == results[1]
        env["AGENT_DATABASE_URL"] = os.environ["AGENT_TEST_BAD_DATABASE_URL"]
        failed = await asyncio.to_thread(
            subprocess.run, command, env=env, capture_output=True, text=True, timeout=15
        )
        assert failed.returncode == 2 and "AGENT_CONFIGURATION_FAILED" in failed.stderr
        assert "wrong-password" not in failed.stderr and "Traceback" not in failed.stderr
