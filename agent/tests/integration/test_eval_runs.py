"""Gateway → Eval → 普通 Agent Runtime → 真实 MySQL/MinIO 的批量评测闭环。"""

import asyncio
import json
import os
from collections.abc import AsyncIterator
from typing import Any
from uuid import uuid4

import httpx
import pytest
from sqlalchemy import select
from sqlalchemy.ext.asyncio import create_async_engine

from app.storage.schema import agent_messages, agent_runs
from tests.integration.test_chat_http import user_token

pytestmark = pytest.mark.integration
BASE = "/api/v1/admin/agent/evals"


@pytest.fixture
async def http() -> AsyncIterator[httpx.AsyncClient]:
    url = os.environ.get("AGENT_TEST_GATEWAY_URL")
    if not url:
        pytest.skip("Run make agent-test-integration")

    async def authenticate(request: httpx.Request) -> None:
        # 长评测每次请求刷新测试 token；显式普通用户请求保持原身份。
        if "authorization" not in request.headers:
            request.headers["Authorization"] = "Bearer " + user_token(88500, ["admin"])

    async with httpx.AsyncClient(
        base_url=url,
        timeout=15,
        event_hooks={"request": [authenticate]},
    ) as client:
        yield client


def sample(identifier: str, message: str = "介绍算法", **changes: Any) -> dict[str, Any]:
    return {
        "id": identifier,
        "category": "basic",
        "title": "Eval 集成测试",
        "turns": [{"role": "user", "content": message}],
        "fixture": {"principal": "student_a"},
        **changes,
    }


async def upload(http: httpx.AsyncClient, cases: list[dict[str, Any]]) -> str:
    response = await http.post(
        BASE + "/datasets",
        headers={"X-Request-ID": str(uuid4())},
        json={
            "name": "集成测试 " + uuid4().hex,
            "content": "\n".join(json.dumps(case, ensure_ascii=False) for case in cases),
        },
    )
    assert response.status_code == 200, response.text
    return str(response.json()["dataset"]["id"])


async def finished(http: httpx.AsyncClient, run_id: str) -> dict[str, Any]:
    async with asyncio.timeout(15):
        while True:
            response = await http.get(BASE + "/runs/" + run_id)
            assert response.status_code == 200, response.text
            run: dict[str, Any] = response.json()["run"]
            if run["status"] not in {"QUEUED", "RUNNING"}:
                return run
            await asyncio.sleep(0.05)


