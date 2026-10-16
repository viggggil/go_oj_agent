"""真实 MySQL/MinIO/Qdrant/BGE 验证发布、幂等、失租、故障和管理员权限。"""

import asyncio
import os
from collections.abc import AsyncIterator
from datetime import timedelta
from pathlib import Path
from typing import Any
from uuid import UUID, uuid4

import httpx
import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from pydantic import SecretStr, ValidationError
from sqlalchemy import select, update
from sqlalchemy.ext.asyncio import create_async_engine

from app.core.auth import DelegationVerifier
from app.core.settings import Settings
from app.main import create_app
from app.models.configuration import ConfigError
from app.models.runtime import Principal
from app.rag.clients import RagClients
from app.rag.models import SearchInput, Upload
from app.rag.schema import tasks, versions
from app.rag.service import KnowledgeService
from app.rag.store import KnowledgeStore, now
from app.rag.worker import process_one
from app.storage.configuration import ConfigurationStore
from tests.integration.test_configuration import registry, setup_agent, write
from tests.test_chat import token

pytestmark = pytest.mark.integration


@pytest.fixture
async def rag() -> AsyncIterator[tuple[KnowledgeStore, RagClients, Settings]]:
    url = os.environ.get("AGENT_TEST_RAG_DATABASE_URL")
    if url is None:
        pytest.skip("Run make rag-test-integration for real RAG dependencies")
    settings = Settings(
        database_url=SecretStr(url),
        rag_minio_endpoint=os.environ["AGENT_TEST_RAG_MINIO_ENDPOINT"],
        rag_qdrant_url=os.environ["AGENT_TEST_RAG_QDRANT_URL"],
        rag_embedding_url=os.environ["AGENT_TEST_RAG_EMBEDDING_URL"],
    )
    engine = create_async_engine(url, pool_size=5, max_overflow=0)
    clients = RagClients(settings)
    try:
        if not await asyncio.to_thread(clients.objects.bucket_exists, clients.bucket):
            await asyncio.to_thread(clients.objects.make_bucket, clients.bucket)
        yield KnowledgeStore(engine), clients, settings
    finally:
        await clients.close()
        await engine.dispose()


def principal() -> Principal:
    return Principal(user_id=7, roles=frozenset({"admin"}), request_id=str(uuid4()))


async def upload(store: KnowledgeStore, clients: RagClients) -> tuple[str, dict[str, Any]]:
    base = await store.create_base(principal(), "测试知识库")
    base_id = base["knowledge_base"]["id"]
    body = Upload(
        base_id=UUID(base_id),
        title="单调队列",
        filename="queue.md",
        content=(
            "# 滑动窗口最大值\n\n使用单调递减双端队列，移除过期下标和被新值支配的队尾。"
            "每个元素最多进出一次，时间 O(n)，空间 O(k)。\n"
        ),
    )
    actor = principal()
    results = await asyncio.gather(*(store.upload(actor, body, clients) for _ in range(3)))
    assert results[0] == results[1] == results[2]
    with pytest.raises(ConfigError, match="REQUEST_CONFLICT"):
        await store.upload(actor, body.model_copy(update={"title": "冲突标题"}), clients)
    return base_id, results[0]["document"]


async def drain(store: KnowledgeStore, clients: RagClients) -> None:
    for _ in range(20):
        if not await process_one(store, clients):
            return
    raise AssertionError("Tasks did not converge")


async def test_real_ingestion_replay_version_switch_and_archive(
    rag: tuple[KnowledgeStore, RagClients, Settings],
) -> None:
    store, clients, _ = rag
    base_id, doc = await upload(store, clients)
    assert not await store.active([base_id], None)
    with pytest.raises(ConfigError, match="NOT_READY"):
        await store.change(principal(), doc["document_id"], doc["id"], "enable")
    await drain(store, clients)
    ready = await store.detail(doc["document_id"])
    assert ready["state"] == "READY" and ready["chunk_count"] > 0
    await store.change(principal(), doc["document_id"], doc["id"], "enable")
    service = KnowledgeService(store, clients)
    query = SearchInput(base_ids=[UUID(base_id)], query="如何用单调队列求窗口最大值")
    matches = await service.search(query)
    assert matches and matches[0]["version_id"] == doc["id"]
    assert "单调" in matches[0]["text"]
    assert not await service.search(query.model_copy(update={"base_ids": [uuid4()]}))
    replacement = await store.upload(
        principal(),
        Upload(
            base_id=UUID(base_id),
            document_id=UUID(doc["document_id"]),
            expected_version=UUID(doc["id"]),
            title="新版本",
            filename="v2.md",
            content="# 新版本\n滑动窗口最大值仍然采用单调队列，新版本证据。",
        ),
        clients,
    )
    new = replacement["document"]
    assert (await service.search(query))[0]["version_id"] == doc["id"]
    await drain(store, clients)
    assert (await service.search(query))[0]["version_id"] == new["id"]
    await store.change(principal(), doc["document_id"], new["id"], "archive")
    assert not await service.search(query)
    with pytest.raises(ConfigError, match="STALE"):
        await store.change(principal(), doc["document_id"], doc["id"], "enable")


