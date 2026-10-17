"""真实 Gateway → Agent → 本地 Responses mock → MySQL。"""

import json
import os

import httpx
import pytest
from sqlalchemy import text
from sqlalchemy.ext.asyncio import create_async_engine

from tests.integration.test_chat_http import user_token

pytestmark = pytest.mark.integration


def urls() -> tuple[str, str]:
    if os.environ.get("AGENT_TEST_MODEL_MODE") != "true":
        pytest.skip("model phase only")
    return os.environ["AGENT_TEST_GATEWAY_URL"], os.environ["AGENT_TEST_MOCK_URL"]


async def test_gateway_receives_first_token_before_provider_completion_and_persists_summary() -> (
    None
):
    gateway, mock = urls()
    headers = {"Authorization": "Bearer " + user_token(7201)}
    async with httpx.AsyncClient(timeout=10) as http:
        received = []
        async with http.stream(
            "POST",
            gateway + "/api/v1/agent/chat",
            json={"message": "wait-for-release"},
            headers=headers,
        ) as response:
            assert response.status_code == 200
            run_id = response.headers["x-agent-run-id"]
            async for line in response.aiter_lines():
                if not line.startswith("data: "):
                    continue
                event = json.loads(line[6:])
                received.append(event)
                if event["type"] == "token":
                    assert (await http.get(mock + "/completed")).json()["completed"] is False
                    assert (await http.post(mock + "/release")).status_code == 200
        assert [event["type"] for event in received] == [
            "progress",
            "progress",
            "progress",
            "token",
            "done",
        ]
        assert received[3]["data"]["text"] == "中文算法回答"
    engine = create_async_engine(os.environ["AGENT_TEST_DATABASE_URL"])
    try:
        async with engine.connect() as connection:
            row = (
                await connection.execute(
                    text(
                        "SELECT status,model_summary,config_snapshot FROM agent_runs WHERE id=:id"
                    ),
                    {"id": run_id},
                )
            ).one()
            assert row.status == "COMPLETED"
            summary = json.loads(row.model_summary)
            assert summary["attempts"][0]["usage_source"] == "provider"
            assert summary["attempts"][0]["output_tokens"] == 8
            assert "integration-model-key" not in row.config_snapshot + row.model_summary
    finally:
        await engine.dispose()


async def test_gateway_partial_failure_has_no_done_or_successful_answer() -> None:
    gateway, _ = urls()
    async with httpx.AsyncClient(timeout=10) as http:
        response = await http.post(
            gateway + "/api/v1/agent/chat",
            json={"message": "fail-after-token"},
            headers={"Authorization": "Bearer " + user_token(7202)},
        )
        assert response.status_code == 200
        events = [
            json.loads(line[6:]) for line in response.text.splitlines() if line.startswith("data: ")
        ]
        assert [event["type"] for event in events] == [
            "progress",
            "progress",
            "progress",
            "token",
            "error",
        ]
        assert "private-api-key" not in response.text
    engine = create_async_engine(os.environ["AGENT_TEST_DATABASE_URL"])
    try:
        async with engine.connect() as connection:
            row = (
                await connection.execute(
                    text("SELECT status FROM agent_runs WHERE id=:id"),
                    {"id": response.headers["x-agent-run-id"]},
                )
            ).one()
            assert row.status == "FAILED"
            count = (
                await connection.execute(
                    text(
                        "SELECT COUNT(*) FROM agent_messages WHERE run_id=:id AND role='assistant'"
                    ),
                    {"id": response.headers["x-agent-run-id"]},
                )
            ).scalar_one()
            assert count == 0
    finally:
        await engine.dispose()