async def test_gateway_eval_multiturn_isolation_idempotency_and_permissions(
    http: httpx.AsyncClient,
) -> None:
    identifier = "eval " + uuid4().hex
    dataset_id = await upload(
        http,
        [
            sample(
                "good",
                identifier,
                turns=[
                    {"role": "user", "content": identifier},
                    {"role": "assistant", "content": "参考答案不注入 Runtime"},
                    {"role": "user", "content": identifier + " followup"},
                ],
            ),
            sample("bad_principal", fixture={"principal": "unknown"}),
            sample("aaa_bad_fixture", fixture={"principal": "student_a", "problem_id": 0}),
        ],
    )
    body = {"dataset_id": dataset_id, "agent_key": "learning_assistant", "concurrency": 2}
    request_id = str(uuid4())
    response = await http.post(BASE + "/runs", json=body, headers={"X-Request-ID": request_id})
    assert response.status_code == 200, response.text
    run_id = response.json()["run"]["id"]
    run = await finished(http, run_id)
    assert run["status"] == "COMPLETED" and run["completed_cases"] == 3, run
    assert run["hard_failures"] == run["error_count"] == 2
    assert "configurations" not in run["config_snapshot"]
    assert "fingerprint" not in run and "cancel_request_id" not in run
    for _ in range(2):
        replay = await http.post(BASE + "/runs", json=body, headers={"X-Request-ID": request_id})
        assert replay.status_code == 200 and replay.json()["duplicate"]
        assert replay.json()["run"]["id"] == run_id
    conflict = await http.post(
        BASE + "/runs", json={**body, "repeat_count": 2}, headers={"X-Request-ID": request_id}
    )
    assert (
        conflict.status_code == 409 and conflict.json()["reason"] == "AGENT_EVAL_REQUEST_CONFLICT"
    )
    results = await http.get(BASE + f"/runs/{run_id}/results")
    assert results.status_code == 200, results.text
    rows = {item["case_id"]: item for item in results.json()["items"]}
    assert rows["good"]["checks"]["pass"] and rows["good"]["answer_preview"]
    detail = await http.get(BASE + f"/runs/{run_id}/results/{rows['good']['id']}")
    assert detail.status_code == 200 and detail.json()["result"]["answer"]
    assert "model_summary" not in rows["good"]
    assert rows["bad_principal"]["error_code"] == "AGENT_EVAL_PRINCIPAL_INVALID"
    filtered = await http.get(BASE + f"/runs/{run_id}/results?status=FAILED&page_size=1")
    assert filtered.json()["page"]["total"] == 2
    assert filtered.json()["items"][0]["case_id"] == "aaa_bad_fixture"
    assert (await http.get(BASE + f"/runs/{uuid4()}/results")).status_code == 404
    for path in ("/datasets", "/runs", f"/runs/{run_id}", f"/runs/{run_id}/results"):
        denied = await http.get(
            BASE + path, headers={"Authorization": "Bearer " + user_token(88501)}
        )
        assert denied.status_code == 403
    denied = await http.post(
        BASE + "/runs",
        json=body,
        headers={
            "Authorization": "Bearer " + user_token(88501),
            "X-Request-ID": str(uuid4()),
        },
    )
    assert denied.status_code == 403
    engine = create_async_engine(os.environ["AGENT_TEST_DATABASE_URL"])
    try:
        async with engine.connect() as conn:
            records = (
                (
                    await conn.execute(
                        select(agent_runs)
                        .join(agent_messages, agent_messages.c.run_id == agent_runs.c.id)
                        .where(agent_messages.c.content.in_([identifier, identifier + " followup"]))
                    )
                )
                .mappings()
                .all()
            )
        assert len(records) == 2  # request ID 必须每轮不同，不能误命中幂等。
        assert {row["user_id"] for row in records} == {88501}
        assert len({row["conversation_id"] for row in records}) == 1
        assert len({row["request_id"] for row in records}) == 2
        conversation_id = records[0]["conversation_id"]
        user_headers = {"Authorization": "Bearer " + user_token(88501)}
        history = await http.get("/api/v1/agent/conversations", headers=user_headers)
        assert conversation_id not in {item["id"] for item in history.json()["items"]}
        detail = await http.get(
            f"/api/v1/agent/conversations/{conversation_id}/messages", headers=user_headers
        )
        assert detail.status_code == 404
        continuation = await http.post(
            "/api/v1/agent/chat",
            json={
                "conversation_id": conversation_id,
                "message": "读取评测历史",
            },
            headers=user_headers,
        )
        assert continuation.status_code == 404

    finally:
        await engine.dispose()


async def test_model_eval_cancel_closes_provider_and_preserves_terminal_state(
    http: httpx.AsyncClient,
) -> None:
    if os.environ.get("AGENT_TEST_MODEL_MODE") != "true":
        pytest.skip("Requires the Responses model phase")
    dataset_id = await upload(http, [sample("cancel", "wait-for-cancel")])
    response = await http.post(
        BASE + "/runs",
        json={
            "dataset_id": dataset_id,
            "agent_key": "learning_assistant",
        },
        headers={"X-Request-ID": str(uuid4())},
    )
    assert response.status_code == 200, response.text
    run_id = response.json()["run"]["id"]
    engine = create_async_engine(os.environ["AGENT_TEST_DATABASE_URL"])
    try:
        async with asyncio.timeout(10):
            while True:
                async with engine.connect() as conn:
                    active = (
                        (
                            await conn.execute(
                                select(agent_runs).where(
                                    agent_runs.c.user_id == 88501, agent_runs.c.status == "RUNNING"
                                )
                            )
                        )
                        .mappings()
                        .first()
                    )
                if active is not None:
                    break
                await asyncio.sleep(0.05)
        run = (await http.get(BASE + "/runs/" + run_id)).json()["run"]
        path = BASE + f"/runs/{run_id}/cancel?version={run['version']}"
        request_id = str(uuid4())
        cancellation = await http.post(path, json={}, headers={"X-Request-ID": request_id})
        assert cancellation.status_code == 200, cancellation.text
        assert (
            await http.post(path, json={}, headers={"X-Request-ID": request_id})
        ).status_code == 200
        done = await finished(http, run_id)
        assert done["status"] == "CANCELLED" and done["completed_cases"] == 1
        rows = (await http.get(BASE + f"/runs/{run_id}/results")).json()["items"]
        assert rows[0]["status"] == "CANCELLED" and rows[0]["checks"]["hard_fail"]
        async with engine.connect() as conn:
            terminal = (
                await conn.execute(
                    select(agent_runs.c.status).where(agent_runs.c.id == active["id"])
                )
            ).scalar_one()
        assert terminal == "CANCELLED"
        conflict = await http.post(path, json={}, headers={"X-Request-ID": str(uuid4())})
        assert conflict.status_code == 409
    finally:
        await engine.dispose()