async def test_lost_lease_crash_retry_and_partial_failure(
    rag: tuple[KnowledgeStore, RagClients, Settings],
) -> None:
    store, clients, _ = rag
    _, doc = await upload(store, clients)
    old = await store.claim()
    assert old and old["id"] == doc["id"]
    async with store.engine.begin() as conn:
        await conn.execute(
            update(tasks)
            .where(tasks.c.version_id == doc["id"])
            .values(lease_until=now() - timedelta(seconds=1))
        )
    new = await store.claim()
    assert new and new["lease_token"] != old["lease_token"]
    await store.finish(old, 99, None)
    assert (await store.detail(doc["document_id"]))["active_version"] is None
    await store.finish(new, 0, "AGENT_RAG_DEPENDENCY_UNAVAILABLE")
    assert (await store.detail(doc["document_id"]))["state"] == "PENDING"
    async with store.engine.begin() as conn:
        await conn.execute(
            update(tasks)
            .where(tasks.c.version_id == doc["id"])
            .values(attempts=3, next_attempt=now())
        )
    attempt = await store.claim()
    assert attempt
    await store.finish(attempt, 0, "AGENT_RAG_DEPENDENCY_UNAVAILABLE")
    assert (await store.detail(doc["document_id"]))["state"] == "FAILED"
    await store.change(principal(), doc["document_id"], doc["id"], "retry")
    await drain(store, clients)
    assert (await store.detail(doc["document_id"]))["state"] == "READY"
    async with store.engine.connect() as conn:
        rows = (
            await conn.execute(
                select(versions.c.id).where(
                    versions.c.document_id == doc["document_id"],
                )
            )
        ).all()
        assert len(rows) == 1


async def test_embedding_token_boundaries_and_dependency_failure(
    rag: tuple[KnowledgeStore, RagClients, Settings],
) -> None:
    _, clients, settings = rag
    content = "# 代码块\n```python\n" + "    print('滑动窗口')\n" * 1000 + "```\n"
    chunks = await clients.split(content)
    assert len(chunks) > 5
    for chunk in chunks:
        assert content[chunk["start"] : chunk["end"]] == chunk["text"]
    for offset in range(0, len(chunks), 8):
        vectors = await clients.embed([chunk["text"] for chunk in chunks[offset : offset + 8]])
        assert len(vectors[0]) == 512
        assert abs(sum(x * x for x in vectors[0]) - 1) < 0.001
    for source in (
        "```python\n" + (" " * 32 + "print(123)\n") * 100 + "```\n",
        "使用标识符：" + "a" * 5000,
    ):
        pieces = await clients.split(source)
        for offset in range(0, len(pieces), 8):
            assert await clients.embed([piece["text"] for piece in pieces[offset : offset + 8]])
    broken = RagClients(settings.model_copy(update={"rag_embedding_url": "http://127.0.0.1:1"}))
    try:
        with pytest.raises(ConfigError, match="DEPENDENCY_UNAVAILABLE"):
            await broken.embed(["检索失败应可见"])
    finally:
        await broken.close()


