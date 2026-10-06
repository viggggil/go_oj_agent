"""真实 Go Gateway -> Python 委托验证/SSE -> MySQL 会话持久化。"""

import json
import os
import time
from pathlib import Path
from uuid import uuid4

import httpx
import jwt
import pytest
from sqlalchemy import text
from sqlalchemy.ext.asyncio import create_async_engine

pytestmark = pytest.mark.integration


def user_token(user_id: int, roles: list[str] | None = None) -> str:
    now = int(time.time())
    key = Path(os.environ["AGENT_TEST_USER_PRIVATE_KEY_FILE"]).read_bytes()
    return jwt.encode(
        {
            "sub": user_id,
            "username": "test-user",
            "roles": roles or ["user"],
            "iss": "go-oj-agent",
            "aud": "go-oj-gateway",
            "iat": now,
            "exp": now + 60,
            "jti": str(uuid4()),
        },
        key,
        algorithm="RS256",
        headers={"kid": "user-test"},
    )


def events(response: httpx.Response) -> list[dict[str, object]]:
    assert response.status_code == 200, response.text
    assert response.headers["content-type"].startswith("text/event-stream")
    result = []
    for frame in response.text.strip().split("\n\n"):
        if frame.startswith(":"):
            continue
        result.append(json.loads(frame.split("data: ", 1)[1]))
    assert [event["type"] for event in result] == ["thinking", "token", "done"]
    assert result[-1]["run_id"] == response.headers["x-agent-run-id"]
    return result


async def test_gateway_signed_actor_persistence_and_conversation_restore() -> None:
    url = os.environ.get("AGENT_TEST_GATEWAY_URL")
    if not url:
        pytest.skip("AGENT_TEST_GATEWAY_URL is not set")
    headers = {
        "Authorization": "Bearer " + user_token(7001),
        "X-Request-ID": "cross-language-test",
        "X-User-ID": "999",
    }
    async with httpx.AsyncClient(base_url=url, timeout=10) as http:
        first = events(
            await http.post("/api/v1/agent/chat", json={"message": "介绍算法"}, headers=headers)
        )
        conversation = first[-1]["conversation_id"]
        second = events(
            await http.post(
                "/api/v1/agent/chat",
                json={"message": "制定学习计划", "conversation_id": conversation},
                headers=headers,
            )
        )
        assert second[-1]["conversation_id"] == conversation
        denied = await http.post(
            "/api/v1/agent/chat",
            json={"message": "other owner", "conversation_id": conversation},
            headers={"Authorization": "Bearer " + user_token(7002, ["admin"])},
        )
        missing = await http.post(
            "/api/v1/agent/chat",
            json={"message": "missing", "conversation_id": str(uuid4())},
            headers=headers,
        )
        assert denied.status_code == missing.status_code == 404
        assert denied.json() == missing.json()
    engine = create_async_engine(os.environ["AGENT_TEST_DATABASE_URL"])
    try:
        async with engine.connect() as connection:
            runs = (
                await connection.execute(
                    text(
                        "SELECT user_id,request_id,status FROM agent_runs "
                        "WHERE conversation_id=:id ORDER BY started_at,id"
                    ),
                    {"id": conversation},
                )
            ).all()
            assert [(run.user_id, run.request_id, run.status) for run in runs] == [
                (7001, "cross-language-test", "COMPLETED"),
                (7001, "cross-language-test", "COMPLETED"),
            ]
            rows = (
                await connection.execute(
                    text(
                        "SELECT role,content FROM agent_messages "
                        "WHERE conversation_id=:id ORDER BY id"
                    ),
                    {"id": conversation},
                )
            ).all()
            assert [row.role for row in rows] == ["user", "assistant", "user", "assistant"]
            assert rows[0].content == "介绍算法" and rows[2].content == "制定学习计划"
            assert "演示回答" in rows[1].content and "演示回答" in rows[3].content
    finally:
        await engine.dispose()


async def test_gateway_and_agent_reject_spoofing_before_run() -> None:
    url = os.environ.get("AGENT_TEST_GATEWAY_URL")
    if not url:
        pytest.skip("AGENT_TEST_GATEWAY_URL is not set")
    async with httpx.AsyncClient(base_url=url, timeout=10) as http:
        assert (
            await http.post(
                "/api/v1/agent/chat", json={"message": "hello"}, headers={"X-User-ID": "7"}
            )
        ).status_code == 401
        headers = {"Authorization": "Bearer " + user_token(7003)}
        invalid = await http.post(
            "/api/v1/agent/chat", json={"message": "hello", "user_id": 1}, headers=headers
        )
        assert invalid.status_code == 400
        for roles in (["role"] * 33, ["r" * 65], [""]):
            invalid_identity = await http.post(
                "/api/v1/agent/chat",
                json={"message": "hello"},
                headers={"Authorization": "Bearer " + user_token(7003, roles)},
            )
            assert invalid_identity.status_code == 400
            assert invalid_identity.json()["reason"] == "GATEWAY_AGENT_INVALID_ARGUMENT"
        oversized = await http.post(
            "/api/v1/agent/chat", json={"message": "x" * 262144}, headers=headers
        )
        assert oversized.status_code == 413
    async with httpx.AsyncClient(base_url=os.environ["AGENT_TEST_BASE_URL"], timeout=10) as http:
        assert (
            await http.post(
                "/api/v1/agent/chat", json={"message": "hello"}, headers={"X-User-ID": "7"}
            )
        ).status_code == 401
        # 外部 Access token 不是内部 Gateway 委托。
        assert (
            await http.post("/api/v1/agent/chat", json={"message": "hello"}, headers=headers)
        ).status_code == 401
    engine = create_async_engine(os.environ["AGENT_TEST_DATABASE_URL"])
    try:
        async with engine.connect() as connection:
            assert (
                await connection.execute(text("SELECT id FROM agent_runs WHERE user_id=7003"))
            ).first() is None
    finally:
        await engine.dispose()