async def test_gateway_followup_uses_completed_history_and_new_conversation_is_empty() -> None:
    gateway, _ = urls()
    headers = {"Authorization": "Bearer " + user_token(7204)}
    async with httpx.AsyncClient(timeout=10) as http:

        async def chat(message: str, conversation: str | None = None) -> httpx.Response:
            body = {"message": message}
            if conversation is not None:
                body["conversation_id"] = conversation
            response = await http.post(gateway + "/api/v1/agent/chat", json=body, headers=headers)
            assert response.status_code == 200
            events = [
                json.loads(line[6:])
                for line in response.text.splitlines()
                if line.startswith("data: ")
            ]
            assert events[-1]["type"] == ("error" if message == "fail-after-token" else "done")
            return response

        first = await chat("PR2 remember 314159")
        conversation = first.headers["x-agent-conversation-id"]
        await chat("fail-after-token", conversation)
        # mock 精确检查两条已完成历史及本轮输入，失败轮次不得混入。
        await chat("PR2 context followup", conversation)
        other_user = await http.post(
            gateway + "/api/v1/agent/chat",
            json={"message": "PR2 context followup", "conversation_id": conversation},
            headers={"Authorization": "Bearer " + user_token(7205)},
        )
        assert other_user.status_code == 404
        fresh = await chat("PR2 new conversation")
        assert fresh.headers["x-agent-conversation-id"] != conversation


async def test_gateway_disconnect_cancels_graph_and_provider_without_saving_partial_answer() -> (
    None
):
    import asyncio
    from time import monotonic

    gateway, mock = urls()
    engine = create_async_engine(os.environ["AGENT_TEST_DATABASE_URL"])
    try:
        async with httpx.AsyncClient(timeout=10) as http:
            async with http.stream(
                "POST",
                gateway + "/api/v1/agent/chat",
                json={"message": "wait-for-cancel"},
                headers={"Authorization": "Bearer " + user_token(7203)},
            ) as response:
                assert response.status_code == 200
                run_id = response.headers["x-agent-run-id"]
                async for line in response.aiter_lines():
                    if line.startswith("data: ") and json.loads(line[6:])["type"] == "token":
                        break
                else:
                    pytest.fail("未收到首段回答")
            # 模拟浏览器 Abort；等待 Gateway → 图 → Provider 关闭与数据库终态收敛。
            deadline = monotonic() + 5
            while True:
                async with engine.connect() as connection:
                    status = await connection.scalar(
                        text("SELECT status FROM agent_runs WHERE id=:id"), {"id": run_id}
                    )
                cancelled = (await http.get(mock + "/cancelled")).json()["cancelled"]
                if status == "CANCELLED" and cancelled:
                    break
                assert monotonic() < deadline, (status, cancelled)
                await asyncio.sleep(0.05)
        async with engine.connect() as connection:
            count = await connection.scalar(
                text("SELECT COUNT(*) FROM agent_messages WHERE run_id=:id AND role='assistant'"),
                {"id": run_id},
            )
        assert count == 0
    finally:
        await engine.dispose()