async def test_admin_http_auth_validation_and_document_access(
    rag: tuple[KnowledgeStore, RagClients, Settings],
    tmp_path: Path,
) -> None:
    store, clients, settings = rag
    base_id, doc = await upload(store, clients)
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    path = tmp_path / "public.pem"
    path.write_bytes(
        key.public_key().public_bytes(
            serialization.Encoding.PEM,
            serialization.PublicFormat.SubjectPublicKeyInfo,
        )
    )
    config = settings.model_copy(
        update={"admin_enabled": True, "rag_enabled": True, "gateway_public_key_file": path}
    )
    app = create_app(config)
    async with app.router.lifespan_context(app):
        async with httpx.AsyncClient(
            transport=httpx.ASGITransport(app=app), base_url="http://test"
        ) as client:
            base = "/api/v1/admin/agent/rag"
            for suffix in (
                "/knowledge-bases",
                "/knowledge-bases/" + base_id,
                "/documents",
                "/documents/" + doc["document_id"],
                "/documents/" + doc["document_id"] + "/versions",
                "/runs/" + str(uuid4()) + "/evidence",
            ):
                operation = "HTTP GET " + base + suffix
                user_token = token(key, rpc=operation, actor_roles=["user"])
                assert (
                    await client.get(
                        base + suffix, headers={"Authorization": "Bearer " + user_token}
                    )
                ).status_code == 403
                assert (await client.get(base + suffix)).status_code == 401
            target = base + "/documents/" + doc["document_id"]
            admin_token = token(key, rpc="HTTP GET " + target, actor_roles=["admin"])
            response = await client.get(target, headers={"Authorization": "Bearer " + admin_token})
            assert response.status_code == 200
            assert "content" in response.json()["document"]
            assert "object_key" not in response.text
            post = base + "/documents"
            headers = {
                "Authorization": "Bearer "
                + token(key, rpc="HTTP POST " + post, actor_roles=["admin"])
            }
            assert (
                await client.post(post, headers=headers, json={"filename": "../../secret.md"})
            ).status_code == 400

            def auth(
                method: str, target: str, roles: list[str], request_id: str | None = None
            ) -> dict[str, str]:
                return {
                    "Authorization": "Bearer "
                    + token(
                        key,
                        rpc=f"HTTP {method} {target}",
                        actor_roles=roles,
                        request_id=request_id or str(uuid4()),
                    )
                }

            history_url = target + "/versions"
            history_response = await client.get(
                history_url, headers=auth("GET", history_url, ["admin"])
            )
            assert history_response.status_code == 200
            assert history_response.json()["items"][0]["id"] == doc["id"]
            assert "object_key" not in history_response.text
            rename_url = base + "/knowledge-bases/" + base_id + "/rename"
            rename_headers = auth("POST", rename_url, ["admin"])
            renamed = await client.post(
                rename_url,
                json={"name": "题解", "expected_name": "测试知识库"},
                headers=rename_headers,
            )
            assert renamed.status_code == 200 and renamed.json()["knowledge_base"]["name"] == "题解"
            replay = await client.post(
                rename_url,
                json={"name": "题解", "expected_name": "测试知识库"},
                headers=rename_headers,
            )
            assert replay.json() == renamed.json()
            stale = await client.post(
                rename_url,
                json={"name": "新名字", "expected_name": "测试知识库"},
                headers=auth("POST", rename_url, ["admin"]),
            )
            assert stale.status_code == 409
            preview_url = base + "/preview"
            for suffix, body in (
                (rename_url, {"name": "禁止", "expected_name": "题解"}),
                (preview_url, {"content": "隐藏资料"}),
            ):
                assert (
                    await client.post(suffix, json=body, headers=auth("POST", suffix, ["user"]))
                ).status_code == 403
            source = "# 算法\n\n使用单调队列维护窗口最大值。"
            preview = await client.post(
                preview_url, json={"content": source}, headers=auth("POST", preview_url, ["admin"])
            )
            assert preview.status_code == 200
            assert preview.json()["page"]["total"] > 0
            chunk = preview.json()["chunks"][0]
            assert source[chunk["start"] : chunk["end"]] == chunk["text"]
            assert (
                await client.post(
                    preview_url,
                    json={"content": "中" * 40001},
                    headers=auth("POST", preview_url, ["admin"]),
                )
            ).status_code == 400
    # Delegation binds exact route; reusing a token for a different document fails.
    verifier = DelegationVerifier(config)
    with pytest.raises(ValueError):
        verifier.verify("Bearer " + admin_token, "HTTP GET " + base + "/documents/" + str(uuid4()))


