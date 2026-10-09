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
        assert [event["type"] for event in received] == ["thinking", "token", "done"]
        assert received[1]["data"]["text"] == "中文算法回答"
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
        assert [event["type"] for event in events] == ["thinking", "token", "error"]
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
