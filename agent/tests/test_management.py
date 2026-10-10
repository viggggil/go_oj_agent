"""管理接口的认证、资源绑定、输入边界和部署隔离。"""

from dataclasses import replace
from pathlib import Path
from typing import Any, cast
from unittest.mock import AsyncMock
from uuid import uuid4

import pytest
from cryptography.hazmat.primitives.asymmetric import rsa
from fastapi.testclient import TestClient
from pydantic import ValidationError

from app.core.auth import DelegationVerifier, InvalidDelegation
from app.core.management import ManagementService
from app.core.operations import operation
from app.core.settings import Settings
from app.graphs.runtime import FakeRuntime
from app.graphs.service import RunService
from app.main import create_app
from app.models.admin import PageQuery
from app.models.runtime import Principal
from app.tools.registry import ToolRegistry
from tests.test_chat import key as key
from tests.test_chat import settings as settings
from tests.test_chat import token
from tests.test_health import FakeProbe
from tests.test_runtime import MemoryStore, Reader

BASE = "/api/v1/admin/agent/prompts"


@pytest.mark.parametrize("resource", ["agents", "prompts", "skills"])
def test_operation_whitelist(resource: str) -> None:
    base = "/api/v1/admin/agent/" + resource
    pairs = [
        ("GET", base),
        ("POST", base),
        ("GET", base + "/test_key"),
        ("PUT", base + "/test_key"),
        ("GET", base + "/test_key/versions"),
        ("GET", base + "/test_key/versions/" + str(uuid4())),
    ]
    pairs.extend(
        ("POST", base + "/test_key/" + action)
        for action in ["archive", "restore", "disable", "enable"]
    )
    for method, path in pairs:
        assert operation(method, path) == f"HTTP {method} {path}"
    for method, path in [
        ("DELETE", base),
        ("PUT", base),
        ("POST", base + "/test_key/versions"),
        ("GET", base + "/test_key/archive"),
        ("GET", base + "/test_key/versions/not-uuid"),
        ("GET", base + "/test_key/versions/" + str(uuid4()).upper()),
        ("GET", base + "/bad-key"),
    ]:
        with pytest.raises(ValueError):
            operation(method, path)


def test_delegation_cannot_change_method_resource_or_scope(
    settings: Settings, key: rsa.RSAPrivateKey
) -> None:
    verifier = DelegationVerifier(settings)
    expected = operation("GET", BASE + "/test_key")
    delegated = "Bearer " + token(key, rpc=expected)
    assert verifier.verify(delegated, expected).user_id == 7
    for target in [
        operation("PUT", BASE + "/test_key"),
        operation("GET", BASE + "/other_key"),
        operation("GET", "/api/v1/admin/agent/skills/test_key"),
        operation("POST", "/api/v1/agent/chat"),
    ]:
        with pytest.raises(InvalidDelegation):
            verifier.verify(delegated, target)
    with pytest.raises(InvalidDelegation):
        verifier.verify("Bearer " + token(key), expected)


class RecordingManagement:
    def __init__(self, settings: Settings) -> None:
        self.settings = settings
        self.registry = ToolRegistry()
        self.saved: tuple[Any, ...] | None = None

    async def list(self, kind: str, query: PageQuery, key: str | None = None) -> dict[str, Any]:
        return {"items": [], "page": {"page": query.page, "page_size": query.page_size, "total": 0}}

    async def save(self, *args: Any) -> dict[str, Any]:
        self.saved = args
        return {"configuration": {"id": str(uuid4()), "key": args[1]}}


def test_http_role_body_limits_and_trusted_actor(
    settings: Settings, key: rsa.RSAPrivateKey
) -> None:
    settings.chat_enabled = False
    settings.max_request_bytes = 1024
    app = create_app(settings, probe_factory=lambda _: FakeProbe())
    service = RecordingManagement(settings)
    with TestClient(app) as http:
        settings.admin_enabled = True
        app.state.resources = replace(
            app.state.resources,
            management=cast(ManagementService, service),
            verifier=DelegationVerifier(settings),
        )
        assert http.get(BASE).status_code == 401

        def headers(
            method: str, roles: list[str], path: str = BASE, request_id: str | None = None
        ) -> dict[str, str]:
            return {
                "Authorization": "Bearer "
                + token(
                    key,
                    rpc=operation(method, path),
                    actor_roles=roles,
                    request_id=request_id or str(uuid4()),
                ),
                "Content-Type": "application/json",
            }

        assert http.get(BASE, headers=headers("GET", ["user"])).status_code == 403
        for role in ["admin", "agent_admin", "system_admin"]:
            response = http.get(BASE, headers=headers("GET", [role]))
            assert response.status_code == 200 and response.json()["items"] == []
        for query in ["page=0", "page_size=101", "page=1&page=2", "actor_id=7", "state=other"]:
            assert (
                http.get(BASE + "?" + query, headers=headers("GET", ["admin"])).status_code == 400
            )
        normal = {"key": "test_prompt", "content": {"text": "hello"}}
        for field in ["actor_id", "role", "request_id", "kind"]:
            assert (
                http.post(
                    BASE, json={**normal, field: "forged"}, headers=headers("POST", ["admin"])
                ).status_code
                == 400
            )
        assert service.saved is None
        assert (
            http.post(
                BASE, json=normal, headers=headers("POST", ["admin"], request_id="random")
            ).status_code
            == 400
        )
        assert (
            http.post(
                BASE,
                json={"key": "test_prompt", "content": {"text": "x" * 2000}},
                headers=headers("POST", ["admin"]),
            ).status_code
            == 413
        )
        assert (
            http.post(
                BASE,
                content="{}",
                headers={**headers("POST", ["admin"]), "Content-Type": "text/plain"},
            ).status_code
            == 415
        )
        response = http.post(BASE, json=normal, headers=headers("POST", ["admin"]))
        assert response.status_code == 200 and service.saved is not None
        principal = service.saved[3]
        assert isinstance(principal, Principal) and principal.user_id == 7


def test_disabled_management_and_configuration_validation(tmp_path: Path) -> None:
    with TestClient(create_app(Settings(), probe_factory=lambda _: FakeProbe())) as http:
        assert http.get(BASE).status_code == 503
    with pytest.raises(ValidationError):
        Settings(admin_enabled=True)


def test_chat_directory_works_with_management_disabled(
    settings: Settings, key: rsa.RSAPrivateKey
) -> None:
    settings.config_mode = "database"
    app = create_app(
        settings, run_service_factory=lambda *_: RunService(MemoryStore(), FakeRuntime(), Reader())
    )
    path = "/api/v1/agent/agents"
    with TestClient(app) as http:
        service = app.state.resources.management
        assert service is not None
        service.catalog = AsyncMock(
            return_value={"items": [], "page": {"page": 1, "page_size": 20, "total": 0}}
        )
        response = http.get(
            path, headers={"Authorization": "Bearer " + token(key, rpc=operation("GET", path))}
        )
        assert response.status_code == 200
        assert (
            http.get(
                BASE,
                headers={
                    "Authorization": "Bearer "
                    + token(key, rpc=operation("GET", BASE), actor_roles=["admin"])
                },
            ).status_code
            == 503
        )
    settings.config_mode = "demo"
    app = create_app(
        settings,
        probe_factory=lambda _: FakeProbe(),
        run_service_factory=lambda *_: RunService(MemoryStore(), FakeRuntime(), Reader()),
    )
    with TestClient(app) as http:
        response = http.get(
            path, headers={"Authorization": "Bearer " + token(key, rpc=operation("GET", path))}
        )
        assert response.status_code == 200 and response.json()["page"]["total"] == 0
