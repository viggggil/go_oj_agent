"""真实 MySQL 管理 HTTP、幂等、目录过滤与 Gateway/test Agent 闭环。"""

import asyncio
import json
import os
from collections.abc import AsyncIterator
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any, cast
from uuid import uuid4

import httpx
import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from pydantic import SecretStr
from sqlalchemy import select, text
from sqlalchemy.ext.asyncio import AsyncEngine, create_async_engine

from app.core.operations import operation
from app.core.provider_policy import ProviderPolicy
from app.core.settings import Settings
from app.main import create_app
from app.storage.configuration import ConfigurationStore
from app.storage.credentials import CredentialWrite
from app.storage.schema import agent_config_audits as audits
from app.tools.registry import ToolRegistry
from tests.integration.test_chat_http import user_token
from tests.integration.test_configuration import config_engine as config_engine
from tests.integration.test_configuration import write
from tests.integration.test_model_storage import credential_store
from tests.test_chat import token

pytestmark = pytest.mark.integration
BASE = "/api/v1/admin/agent"


@pytest.fixture
async def management_http(
    config_engine: AsyncEngine, tmp_path: Path
) -> AsyncIterator[tuple[httpx.AsyncClient, rsa.RSAPrivateKey, Settings]]:
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    public = tmp_path / "gateway.pem"
    public.write_bytes(
        key.public_key().public_bytes(
            serialization.Encoding.PEM, serialization.PublicFormat.SubjectPublicKeyInfo
        )
    )
    settings = Settings(
        admin_enabled=True,
        config_mode="database",
        database_url=SecretStr(os.environ["AGENT_TEST_DATABASE_URL"]),
        gateway_public_key_file=public,
        provider_allowed_origins=("https://relay.example",),
        admin_provider_key="provider_" + uuid4().hex,
    )
    app = create_app(settings)
    async with (
        app.router.lifespan_context(app),
        httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://agent") as http,
    ):
        yield http, key, settings


def headers(
    key: rsa.RSAPrivateKey,
    method: str,
    path: str,
    request_id: str | None = None,
    roles: list[str] | None = None,
    actor: int = 7,
) -> dict[str, str]:
    return {
        "Authorization": "Bearer "
        + token(
            key,
            rpc=operation(method, path),
            actor_roles=roles or ["admin"],
            actor_id=actor,
            request_id=request_id or str(uuid4()),
        )
    }


async def request(
    http: httpx.AsyncClient,
    key: rsa.RSAPrivateKey,
    method: str,
    path: str,
    payload: dict[str, Any] | None = None,
    **options: Any,
) -> httpx.Response:
    return await http.request(
        method, path, json=payload, headers=headers(key, method, path, **options)
    )


async def test_http_prompt_crud_history_replay_and_conflict(
    management_http: tuple[httpx.AsyncClient, rsa.RSAPrivateKey, Settings],
    config_engine: AsyncEngine,
) -> None:
    http, key, _ = management_http
    name = "api_" + uuid4().hex
    path = BASE + "/prompts"
    body = {"key": name, "content": {"text": "旧提示"}}
    identifier = str(uuid4())
    first = await request(http, key, "POST", path, body, request_id=identifier)
    assert first.status_code == 200, first.text
    old = first.json()["configuration"]["id"]
    assert first.json()["configuration"]["created_by"] == 7
    assert (await request(http, key, "POST", path, body)).status_code == 409
    replacement = {"expected_id": old, "content": {"text": "新提示"}}
    second = await request(http, key, "PUT", path + "/" + name, replacement)
    assert second.status_code == 200, second.text
    current = second.json()["configuration"]["id"]
    assert current != old
    replay = await request(http, key, "POST", path, body, request_id=identifier)
    assert replay.status_code == 200 and replay.json()["configuration"]["id"] == old
    assert replay.json()["configuration"]["archived"]
    assert (
        await request(
            http, key, "POST", path, {**body, "content": {"text": "冲突"}}, request_id=identifier
        )
    ).status_code == 409
    assert (
        await request(http, key, "POST", path, body, request_id=identifier, actor=8)
    ).status_code == 409
    assert (await request(http, key, "PUT", path + "/" + name, replacement)).status_code == 409
    response = await request(http, key, "GET", path + "/" + name + "/versions")
    assert response.json()["page"]["total"] == 2
    assert [item["id"] for item in response.json()["items"]] == [current, old]
    old_response = await request(http, key, "GET", path + "/" + name + "/versions/" + old)
    assert old_response.json()["configuration"]["content"]["text"] == "旧提示"
    archived = await request(
        http, key, "POST", path + "/" + name + "/archive", {"expected_id": current}
    )
    assert archived.status_code == 200 and archived.json()["configuration"]["archived"]
    detail = await request(http, key, "GET", path + "/" + name)
    assert detail.json()["configuration"]["id"] == current
    filtered = await http.get(
        path + "?state=archived&search=" + name, headers=headers(key, "GET", path)
    )
    assert filtered.json()["page"]["total"] == 1 and "content" not in filtered.json()["items"][0]
    restored = await request(
        http,
        key,
        "POST",
        path + "/" + name + "/restore",
        {"expected_id": current, "source_id": old},
    )
    assert restored.status_code == 200, restored.text
    assert restored.json()["configuration"]["id"] not in {old, current}
    async with config_engine.connect() as connection:
        rows = (
            await connection.execute(
                select(audits.c.actor_id).where(audits.c.request_id == identifier)
            )
        ).all()
        assert [row.actor_id for row in rows] == [7]


