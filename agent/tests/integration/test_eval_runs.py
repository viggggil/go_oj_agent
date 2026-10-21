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

pytestmark = [
    pytest.mark.integration,
    pytest.mark.skipif(
        os.environ.get("AGENT_TEST_MODEL_MODE") != "true",
        reason="Eval runs require the isolated model-review phase",
    ),
]
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


async def test_model_review_strict_json_unavailable_and_retry(http: httpx.AsyncClient) -> None:
    if os.environ.get("AGENT_TEST_MODEL_MODE") != "true":
        pytest.skip("Requires the Responses model phase")
    dataset_id = await upload(http, [sample("review_retry", "review-invalid-once")])
    response = await http.post(
        BASE + "/runs",
        json={
            "dataset_id": dataset_id,
            "agent_key": "learning_assistant",
        },
        headers={"X-Request-ID": str(uuid4())},
    )
    assert response.status_code == 200, response.text
    run = await finished(http, response.json()["run"]["id"])
    assert run["status"] == "COMPLETED"
    engine = create_async_engine(os.environ["AGENT_TEST_DATABASE_URL"])
    try:
        async with engine.connect() as connection:
            reviewer = await connection.scalar(
                select(agent_runs.c.config_snapshot)
                .where(agent_runs.c.config_snapshot["agent_key"].as_string() == "eval_reviewer")
                .order_by(agent_runs.c.started_at.desc())
                .limit(1)
            )
        # 验证部署天花板没有把 Reviewer 的独立事件预算再次夹回 1,000。
        assert reviewer is not None
        assert reviewer["budget"]["max_events"] == 10000
    finally:
        await engine.dispose()
    result = (await http.get(BASE + f"/runs/{run['id']}/results")).json()["items"][0]
    detail = (await http.get(BASE + f"/runs/{run['id']}/results/{result['id']}")).json()["result"]
    assert detail["model_review_status"] == "UNAVAILABLE"
    assert detail["effective_pass"] is False
    retry_request_id = str(uuid4())
    retry = await http.post(
        BASE + f"/results/{result['id']}/review-agent",
        json={"expected_attempt": 1},
        headers={"X-Request-ID": retry_request_id},
    )
    assert retry.status_code == 200, retry.text
    async with asyncio.timeout(10):
        while True:
            detail = await http.get(BASE + f"/runs/{run['id']}/results/{result['id']}")
            value = detail.json()["result"]
            if value["model_review_status"] not in {"RUNNING", "PENDING"}:
                break
            await asyncio.sleep(0.05)
    assert value["model_review_status"] == "COMPLETED", value
    assert value["model_review"]["case_id"] == "review_retry"
    assert value["effective_pass"] is True
    attempts = await http.get(BASE + f"/results/{result['id']}/model-reviews")
    assert attempts.status_code == 200
    assert [item["attempt"] for item in attempts.json()["items"]] == [2, 1]
    replay = await http.post(
        BASE + f"/results/{result['id']}/review-agent",
        json={"expected_attempt": 1},
        headers={"X-Request-ID": retry_request_id},
    )
    assert replay.status_code == 200 and replay.json()["duplicate"]