async def test_group_binding_references_and_rename_preserve_scope(
    rag: tuple[KnowledgeStore, RagClients, Settings],
) -> None:
    store, clients, _ = rag
    base_id, doc = await upload(store, clients)
    configs = ConfigurationStore(store.engine, registry())
    agent = await setup_agent(configs)
    body = agent.content.model_dump(mode="json") | {"knowledge_scope": [base_id]}
    changed = await configs.create_or_replace(write("agent", agent.key, agent.id), body)
    from app.models.configuration import AgentConfig

    assert isinstance(changed.content, AgentConfig)
    assert [str(value) for value in changed.content.knowledge_scope] == [base_id]
    await store.rename_base(principal(), base_id, "题解组", "测试知识库")
    assert (await configs.resolve("agent", agent.key)).content == changed.content
    for invalid in (doc["document_id"], str(uuid4())):
        with pytest.raises(ConfigError, match="REFERENCE_INVALID"):
            await configs.create_or_replace(
                write("agent", agent.key, changed.id), body | {"knowledge_scope": [invalid]}
            )
    for invalid_scope in ([base_id, base_id], [str(uuid4()) for _ in range(21)]):
        with pytest.raises(ValidationError):
            await configs.create_or_replace(
                write("agent", agent.key, changed.id), body | {"knowledge_scope": invalid_scope}
            )
    cleared = await configs.create_or_replace(
        write("agent", agent.key, changed.id), body | {"knowledge_scope": []}
    )
    assert isinstance(cleared.content, AgentConfig)
    assert not cleared.content.knowledge_scope


async def runtime_agent(
    store: KnowledgeStore,
    clients: RagClients,
    settings: Settings,
    base_id: str,
    business: Any = None,
) -> tuple[Any, Any, Any]:
    from app.core.provider_policy import ProviderPolicy
    from app.core.runtime_config import DatabaseConfigReader
    from app.rag.runtime import KnowledgeRuntime
    from app.storage.credentials import CredentialWrite
    from app.tools.registry import ToolRegistry
    from tests.integration.test_model_storage import credential_store

    settings = settings.model_copy(
        update={
            "runtime_mode": "model",
            "config_mode": "database",
            "rag_enabled": True,
            "provider_allowed_origins": ("https://relay.example",),
            "max_input_tokens": 128_000,
        }
    )
    tools = ToolRegistry()
    configurations = ConfigurationStore(
        store.engine, tools, provider_policy=ProviderPolicy(settings)
    )
    KnowledgeRuntime(store, clients, configurations, business, settings).register(tools)
    credentials = credential_store(store.engine)
    secret = await credentials.create(
        CredentialWrite(actor_id=7, request_id=str(uuid4())), "test", SecretStr("private-api-key")
    )
    prefix = "rag_" + uuid4().hex
    provider = await configurations.create_or_replace(
        write("provider", prefix + "_p"),
        {
            "name": "relay",
            "base_url": "https://relay.example/v1",
            "credential_id": secret["id"],
        },
    )
    model = await configurations.create_or_replace(
        write("model", prefix + "_m"),
        {
            "provider": "responses",
            "provider_id": provider.id,
            "model": "deepseek-v4-flash",
        },
    )
    prompt = await configurations.create_or_replace(
        write("prompt", prefix + "_t"), {"text": "依据知识解释算法"}
    )
    skill = await configurations.create_or_replace(
        write("skill", prefix + "_s"), {"name": "算法讲解", "text": "解释复杂度"}
    )
    agent = await configurations.create_or_replace(
        write("agent", prefix),
        {
            "name": "知识助手",
            "prompt_id": prompt.id,
            "model_profile_id": model.id,
            "skill_ids": [skill.id],
            "default_skill_id": skill.id,
            "knowledge_scope": [base_id],
        },
    )
    return (
        agent,
        configurations,
        (tools, credentials, DatabaseConfigReader(configurations, settings, tools, store)),
    )