async def test_http_concurrent_save_pagination_and_invalid_skill(
    management_http: tuple[httpx.AsyncClient, rsa.RSAPrivateKey, Settings],
) -> None:
    http, key, _ = management_http
    prefix = "api_" + uuid4().hex
    prompts = []
    for index in range(3):
        response = await request(
            http,
            key,
            "POST",
            BASE + "/prompts",
            {"key": prefix + str(index), "content": {"text": "提示"}},
        )
        assert response.status_code == 200, response.text
        prompts.append(response.json()["configuration"])
    path = BASE + "/prompts"
    one = await http.get(path + "?page_size=1&search=" + prefix, headers=headers(key, "GET", path))
    two = await http.get(
        path + "?page_size=1&page=2&search=" + prefix, headers=headers(key, "GET", path)
    )
    assert one.json()["page"]["total"] == two.json()["page"]["total"] == 3
    assert one.json()["items"][0]["id"] != two.json()["items"][0]["id"]
    current = prompts[0]
    changed = await asyncio.gather(
        *(
            request(
                http,
                key,
                "PUT",
                path + "/" + current["key"],
                {"expected_id": current["id"], "content": {"text": str(index)}},
            )
            for index in range(4)
        )
    )
    assert sorted(response.status_code for response in changed) == [200, 409, 409, 409]
    invalid_skills: list[dict[str, Any]] = [
        {"execution_mode": "react"},
        {"allowed_tools": ["not_registered"]},
        {"budget": {"max_model_calls": 0}},
        {"prompt_id": str(uuid4())},
    ]
    for invalid in invalid_skills:
        response = await request(
            http,
            key,
            "POST",
            BASE + "/skills",
            {
                "key": prefix + "_s",
                "content": {"name": "测试", "prompt_id": prompts[1]["id"], **invalid},
            },
        )
        assert response.status_code in {400, 409}, response.text
    assert (await request(http, key, "GET", BASE + "/skills/" + prefix + "_s")).status_code == 404


