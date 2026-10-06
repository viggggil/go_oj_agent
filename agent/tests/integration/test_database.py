"""真实 MySQL 和已构建容器的探针契约。"""

import os

import httpx
import pytest
from pydantic import SecretStr

from app.core.database import DatabaseProbe
from app.core.settings import Settings

pytestmark = pytest.mark.integration


async def test_real_mysql_connection_and_pool_cleanup() -> None:
    url = os.environ.get("AGENT_TEST_DATABASE_URL")
    if not url:
        pytest.skip("AGENT_TEST_DATABASE_URL is not set")
    probe = DatabaseProbe(Settings(database_url=SecretStr(url)))
    try:
        assert await probe.check() == "ready"
        assert await probe.check() == "ready"
    finally:
        await probe.close()


async def test_database_authentication_failure_is_unavailable() -> None:
    url = os.environ.get("AGENT_TEST_BAD_DATABASE_URL")
    if not url:
        pytest.skip("AGENT_TEST_BAD_DATABASE_URL is not set")
    probe = DatabaseProbe(Settings(database_url=SecretStr(url)))
    try:
        assert await probe.check() == "unavailable"
    finally:
        await probe.close()


def test_container_uses_configured_database() -> None:
    base_url = os.environ.get("AGENT_TEST_BASE_URL")
    if not base_url:
        pytest.skip("AGENT_TEST_BASE_URL is not set")
    response = httpx.get(base_url + "/healthz", timeout=5)
    assert response.status_code == 200
    assert response.json() == {"service": "agent-service", "status": "ok"}
    response = httpx.get(base_url + "/readyz", timeout=5)
    assert response.status_code == 200
    assert response.json() == {
        "service": "agent-service",
        "status": "ready",
        "checks": {"database": "ready"},
    }