async def test_frozen_config_renders_each_case_and_preserves_user_visibility() -> None:
    from pydantic import SecretStr
    from sqlalchemy.ext.asyncio import create_async_engine

    from app.core.runtime_config import DatabaseConfigReader
    from app.core.settings import Settings
    from app.models.configuration import ConfigError
    from app.models.runtime import ChatContext, ChatRequest, Principal
    from app.storage.configuration import ConfigurationStore
    from tests.integration.test_configuration import registry, setup_agent, write

    url = os.environ.get("AGENT_TEST_DATABASE_URL")
    if not url:
        pytest.skip("Run make agent-test-integration")
    engine = create_async_engine(url)
    try:
        tools = registry()
        configs = ConfigurationStore(engine, tools)
        agent = await setup_agent(configs)
        old_prompt = await configs.get_version(str(agent.content.model_dump()["prompt_id"]))
        prompt = await configs.create_or_replace(
            write("prompt", old_prompt.key, old_prompt.id),
            {
                "text": "题号 {problem_id}",
                "variables": ["problem_id"],
            },
        )
        content = agent.content.model_dump(mode="json")
        content["prompt_id"] = prompt.id
        agent = await configs.create_or_replace(write("agent", agent.key, agent.id), content)
        reader = DatabaseConfigReader(
            configs, Settings(runtime_mode="fake", database_url=SecretStr(url)), tools
        )
        actor = Principal(user_id=88500, roles=frozenset({"admin"}), request_id=str(uuid4()))
        frozen = await reader.read(
            ChatRequest(agent_key=agent.key, message="Eval"), actor, render_prompts=False
        )
        replacement = await configs.create_or_replace(
            write("prompt", prompt.key, prompt.id),
            {
                "text": "替换后的提示",
            },
        )
        content["prompt_id"] = replacement.id
        await configs.create_or_replace(write("agent", agent.key, agent.id), content)
        for problem_id in (22, 33):
            request = ChatRequest(
                agent_key=agent.key, message="解释", context=ChatContext(problem_id=problem_id)
            )
            pinned = await reader.read(request, actor, pinned=frozen)
            assert pinned.version == frozen.version
            assert pinned.prompt_text == f"题号 {problem_id}"
            assert (await reader.read(request, actor)).prompt_text == "替换后的提示"
        private = await setup_agent(configs, admin=True)
        snapshot = await reader.read(ChatRequest(agent_key=private.key, message="Eval"), actor)
        with pytest.raises(ConfigError, match="NOT_FOUND"):
            await reader.read(
                ChatRequest(agent_key=private.key, message="解释"),
                actor.model_copy(update={"roles": frozenset({"user"})}),
                pinned=snapshot,
            )
    finally:
        await engine.dispose()


async def test_concurrent_case_results_and_progress_commit_without_deadlock(
    http: httpx.AsyncClient,
) -> None:
    dataset_id = await upload(
        http,
        [sample(f"concurrent_{index}", fixture={"principal": "missing"}) for index in range(8)],
    )
    response = await http.post(
        BASE + "/runs",
        json={
            "dataset_id": dataset_id,
            "agent_key": "learning_assistant",
            "concurrency": 8,
            "repeat_count": 3,
        },
        headers={"X-Request-ID": str(uuid4())},
    )
    assert response.status_code == 200, response.text
    run_id = response.json()["run"]["id"]
    run = await finished(http, run_id)
    assert run["status"] == "COMPLETED", run
    assert run["total_cases"] == run["completed_cases"] == run["hard_failures"] == 24
    assert run["error_count"] == 24
    page = await http.get(BASE + f"/runs/{run_id}/results?page_size=100")
    assert page.status_code == 200, page.text
    assert page.json()["page"]["total"] == len(page.json()["items"]) == 24
    assert len({(item["case_id"], item["attempt"]) for item in page.json()["items"]}) == 24
