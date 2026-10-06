"""HTTP 契约、依赖注入、生命周期和错误脱敏。"""

import pytest
from fastapi.testclient import TestClient

from app.core.database import DatabaseStatus
from app.core.settings import Settings
from app.main import create_app


class FakeProbe:
    def __init__(self, status: DatabaseStatus = "ready") -> None:
        self.status = status
        self.checks = 0
        self.closed = False

    async def check(self) -> DatabaseStatus:
        self.checks += 1
        return self.status

    async def close(self) -> None:
        self.closed = True


def test_liveness_does_not_touch_database() -> None:
    probe = FakeProbe("unavailable")
    application = create_app(Settings(), probe_factory=lambda _: probe)
    with TestClient(application) as client:
        response = client.get("/healthz")
        assert response.status_code == 200
        assert response.json() == {"service": "agent-service", "status": "ok"}
        assert probe.checks == 0
    assert probe.closed
    assert not hasattr(application.state, "resources")


def test_readiness_uses_database_check() -> None:
    probe = FakeProbe()
    with TestClient(create_app(Settings(), probe_factory=lambda _: probe)) as client:
        response = client.get("/readyz")
        assert response.status_code == 200
        assert response.json() == {
            "service": "agent-service",
            "status": "ready",
            "checks": {"database": "ready"},
        }
        assert probe.checks == 1
    assert probe.closed


def test_readiness_recovers_without_restart() -> None:
    probe = FakeProbe("unavailable")
    with TestClient(create_app(Settings(), probe_factory=lambda _: probe)) as client:
        response = client.get("/readyz")
        assert response.status_code == 503
        assert response.json()["checks"] == {"database": "unavailable"}
        probe.status = "ready"
        assert client.get("/readyz").status_code == 200


def test_unconfigured_database_is_not_ready() -> None:
    with TestClient(create_app(Settings())) as client:
        assert client.get("/healthz").status_code == 200
        response = client.get("/readyz")
        assert response.status_code == 503
        assert response.json() == {
            "service": "agent-service",
            "status": "not_ready",
            "checks": {"database": "not_configured"},
        }
        # Chat 不能因健康检查成功而被误认为已经提供。
        assert client.post("/api/v1/agent/chat", json={"message": "hello"}).status_code == 404


def test_request_failure_does_not_expose_exception(caplog: pytest.LogCaptureFixture) -> None:
    probe = FakeProbe()
    application = create_app(Settings(), probe_factory=lambda _: probe)

    @application.get("/test-failure")
    async def fail() -> None:
        raise RuntimeError("private-password-in-driver-error")

    with TestClient(application, raise_server_exceptions=False) as client:
        response = client.get("/test-failure")
        assert response.status_code == 500
        assert response.json()["code"] == "AGENT_INTERNAL_ERROR"
        assert "private-password" not in response.text
    assert "private-password" not in caplog.text
    assert "AGENT_INTERNAL_ERROR" == getattr(caplog.records[-1], "error_code", None)
    assert probe.closed


def test_shutdown_failure_is_redacted(caplog: pytest.LogCaptureFixture) -> None:
    class BrokenProbe(FakeProbe):
        async def close(self) -> None:
            self.closed = True
            raise RuntimeError("private-shutdown-password")

    probe = BrokenProbe()
    application = create_app(Settings(), probe_factory=lambda _: probe)
    with TestClient(application) as client:
        assert client.get("/healthz").status_code == 200
    assert probe.closed
    assert not hasattr(application.state, "resources")
    assert "private-shutdown-password" not in caplog.text
    assert getattr(caplog.records[-1], "error_code", None) == "AGENT_SHUTDOWN_FAILED"