async def test_model_reviews_reject_bad_schema_skip_hard_failure_and_enforce_permissions(
    http: httpx.AsyncClient,
) -> None:
    if os.environ.get("AGENT_TEST_MODEL_MODE") != "true":
        pytest.skip("Requires the Responses model phase")
    suffix = uuid4().hex
    dataset_id = await upload(
        http,
        [
            sample("review_good_" + suffix, "review-success"),
            sample("review_wrong_" + suffix, "review-wrong-id"),
            sample("review_extra_" + suffix, "review-extra-key"),
            sample("review_error_" + suffix, "review-error-once"),
            sample("review_hard_" + suffix, fixture={"principal": "missing"}),
        ],
    )
    body = {
        "dataset_id": dataset_id,
        "agent_key": "learning_assistant",
        "concurrency": 2,
    }
    response = await http.post(BASE + "/runs", json=body, headers={"X-Request-ID": str(uuid4())})
    assert response.status_code == 200, response.text
    run = await finished(http, response.json()["run"]["id"])
    rows = (await http.get(BASE + f"/runs/{run['id']}/results")).json()["items"]
    by_case = {row["case_id"].removesuffix("_" + suffix): row for row in rows}
    assert by_case["review_good"]["model_review_status"] == "COMPLETED"
    assert by_case["review_good"]["effective_pass"]
    for name in ("review_wrong", "review_extra", "review_error"):
        assert by_case[name]["model_review_status"] == "UNAVAILABLE", by_case[name]
        assert not by_case[name]["effective_pass"]
    hard = by_case["review_hard"]
    assert hard["model_review_status"] == "SKIPPED" and hard["model_review_attempt"] == 0
    assert (await http.get(BASE + f"/results/{hard['id']}/model-reviews")).json()["items"] == []
    rejected = await http.post(
        BASE + f"/results/{hard['id']}/review-agent",
        json={"expected_attempt": 0},
        headers={"X-Request-ID": str(uuid4())},
    )
    assert (
        rejected.status_code == 409 and rejected.json()["reason"] == "AGENT_EVAL_REVIEW_NOT_READY"
    )
    result_id = by_case["review_error"]["id"]
    attempts = await asyncio.gather(
        *[
            http.post(
                BASE + f"/results/{result_id}/review-agent",
                json={"expected_attempt": 1},
                headers={"X-Request-ID": str(uuid4())},
            )
            for _ in range(2)
        ]
    )
    assert sorted(item.status_code for item in attempts) == [200, 409], [r.text for r in attempts]
    user_headers = {"Authorization": "Bearer " + user_token(88501)}
    assert (
        await http.get(BASE + f"/results/{result_id}/model-reviews", headers=user_headers)
    ).status_code == 403
    assert (
        await http.post(
            BASE + f"/results/{result_id}/review-agent",
            json={"expected_attempt": 1},
            headers={**user_headers, "X-Request-ID": str(uuid4())},
        )
    ).status_code == 403
    directory = await http.get("/api/v1/agent/agents", headers=user_headers)
    assert directory.status_code == 200
    assert "eval_reviewer" not in {item["key"] for item in directory.json()["items"]}


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