async def test_configured_react_skill_reads_signed_grpc_evidence_and_maps_denial() -> None:
    from uuid import uuid4

    gateway, _ = urls()
    headers = {"Authorization": "Bearer " + user_token(7206, ["admin"])}
    base = gateway + "/api/v1/admin/agent"
    async with httpx.AsyncClient(timeout=10) as http:

        async def create(kind: str, content: dict[str, object]) -> dict[str, object]:
            response = await http.post(
                base + "/" + kind,
                headers={**headers, "X-Request-ID": str(uuid4())},
                json={"content": content},
            )
            assert response.status_code == 200, response.text
            return response.json()["configuration"]  # type: ignore[no-any-return]

        prompt = await create(
            "prompts", {"name": "PR3 系统说明", "text": "按读取证据回答", "variables": []}
        )
        skill = await create(
            "skills",
            {
                "name": "PR3 读取题目",
                "text": "先读取题目再回答",
                "allowed_tools": ["get_problem"],
            },
        )
        models = await http.get(base + "/model-options", headers=headers)
        agent = await create(
            "agents",
            {
                "name": "PR3 工具助手",
                "prompt_id": prompt["id"],
                "skill_ids": [skill["id"]],
                "default_skill_id": skill["id"],
                "model_profile_id": models.json()["items"][0]["id"],
                "allowed_tools": ["get_problem"],
                "visibility": "user",
            },
        )
        directory = await http.get(
            gateway + "/api/v1/agent/agents/" + str(agent["key"]) + "/skills", headers=headers
        )
        assert directory.status_code == 200 and "execution_mode" not in directory.json()["items"][0]
        for message, expected in [
            ("PR3 read problem", "证据数字 739216"),
            ("PR3 denied problem", "无权读取题目"),
        ]:
            response = await http.post(
                gateway + "/api/v1/agent/chat",
                headers=headers,
                json={"agent_key": agent["key"], "message": message},
            )
            assert response.status_code == 200
            events = [
                json.loads(line[6:])
                for line in response.text.splitlines()
                if line.startswith("data: ")
            ]
            assert events[-1]["type"] == "done", response.text
            assert expected in response.text
            assert all(
                event["type"] in {"progress", "tool_call", "tool_result", "token", "done"}
                for event in events
            )
            assert [event["type"] for event in events].count("tool_call") == 1
            assert [event["type"] for event in events].count("tool_result") == 1
            assert (
                "function_call" not in response.text
                and "private business detail" not in response.text
            )
            history = await http.get(
                gateway
                + "/api/v1/agent/conversations/"
                + response.headers["x-agent-conversation-id"]
                + "/messages",
                headers=headers,
            )
            assert history.status_code == 200
            trace = history.json()["items"][-1]["tool_steps"]
            assert len(trace) == 1 and trace[0]["tool_name"] == "get_problem"
            assert trace[0]["status"] == ("failed" if "denied" in message else "succeeded")
            assert set(trace[0]) == {
                "step",
                "call_id",
                "tool_name",
                "status",
                "duration_ms",
                "summary",
            }
            engine = create_async_engine(os.environ["AGENT_TEST_DATABASE_URL"])
            try:
                async with engine.connect() as connection:
                    row = (
                        await connection.execute(
                            text("SELECT status, model_summary FROM agent_runs WHERE id=:id"),
                            {"id": response.headers["x-agent-run-id"]},
                        )
                    ).one()
                    assert (
                        row.status == "COMPLETED"
                        and len(json.loads(row.model_summary)["attempts"]) == 2
                    )
            finally:
                await engine.dispose()

        # 同一 Provider call_id 在不同轮次复用时，Eval 仍保留两条真实轨迹及 Run 归属。
        from tests.integration.test_eval_runs import finished, sample, upload

        http.base_url = gateway
        http.headers.update(headers)
        dataset_id = await upload(
            http,
            [
                sample(
                    "trace_multiturn",
                    turns=[
                        {"role": "user", "content": "PR3 read problem"},
                        {"role": "user", "content": "PR3 read problem"},
                    ],
                )
            ],
        )
        response = await http.post(
            "/api/v1/admin/agent/evals/runs",
            headers={**headers, "X-Request-ID": str(uuid4())},
            json={"dataset_id": dataset_id, "agent_key": agent["key"]},
        )
        assert response.status_code == 200, response.text
        run = await finished(http, response.json()["run"]["id"])
        rows = (
            await http.get(f"/api/v1/admin/agent/evals/runs/{run['id']}/results", headers=headers)
        ).json()["items"]
        result = (
            await http.get(
                f"/api/v1/admin/agent/evals/runs/{run['id']}/results/{rows[0]['id']}",
                headers=headers,
            )
        ).json()["result"]
        assert result["status"] == "COMPLETED", result
        steps = result["model_summary"]["tool_steps"]
        assert len(steps) == 2
        assert {step["call_id"] for step in steps} == {"call_read_problem"}
        assert len({step["run_id"] for step in steps}) == 2