async def test_agent_directory_permissions_expiry_disable_and_model_redaction(
    management_http: tuple[httpx.AsyncClient, rsa.RSAPrivateKey, Settings],
    config_engine: AsyncEngine,
) -> None:
    http, key, settings = management_http
    store = ConfigurationStore(
        config_engine, ToolRegistry(), provider_policy=ProviderPolicy(settings)
    )
    credential = await credential_store(config_engine).create(
        CredentialWrite(actor_id=7, request_id=str(uuid4())), "test", SecretStr("private-api-key")
    )
    provider = await store.create_or_replace(
        write("provider", settings.admin_provider_key),
        {
            "name": "su8 test",
            "base_url": "https://relay.example/v1",
            "credential_id": credential["id"],
        },
    )
    prefix = "api_" + uuid4().hex
    model = await store.create_or_replace(
        write("model", prefix + "_m"),
        {"provider": "responses", "provider_id": provider.id, "model": "deepseek-v4-flash"},
    )
    prompt = (
        await request(
            http,
            key,
            "POST",
            BASE + "/prompts",
            {"key": prefix + "_p", "content": {"text": "提示"}},
        )
    ).json()["configuration"]
    skill = (
        await request(
            http,
            key,
            "POST",
            BASE + "/skills",
            {"key": prefix + "_s", "content": {"name": "测试", "prompt_id": prompt["id"]}},
        )
    ).json()["configuration"]
    body = {
        "name": "测试助手",
        "prompt_id": prompt["id"],
        "skill_ids": [skill["id"]],
        "default_skill_id": skill["id"],
        "model_profile_id": model.id,
        "visibility": "admin",
        "is_test": True,
        "test_expires_at": (datetime.now(UTC) + timedelta(hours=1)).isoformat(),
    }
    response = await request(http, key, "POST", BASE + "/agents", {"key": prefix, "content": body})
    assert response.status_code == 200, response.text
    agent = response.json()["configuration"]
    options = await request(http, key, "GET", BASE + "/model-options")
    assert options.status_code == 200, options.text
    assert any(item["id"] == model.id for item in options.json()["items"])
    assert not any(
        secret in options.text
        for secret in [provider.id, credential["id"], "private-api-key", "base_url"]
    )
    # 设置仅用于本实例目录，不启动 Runtime；管理关闭 Chat 时仍可独立查询/修复。
    settings.chat_enabled = True
    settings.runtime_mode = "model"
    path = "/api/v1/agent/agents"
    result = await http.get(path + "?search=" + prefix, headers=headers(key, "GET", path))
    assert result.status_code == 200, result.text
    assert result.json()["page"]["total"] == 1 and result.json()["items"][0]["key"] == prefix
    public = await http.get(
        path + "?search=" + prefix, headers=headers(key, "GET", path, roles=["user"])
    )
    assert public.json()["page"]["total"] == 0
    assert (
        await request(http, key, "GET", path + "/" + prefix + "/skills", roles=["user"])
    ).status_code == 404
    skills = await request(http, key, "GET", path + "/" + prefix + "/skills")
    assert skills.json()["default_skill_key"] == skill["key"]
    assert "prompt_id" not in skills.text
    assert (await request(http, key, "GET", BASE + "/agents", roles=["user"])).status_code == 403
    settings.provider_allowed_origins = ()
    options = await request(http, key, "GET", BASE + "/model-options")
    assert options.json()["page"]["total"] == 0
    denied = await request(
        http, key, "PUT", BASE + "/agents/" + prefix, {"expected_id": agent["id"], "content": body}
    )
    assert denied.status_code == 400 and denied.json()["code"] == "AGENT_MODEL_ENDPOINT_DENIED"
    result = await http.get(path + "?search=" + prefix, headers=headers(key, "GET", path))
    assert result.json()["page"]["total"] == 0
    settings.provider_allowed_origins = ("https://relay.example",)
    settings.admin_enabled = False
    assert (await request(http, key, "GET", BASE + "/agents")).status_code == 503
    result = await http.get(path + "?search=" + prefix, headers=headers(key, "GET", path))
    assert result.json()["page"]["total"] == 1
    settings.admin_enabled = True
    # 归档绑定仍可运行；停用依赖才阻止新 Run/目录。
    await request(
        http,
        key,
        "POST",
        BASE + "/prompts/" + prompt["key"] + "/archive",
        {"expected_id": prompt["id"]},
    )
    result = await http.get(path + "?search=" + prefix, headers=headers(key, "GET", path))
    assert result.json()["page"]["total"] == 1
    await request(
        http,
        key,
        "POST",
        BASE + "/prompts/" + prompt["key"] + "/disable",
        {"expected_id": prompt["id"]},
    )
    result = await http.get(path + "?search=" + prefix, headers=headers(key, "GET", path))
    assert result.json()["page"]["total"] == 0
    await request(
        http,
        key,
        "POST",
        BASE + "/prompts/" + prompt["key"] + "/enable",
        {"expected_id": prompt["id"]},
    )
    async with config_engine.begin() as connection:
        await connection.execute(
            text(
                "UPDATE agent_config_versions "
                "SET content=JSON_SET(content,'$.test_expires_at',:expiry) WHERE id=:id"
            ),
            {"expiry": (datetime.now(UTC) - timedelta(seconds=10)).isoformat(), "id": agent["id"]},
        )
    result = await http.get(path + "?search=" + prefix, headers=headers(key, "GET", path))
    assert result.json()["page"]["total"] == 0