async def test_manual_reviews_are_atomic_audited_idempotent_and_cannot_override_hard_failures(
    http: httpx.AsyncClient,
) -> None:
    dataset_id = await upload(
        http,
        [
            sample("good"),
            sample("soft", expectations={"required_facts": ["不可能出现的事实_" + uuid4().hex]}),
            sample("hard", fixture={"principal": "missing"}),
        ],
    )
    response = await http.post(
        BASE + "/runs",
        json={"dataset_id": dataset_id, "agent_key": "learning_assistant"},
        headers={"X-Request-ID": str(uuid4())},
    )
    assert response.status_code == 200, response.text
    run_id = response.json()["run"]["id"]
    await finished(http, run_id)
    rows = (await http.get(BASE + f"/runs/{run_id}/results")).json()["items"]
    by_case = {row["case_id"]: row for row in rows}
    soft_id, hard_id = by_case["soft"]["id"], by_case["hard"]["id"]
    scores = {
        key: 4
        for key in (
            "task_completion",
            "technical_correctness",
            "evidence_support",
            "instruction_following",
            "clarity",
        )
    }
    body = {
        "expected_version": 0,
        "scores": scores,
        "pass": True,
        "tags": ["人工核对"],
        "note": "语义等价，字面检查未命中",
        "reason": "依据完整回答复核",
    }
    path = BASE + f"/results/{soft_id}/review"
    request_id = str(uuid4())
    first = await http.post(path, json=body, headers={"X-Request-ID": request_id})
    assert first.status_code == 200, first.text
    assert first.json()["review_version"] == 1 and not first.json()["duplicate"]
    assert first.json()["review"]["reviewed_by"] == 88500
    competing = await asyncio.gather(
        *[
            http.post(
                path,
                json={**body, "expected_version": 1, "note": note},
                headers={"X-Request-ID": str(uuid4())},
            )
            for note in ("并发管理员 A", "并发管理员 B")
        ]
    )
    assert sorted(item.status_code for item in competing) == [200, 409], [
        item.text for item in competing
    ]
    replay = await http.post(path, json=body, headers={"X-Request-ID": request_id})
    assert replay.status_code == 200 and replay.json()["duplicate"]
    assert replay.json()["review_version"] == 1  # 重放旧请求不覆盖版本 2。
    detail = await http.get(BASE + f"/runs/{run_id}/results/{soft_id}")
    assert detail.status_code == 200, detail.text
    value = detail.json()["result"]
    assert value["review_version"] == 2 and value["effective_pass"]
    assert value["manual_review"]["note"].startswith("并发管理员")
    assert not value["checks"]["pass"] and not value["checks"]["hard_fail"]
    assert value["case"]["id"] == "soft" and value["case"]["expectations"]["required_facts"]
    conflict = await http.post(
        path, json={**body, "note": "不同内容"}, headers={"X-Request-ID": request_id}
    )
    assert (
        conflict.status_code == 409 and conflict.json()["reason"] == "AGENT_EVAL_REQUEST_CONFLICT"
    )
    other_actor = await http.post(
        path,
        json=body,
        headers={
            "X-Request-ID": request_id,
            "Authorization": "Bearer " + user_token(88502, ["admin"]),
        },
    )
    assert other_actor.status_code == 409
    other_result = await http.post(
        BASE + f"/results/{by_case['good']['id']}/review",
        json=body,
        headers={"X-Request-ID": request_id},
    )
    assert other_result.status_code == 409
    for patch in (
        {"scores": {**scores, "clarity": 5}},
        {"scores": {**scores, "clarity": True}},
        {"reason": " "},
        {"tags": ["x" * 33]},
    ):
        invalid = await http.post(
            path, json={**body, **patch}, headers={"X-Request-ID": str(uuid4())}
        )
        assert invalid.status_code == 400, invalid.text
    hard_path = BASE + f"/results/{hard_id}/review"
    rejected = await http.post(hard_path, json=body, headers={"X-Request-ID": str(uuid4())})
    assert rejected.status_code == 409 and rejected.json()["reason"] == "AGENT_EVAL_HARD_FAILURE"
    saved = await http.post(
        hard_path, json={**body, "pass": False}, headers={"X-Request-ID": str(uuid4())}
    )
    assert saved.status_code == 200, saved.text
    hard = (await http.get(BASE + f"/runs/{run_id}/results/{hard_id}")).json()["result"]
    assert hard["checks"]["hard_fail"] and not hard["effective_pass"]
    audit = await http.get(BASE + f"/results/{soft_id}/reviews?page_size=1")
    assert audit.status_code == 200, audit.text
    assert audit.json()["page"]["total"] == 2
    latest = audit.json()["items"][0]
    assert latest["version"] == 2 and latest["old_value"]["note"] == body["note"]
    assert "fingerprint" not in latest and "request_id" not in latest
    older = await http.get(BASE + f"/results/{soft_id}/reviews?page_size=1&page=2")
    assert (
        older.json()["items"][0]["version"] == 1 and older.json()["items"][0]["old_value"] is None
    )
    metrics = (await http.get(BASE + f"/runs/{run_id}")).json()["metrics"]
    assert metrics["recorded_cases"] == 3 and metrics["check_passes"] == 1
    assert metrics["effective_passes"] == 2 and metrics["reviewed_cases"] == 2
    assert metrics["average_latency_ms"] >= 0
    for query, total in (
        ("verdict=pass&category=basic", 2),
        ("verdict=hard_fail", 1),
        ("verdict=unreviewed", 1),
        ("verdict=fail", 1),
        ("error_code=AGENT_EVAL_PRINCIPAL_INVALID", 1),
        ("search=soft", 1),
        ("category=unknown", 0),
    ):
        filtered = await http.get(BASE + f"/runs/{run_id}/results?{query}")
        assert filtered.status_code == 200 and filtered.json()["page"]["total"] == total, (
            filtered.text
        )
    public_headers = {"Authorization": "Bearer " + user_token(88501)}
    assert (
        await http.get(BASE + f"/results/{soft_id}/reviews", headers=public_headers)
    ).status_code == 403
    assert (
        await http.post(
            path,
            json={**body, "expected_version": 2},
            headers={**public_headers, "X-Request-ID": str(uuid4())},
        )
    ).status_code == 403
    assert (await http.get(BASE + f"/results/{uuid4()}/reviews")).status_code == 404
