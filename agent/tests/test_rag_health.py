"""RAG worker liveness and dependency readiness contract."""

from typing import Literal

from fastapi.testclient import TestClient

from app.rag.health import create_health_app


class FakeDatabase:
    def __init__(self, status: Literal["ready", "unavailable", "not_configured"] = "ready") -> None:
        self.status = status
        self.checks = 0

    async def check(self) -> Literal["ready", "unavailable", "not_configured"]:
        self.checks += 1
        return self.status


class FakeClients:
    def __init__(self, status: Literal["ready", "unavailable"] = "ready") -> None:
        self.status = status
        self.checks: list[str] = []

    async def check_embedding(self) -> Literal["ready", "unavailable"]:
        self.checks.append("embedding")
        return self.status

    async def check_qdrant(self) -> Literal["ready", "unavailable"]:
        self.checks.append("qdrant")
        return self.status

    async def check_minio(self) -> Literal["ready", "unavailable"]:
        self.checks.append("minio")
        return self.status


def test_liveness_does_not_touch_dependencies() -> None:
    database = FakeDatabase("unavailable")
    clients = FakeClients("unavailable")
    with TestClient(create_health_app(database, clients)) as http:
        response = http.get("/healthz")
        assert response.status_code == 200
        assert response.json() == {"service": "rag-worker", "status": "ok"}
    assert database.checks == 0
    assert clients.checks == []


def test_readiness_reports_all_dependencies() -> None:
    database = FakeDatabase()
    clients = FakeClients()
    with TestClient(create_health_app(database, clients)) as http:
        response = http.get("/readyz")
        assert response.status_code == 200
        assert response.json() == {
            "service": "rag-worker",
            "status": "ready",
            "checks": {
                "database": "ready",
                "embedding": "ready",
                "qdrant": "ready",
                "minio": "ready",
            },
        }
    assert database.checks == 1
    assert sorted(clients.checks) == ["embedding", "minio", "qdrant"]


def test_readiness_returns_503_without_dependency_details() -> None:
    database = FakeDatabase("unavailable")
    clients = FakeClients("unavailable")
    with TestClient(create_health_app(database, clients)) as http:
        response = http.get("/readyz")
        assert response.status_code == 503
        assert response.json() == {
            "service": "rag-worker",
            "status": "not_ready",
            "checks": {
                "database": "unavailable",
                "embedding": "unavailable",
                "qdrant": "unavailable",
                "minio": "unavailable",
            },
        }
        assert "password" not in response.text.lower()