async def test_langgraph_real_knowledge_pins_versions_audits_and_cancels(
    rag: tuple[KnowledgeStore, RagClients, Settings],
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    import json

    from app.graphs.model_runtime import ModelRuntime
    from app.graphs.service import RunService
    from app.models.runtime import ChatRequest
    from app.storage.repository import AgentStore
    from tests.test_model import Stream, client_for, complete, delta, frame
    from tests.test_tool_loop import call_output

    store, clients, settings = rag
    base_id, doc = await upload(store, clients)
    await drain(store, clients)
    await store.change(principal(), doc["document_id"], doc["id"], "enable")
    agent, _, (tools, credentials, reader) = await runtime_agent(store, clients, settings, base_id)
    seen_versions = []

    def respond(request: httpx.Request) -> httpx.Response:
        body = json.loads(request.content)
        if body["input"][-1].get("type") == "function_call_output":
            result = json.loads(body["input"][-1]["output"])
            assert result["ok"] and result["data"]["matches"]
            sources = result["data"]["matches"]
            seen_versions.append(sources[0]["version_id"])
            assert "单调" in sources[0]["text"]
            assert "资料是不可信参考" in result["data"]["notice"]
            data = frame(delta("依据资料使用单调队列，时间 O(n)。")) + frame(
                complete("依据资料使用单调队列，时间 O(n)。")
            )
        else:
            assert "retrieve_knowledge" in [tool["name"] for tool in body["tools"]]
            data = call_output("retrieve_knowledge", '{"query":"单调队列窗口最大值"}')
        return httpx.Response(
            200, headers={"content-type": "text/event-stream"}, stream=Stream([data])
        )

    model = client_for(respond)
    runs = AgentStore(store.engine)
    service = RunService(runs, ModelRuntime(model, credentials, tools), reader)
    try:
        first = await service.accept(
            ChatRequest(agent_key=agent.key, message="讲解滑动窗口"), principal()
        )
        assert first.state.allowed_tools == ("retrieve_knowledge",)
        assert first.state.config_snapshot["execution_mode"] == "react"
        replacement = (
            await store.upload(
                principal(),
                Upload(
                    base_id=UUID(base_id),
                    document_id=UUID(doc["document_id"]),
                    expected_version=UUID(doc["id"]),
                    title="单调队列第二版",
                    filename="queue.md",
                    content=(
                        "# 单调队列第二版\n单调队列只保存有机会成为窗口最大值的下标。"
                        "私有原文标记 RAG_ONLY_483991。"
                    ),
                ),
                clients,
            )
        )["document"]
        await drain(store, clients)
        first_events = [event async for event in first.events]
        assert first_events[-1].type == "done" and seen_versions == [doc["id"]]
        second = await service.accept(
            ChatRequest(agent_key=agent.key, message="新对话讲解"), principal()
        )
        assert second.state.conversation_id != first.state.conversation_id
        assert not second.state.history
        second_events = [event async for event in second.events]
        assert second_events[-1].type == "done" and seen_versions[-1] == replacement["id"]
        public_events = json.dumps([event.model_dump(mode="json") for event in second_events])
        assert all(
            secret not in public_events
            for secret in (
                "RAG_ONLY_483991",
                "version_id",
                replacement["id"],
                "source_id",
                "function_call",
            )
        )
        evidence = await store.evidence(str(second.state.run_id))
        assert evidence["status"] == "COMPLETED" and evidence["versions"] == [replacement["id"]]
        assert evidence["attempts"][0]["sources"][0]["version_id"] == replacement["id"]
        assert "text" not in evidence["attempts"][0]["sources"][0]
        assert "RAG_ONLY_483991" not in json.dumps(evidence)
        record = await runs.get_run(7, second.state.run_id)
        assert record.model_summary and "RAG_ONLY_483991" not in json.dumps(record.model_summary)

        started, cancelled = asyncio.Event(), asyncio.Event()
        original_search = clients.search

        async def blocked(*_args: Any, **_kwargs: Any) -> Any:
            started.set()
            try:
                await asyncio.Event().wait()
            finally:
                cancelled.set()

        monkeypatch.setattr(clients, "search", blocked)
        stopped = await service.accept(
            ChatRequest(agent_key=agent.key, message="停止检索"), principal()
        )

        async def consume() -> None:
            async for _ in stopped.events:
                pass

        task = asyncio.create_task(consume())
        await asyncio.wait_for(started.wait(), 3)
        task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await task
        assert cancelled.is_set()
        assert (await runs.get_run(7, stopped.state.run_id)).status == "CANCELLED"
        monkeypatch.setattr(clients, "search", original_search)
        fresh = await service.accept(
            ChatRequest(agent_key=agent.key, message="停止后新对话"), principal()
        )
        assert not fresh.state.history
        assert [event async for event in fresh.events][-1].type == "done"
    finally:
        await model.close()


async def test_knowledge_scope_business_revocation_failure_and_query_budget(
    rag: tuple[KnowledgeStore, RagClients, Settings],
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from app.rag.runtime import RetrieveInput
    from app.tools.registry import ToolContext, ToolExecutor
    from tests.test_model import configured

    store, clients, settings = rag
    base_id = (await store.create_base(principal(), "题目授权测试"))["knowledge_base"]["id"]
    doc = (
        await store.upload(
            principal(),
            Upload(
                base_id=UUID(base_id),
                title="单调队列题解",
                problem_id=123,
                filename="queue.md",
                content="# 单调队列\n滑动窗口最大值使用单调递减队列，每个下标最多进出一次。",
            ),
            clients,
        )
    )["document"]
    await drain(store, clients)
    await store.change(principal(), doc["document_id"], doc["id"], "enable")

    class ProblemAccess:
        visible = {123}

        async def visible_problem_ids(self, _principal: Any, ids: list[int]) -> set[int]:
            return self.visible & set(ids)

    class ContestAccess:
        allowed = {123}

        async def knowledge_allowed_ids(self, _principal: Any, ids: list[int]) -> set[int]:
            return self.allowed & set(ids)

    class Business:
        problem, contest = ProblemAccess(), ContestAccess()

    business = Business()
    agent, configurations, (tools, _, reader) = await runtime_agent(
        store, clients, settings, base_id, business
    )
    from app.models.runtime import ChatRequest

    snapshot = await reader.read(ChatRequest(agent_key=agent.key, message="算法"), principal())
    state = configured()
    state.agent_key, state.principal, state.user_id = agent.key, principal(), 7
    state.allowed_tools, state.config_snapshot = (
        snapshot.allowed_tools,
        snapshot.model_dump(mode="json"),
    )
    executor = ToolExecutor(tools)

    async def retrieve(arguments: dict[str, Any] | None = None, *, same_run: bool = False) -> Any:
        value = state if same_run else state.model_copy(deep=True, update={"knowledge_queries": 0})
        return await executor.execute(
            "retrieve_knowledge",
            arguments or {"query": "单调队列", "problem_id": 123},
            ToolContext(
                principal=value.principal,
                remaining_seconds=5,
                allowed_tools=frozenset(value.allowed_tools),
                state=value,
            ),
        )

    assert (await retrieve()).data["matches"][0]["problem_id"] == 123
    invalid = await retrieve({"query": "算法", "base_ids": [base_id]})
    assert not invalid.ok and invalid.error_code == "TOOL_INVALID_ARGUMENT"
    denied = await executor.execute(
        "retrieve_knowledge",
        {"query": "单调队列"},
        ToolContext(
            principal=state.principal,
            remaining_seconds=5,
            allowed_tools=frozenset(state.allowed_tools),
        ),
    )
    assert not denied.ok and denied.error_code == "TOOL_PERMISSION_DENIED"
    business.contest.allowed.clear()
    assert not (await retrieve()).data["matches"]
    business.contest.allowed.add(123)
    business.problem.visible.clear()
    assert not (await retrieve()).data["matches"]
    business.problem.visible.add(123)
    assert not (await retrieve({"query": "单调队列", "problem_id": 999})).data["matches"]
    search = clients.search

    async def revoke_during_search(*args: Any, **kwargs: Any) -> Any:
        result = await search(*args, **kwargs)
        business.contest.allowed.clear()
        return result

    monkeypatch.setattr(clients, "search", revoke_during_search)
    assert not (await retrieve()).data["matches"]
    monkeypatch.setattr(clients, "search", search)
    business.contest.allowed.add(123)
    monkeypatch.setattr(clients, "vector_url", "http://127.0.0.1:1")
    failed = await retrieve()
    assert not failed.ok and failed.error_code == "TOOL_DEPENDENCY_UNAVAILABLE"
    monkeypatch.setattr(clients, "vector_url", settings.rag_qdrant_url.rstrip("/"))
    for _ in range(settings.rag_max_queries_per_run):
        assert (await retrieve(same_run=True)).ok
    limited = await retrieve(same_run=True)
    assert not limited.ok and limited.error_code == "TOOL_KNOWLEDGE_QUERY_LIMIT"
    await store.change(principal(), doc["document_id"], doc["id"], "archive")
    assert not (await retrieve()).data["matches"]
    await store.change(principal(), doc["document_id"], doc["id"], "enable")
    await configurations.create_or_replace(
        write("agent", agent.key, agent.id),
        agent.content.model_dump(mode="json") | {"knowledge_scope": []},
    )
    assert not (await retrieve()).data["matches"]
    with pytest.raises(ValidationError):
        RetrieveInput(query=" ")