async def test_gateway_create_test_agent_then_real_direct_chat() -> None:
    if os.environ.get("AGENT_TEST_MODEL_MODE") != "true":
        pytest.skip("model phase only")
    url = os.environ["AGENT_TEST_GATEWAY_URL"]
    prefix = "gateway_" + uuid4().hex
    admin = {"Authorization": "Bearer " + user_token(7301, ["admin"])}
    async with httpx.AsyncClient(base_url=url, timeout=10, headers=admin) as http:

        async def create(kind: str, name: str, body: dict[str, Any]) -> dict[str, Any]:
            response = await http.post(
                BASE + "/" + kind,
                json={"key": name, "content": body},
                headers={"X-Request-ID": str(uuid4())},
            )
            assert response.status_code == 200, response.text
            return cast(dict[str, Any], response.json()["configuration"])

        prompt = await create("prompts", prefix + "_p", {"text": "PR7 测试基础提示"})
        skill_prompt = await create("prompts", prefix + "_sp", {"text": "PR7 测试 Skill 提示"})
        skill = await create(
            "skills", prefix + "_s", {"name": "直接回答", "prompt_id": skill_prompt["id"]}
        )
        options = await http.get(BASE + "/model-options")
        assert options.status_code == 200 and options.json()["items"], options.text
        model = options.json()["items"][0]
        agent = await create(
            "agents",
            prefix,
            {
                "name": "测试 Agent",
                "prompt_id": prompt["id"],
                "skill_ids": [skill["id"]],
                "default_skill_id": skill["id"],
                "model_profile_id": model["id"],
                "visibility": "admin",
                "is_test": True,
                "test_expires_at": (datetime.now(UTC) + timedelta(hours=1)).isoformat(),
            },
        )
        listing = await http.get("/api/v1/agent/agents?search=" + prefix)
        assert listing.status_code == 200 and listing.json()["page"]["total"] == 1, listing.text
        response = await http.post(
            "/api/v1/agent/chat", json={"agent_key": prefix, "message": "PR7 normal chat"}
        )
        assert response.status_code == 200, response.text
        events = [
            json.loads(line[6:]) for line in response.text.splitlines() if line.startswith("data: ")
        ]
        assert events[-1]["type"] == "done"
        assert any(item["data"].get("text") == "中文算法回答" for item in events)
        denied = await http.post(
            "/api/v1/agent/chat",
            json={"agent_key": prefix, "message": "no access"},
            headers={"Authorization": "Bearer " + user_token(7302)},
        )
        assert denied.status_code == 404
        denied = await http.get(
            BASE + "/agents", headers={"Authorization": "Bearer " + user_token(7302)}
        )
        assert denied.status_code == 403
        # 每类管理资源经网关验证查询/版本及全部状态动作的响应契约。
        for kind, resource in [("prompts", prompt), ("skills", skill), ("agents", agent)]:
            path = BASE + "/" + kind
            detail_path = path + "/" + resource["key"]
            listing = await http.get(path + "?search=" + resource["key"])
            assert listing.status_code == 200 and listing.json()["items"], listing.text
            detail = await http.get(detail_path)
            assert detail.status_code == 200, detail.text
            updated = await http.put(
                detail_path,
                json={"expected_id": resource["id"], "content": resource["content"]},
                headers={"X-Request-ID": str(uuid4())},
            )
            assert updated.status_code == 200, updated.text
            identifier = updated.json()["configuration"]["id"]
            history = await http.get(detail_path + "/versions")
            assert history.status_code == 200 and history.json()["page"]["total"] == 2, history.text
            old = await http.get(detail_path + "/versions/" + resource["id"])
            assert old.status_code == 200 and old.json()["configuration"]["archived"], old.text
            for action in ["archive", "disable", "enable"]:
                result = await http.post(
                    detail_path + "/" + action,
                    json={"expected_id": identifier},
                    headers={"X-Request-ID": str(uuid4())},
                )
                assert result.status_code == 200, result.text
            result = await http.post(
                detail_path + "/restore",
                json={"expected_id": identifier, "source_id": resource["id"]},
                headers={"X-Request-ID": str(uuid4())},
            )
            assert (
                result.status_code == 200 and result.json()["configuration"]["id"] != identifier
            ), result.text
        engine = create_async_engine(os.environ["AGENT_TEST_DATABASE_URL"])
        try:
            async with engine.connect() as connection:
                row = (
                    await connection.execute(
                        text("SELECT config_snapshot FROM agent_runs WHERE id=:id"),
                        {"id": response.headers["x-agent-run-id"]},
                    )
                ).one()
                snapshot = json.loads(row.config_snapshot)
                assert (
                    snapshot["version"] == agent["id"]
                    and snapshot["prompt_text"] == "PR7 测试基础提示"
                )
                assert snapshot["skill_prompt_text"] == "PR7 测试 Skill 提示"
        finally:
            await engine.dispose()
