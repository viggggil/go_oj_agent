"""MySQL 为发布状态真源；对象/向量只在已发布版本中参与检索。"""

import hashlib
import json
from collections.abc import Awaitable, Callable
from datetime import UTC, datetime, timedelta
from typing import Any
from uuid import NAMESPACE_URL, uuid4, uuid5

from sqlalchemy import and_, func, insert, or_, select, update
from sqlalchemy.dialects.mysql import insert as mysql_insert
from sqlalchemy.ext.asyncio import AsyncConnection, AsyncEngine

from app.models.configuration import ConfigError
from app.models.runtime import Principal
from app.rag.clients import RagClients
from app.rag.models import REVISION, SPLITTER, Page, Upload
from app.rag.schema import bases, documents, requests, tasks, versions


def now() -> datetime:
    return datetime.now(UTC).replace(tzinfo=None)


def public(row: dict[str, Any]) -> dict[str, Any]:
    return {
        key: (value.isoformat() + "Z" if isinstance(value, datetime) else value)
        for key, value in row.items()
        if key not in {"object_key", "lease_token", "lease_until"}
    }


class KnowledgeStore:
    def __init__(self, engine: AsyncEngine) -> None:
        self.engine = engine

    async def write(
        self,
        principal: Principal,
        action: str,
        body: dict[str, Any],
        mutation: Callable[[AsyncConnection], Awaitable[dict[str, Any]]],
    ) -> dict[str, Any]:
        fingerprint = hashlib.sha256(
            json.dumps(
                [action, body],
                ensure_ascii=False,
                sort_keys=True,
                separators=(",", ":"),
            ).encode()
        ).hexdigest()
        async with self.engine.begin() as conn:
            await conn.execute(
                mysql_insert(requests)
                .values(
                    request_id=principal.request_id,
                    actor_id=principal.user_id,
                    fingerprint=fingerprint,
                    response={},
                    created_at=now(),
                )
                .on_duplicate_key_update(request_id=requests.c.request_id)
            )
            saved = (
                (
                    await conn.execute(
                        select(requests)
                        .where(
                            requests.c.request_id == principal.request_id,
                        )
                        .with_for_update()
                    )
                )
                .mappings()
                .one()
            )
            if saved["actor_id"] != principal.user_id or saved["fingerprint"] != fingerprint:
                raise ConfigError("AGENT_RAG_REQUEST_CONFLICT", 409)
            if saved["response"]:
                return dict(saved["response"])
            result = await mutation(conn)
            await conn.execute(
                update(requests)
                .where(requests.c.request_id == principal.request_id)
                .values(response=result)
            )
            return result

    async def create_base(self, principal: Principal, name: str) -> dict[str, Any]:
        async def mutation(conn: AsyncConnection) -> dict[str, Any]:
            value = {
                "id": str(uuid4()),
                "name": name,
                "created_by": principal.user_id,
                "created_at": now(),
            }
            await conn.execute(insert(bases).values(**value))
            return {"knowledge_base": public(value)}

        return await self.write(principal, "create_base", {"name": name}, mutation)

    async def base_detail(self, base_id: str) -> dict[str, Any]:
        async with self.engine.connect() as conn:
            row = (
                (await conn.execute(select(bases).where(bases.c.id == base_id)))
                .mappings()
                .one_or_none()
            )
            if row is None:
                raise ConfigError("AGENT_RAG_NOT_FOUND", 404)
            return {"knowledge_base": public(dict(row))}

    async def rename_base(
        self, principal: Principal, base_id: str, name: str, expected_name: str
    ) -> dict[str, Any]:
        async def mutation(conn: AsyncConnection) -> dict[str, Any]:
            row = (
                (await conn.execute(select(bases).where(bases.c.id == base_id).with_for_update()))
                .mappings()
                .one_or_none()
            )
            if row is None:
                raise ConfigError("AGENT_RAG_NOT_FOUND", 404)
            if row["name"] != expected_name:
                raise ConfigError("AGENT_RAG_STALE", 409)
            await conn.execute(update(bases).where(bases.c.id == base_id).values(name=name))
            return {"knowledge_base": public(dict(row) | {"name": name})}

        return await self.write(
            principal, f"rename:{base_id}", {"name": name, "expected_name": expected_name}, mutation
        )

    async def upload(
        self,
        principal: Principal,
        body: Upload,
        clients: RagClients,
    ) -> dict[str, Any]:
        version_id = str(uuid5(NAMESPACE_URL, f"rag:{principal.user_id}:{principal.request_id}"))
        content = body.content.encode("utf-8")
        digest = hashlib.sha256(content).hexdigest()
        # 请求重放采用确定性对象键，冲突请求不能覆盖首次原文。
        key = f"bases/{body.base_id}/versions/{version_id}/{digest}.txt"
        async with self.engine.connect() as conn:
            if not (
                await conn.execute(
                    select(bases.c.id).where(
                        bases.c.id == str(body.base_id),
                    )
                )
            ).scalar_one_or_none():
                raise ConfigError("AGENT_RAG_NOT_FOUND", 404)

        async def mutation(conn: AsyncConnection) -> dict[str, Any]:
            # 与孤立对象清理共用 base 行锁；先写对象，再写不可变文档元数据。
            await conn.execute(
                select(bases.c.id)
                .where(
                    bases.c.id == str(body.base_id),
                )
                .with_for_update()
            )
            await clients.put(key, content)
            document_id = str(body.document_id or uuid4())
            if body.document_id:
                doc = (
                    (
                        await conn.execute(
                            select(documents)
                            .where(
                                documents.c.id == document_id,
                            )
                            .with_for_update()
                        )
                    )
                    .mappings()
                    .one_or_none()
                )
                if doc is None or doc["base_id"] != str(body.base_id):
                    raise ConfigError("AGENT_RAG_NOT_FOUND", 404)
                if doc["current_version"] != str(body.expected_version):
                    raise ConfigError("AGENT_RAG_STALE", 409)
            else:
                await conn.execute(
                    insert(documents).values(
                        id=document_id,
                        base_id=str(body.base_id),
                        enabled=False,
                        created_at=now(),
                    )
                )
            value = body.model_dump(
                mode="json",
                exclude={
                    "content",
                    "base_id",
                    "expected_version",
                    "document_id",
                },
            ) | {
                "id": version_id,
                "document_id": document_id,
                "object_key": key,
                "content_hash": digest,
                "size_bytes": len(content),
                "model_revision": REVISION,
                "splitter_version": SPLITTER,
                "created_by": principal.user_id,
                "created_at": now(),
            }
            await conn.execute(insert(versions).values(**value))
            await conn.execute(
                update(documents)
                .where(documents.c.id == document_id)
                .values(current_version=version_id)
            )
            await conn.execute(
                insert(tasks).values(
                    version_id=version_id,
                    state="PENDING",
                    attempts=0,
                    next_attempt=now(),
                    chunk_count=0,
                    updated_at=now(),
                )
            )
            return {
                "document": public(value)
                | {
                    "base_id": str(body.base_id),
                    "enabled": bool(doc["enabled"])
                    if body.document_id and doc is not None
                    else False,
                    "state": "PENDING",
                    "chunk_count": 0,
                    "error_code": None,
                    "active_version": doc["active_version"]
                    if body.document_id and doc is not None
                    else None,
                }
            }

        return await self.write(principal, "upload", body.model_dump(mode="json"), mutation)

    def document_query(self, *, history: bool = False) -> Any:
        return select(
            documents.c.base_id,
            documents.c.enabled,
            documents.c.active_version,
            versions,
            tasks.c.state,
            tasks.c.chunk_count,
            tasks.c.error_code,
        ).select_from(
            documents.join(
                versions,
                versions.c.document_id == documents.c.id
                if history
                else documents.c.current_version == versions.c.id,
            ).join(tasks, tasks.c.version_id == versions.c.id)
        )

    async def detail(self, document_id: str) -> dict[str, Any]:
        async with self.engine.connect() as conn:
            row = (
                (
                    await conn.execute(
                        self.document_query().where(
                            documents.c.id == document_id,
                        )
                    )
                )
                .mappings()
                .one_or_none()
            )
            if row is None:
                raise ConfigError("AGENT_RAG_NOT_FOUND", 404)
            return dict(row)

    async def page(
        self, resource: str, page: Page, document_id: str | None = None
    ) -> dict[str, Any]:
        if document_id:
            await self.detail(document_id)
        query = (
            select(bases)
            if resource == "knowledge-bases"
            else self.document_query(history=document_id is not None)
        )
        if document_id:
            query = query.where(documents.c.id == document_id)
        if page.base_id:
            if resource == "knowledge-bases":
                raise ConfigError("AGENT_INVALID_ARGUMENT")
            query = query.where(documents.c.base_id == str(page.base_id))
        ordering = (
            bases if resource == "knowledge-bases" else versions if document_id else documents
        )
        async with self.engine.connect() as conn:
            total = (
                await conn.execute(select(func.count()).select_from(query.subquery()))
            ).scalar_one()
            rows = (
                (
                    await conn.execute(
                        query.order_by(
                            ordering.c.created_at.desc(),
                            ordering.c.id.desc(),
                        )
                        .offset((page.page - 1) * page.page_size)
                        .limit(page.page_size)
                    )
                )
                .mappings()
                .all()
            )
            return {
                "items": [public(dict(row)) for row in rows],
                "page": {"page": page.page, "page_size": page.page_size, "total": total},
            }

    async def change(
        self,
        principal: Principal,
        document_id: str,
        expected: str,
        action: str,
    ) -> dict[str, Any]:
        async def mutation(conn: AsyncConnection) -> dict[str, Any]:
            doc = (
                (
                    await conn.execute(
                        select(documents)
                        .where(
                            documents.c.id == document_id,
                        )
                        .with_for_update()
                    )
                )
                .mappings()
                .one_or_none()
            )
            if doc is None:
                raise ConfigError("AGENT_RAG_NOT_FOUND", 404)
            if doc["current_version"] != expected:
                raise ConfigError("AGENT_RAG_STALE", 409)
            task = (
                (
                    await conn.execute(
                        select(tasks)
                        .where(
                            tasks.c.version_id == expected,
                        )
                        .with_for_update()
                    )
                )
                .mappings()
                .one()
            )
            if action == "retry":
                if task["state"] != "FAILED":
                    raise ConfigError("AGENT_RAG_STALE", 409)
                await conn.execute(
                    update(tasks)
                    .where(tasks.c.version_id == expected)
                    .values(
                        state="PENDING",
                        attempts=0,
                        next_attempt=now(),
                        updated_at=now(),
                        error_code=None,
                        lease_token=None,
                        lease_until=None,
                    )
                )
            else:
                if action == "enable" and not doc["active_version"]:
                    raise ConfigError("AGENT_RAG_NOT_READY", 409)
                await conn.execute(
                    update(documents)
                    .where(documents.c.id == document_id)
                    .values(enabled=action == "enable")
                )
            return {
                "result": {"document_id": document_id, "version_id": expected, "action": action}
            }

        return await self.write(
            principal, f"{action}:{document_id}", {"expected_version": expected}, mutation
        )

    async def active(self, base_ids: list[str], problem_id: int | None) -> list[dict[str, Any]]:
        if not base_ids:
            return []
        query = (
            select(versions, documents.c.base_id)
            .select_from(
                documents.join(versions, documents.c.active_version == versions.c.id),
            )
            .where(documents.c.enabled.is_(True), documents.c.base_id.in_(base_ids))
        )
        if problem_id is not None:
            query = query.where(versions.c.problem_id == problem_id)
        async with self.engine.connect() as conn:
            rows = (await conn.execute(query.limit(2001))).mappings().all()
            if len(rows) > 2000:
                raise ConfigError("AGENT_UNAVAILABLE", 503)
            return [dict(row) for row in rows]

    async def pinned(
        self, base_ids: list[str], version_ids: list[str], problem_id: int | None = None
    ) -> list[dict[str, Any]]:
        """旧 Run 可继续读取旧发布版本；停用覆盖快照，不能回退到当前全库。"""
        if not base_ids or not version_ids:
            return []
        query = (
            select(versions, documents.c.base_id)
            .select_from(
                documents.join(versions, documents.c.id == versions.c.document_id).join(
                    tasks, tasks.c.version_id == versions.c.id
                )
            )
            .where(
                documents.c.enabled.is_(True),
                documents.c.base_id.in_(base_ids),
                versions.c.id.in_(version_ids),
                tasks.c.state == "READY",
            )
        )
        if problem_id is not None:
            query = query.where(versions.c.problem_id == problem_id)
        async with self.engine.connect() as conn:
            return [dict(row) for row in (await conn.execute(query)).mappings()]

    async def evidence(self, run_id: str) -> dict[str, Any]:
        """仅管理员路由调用；严格投影，不能返回完整配置或模型输入。"""
        from app.models.provider import KnowledgeAttempt
        from app.storage.schema import agent_runs

        async with self.engine.connect() as conn:
            row = (
                (
                    await conn.execute(
                        select(
                            agent_runs.c.status,
                            agent_runs.c.config_snapshot,
                            agent_runs.c.model_summary,
                        ).where(agent_runs.c.id == run_id)
                    )
                )
                .mappings()
                .one_or_none()
            )
        if row is None:
            raise ConfigError("AGENT_RAG_NOT_FOUND", 404)
        snapshot, summary = row["config_snapshot"], row["model_summary"] or {}
        return {
            "run_id": run_id,
            "status": row["status"],
            "agent_key": snapshot.get("agent_key", "demo"),
            "scope": snapshot.get("knowledge_scope", []),
            "versions": snapshot.get("knowledge_versions", []),
            "attempts": [
                KnowledgeAttempt.model_validate(item).model_dump(mode="json")
                for item in summary.get("knowledge", [])
            ],
        }

    async def claim(self) -> dict[str, Any] | None:
        async with self.engine.begin() as conn:
            row = (
                (
                    await conn.execute(
                        select(tasks)
                        .where(
                            or_(
                                and_(tasks.c.state == "PENDING", tasks.c.next_attempt <= now()),
                                and_(tasks.c.state == "RUNNING", tasks.c.lease_until < now()),
                            )
                        )
                        .order_by(tasks.c.next_attempt, tasks.c.version_id)
                        .limit(1)
                        .with_for_update(skip_locked=True)
                    )
                )
                .mappings()
                .one_or_none()
            )
            if row is None:
                return None
            token = str(uuid4())
            if row["attempts"] >= 4:
                await conn.execute(
                    update(tasks)
                    .where(tasks.c.version_id == row["version_id"])
                    .values(state="FAILED", error_code="AGENT_RAG_RETRIES_EXHAUSTED")
                )
                return None
            await conn.execute(
                update(tasks)
                .where(tasks.c.version_id == row["version_id"])
                .values(
                    state="RUNNING",
                    lease_token=token,
                    lease_until=now() + timedelta(seconds=60),
                    attempts=row["attempts"] + 1,
                    updated_at=now(),
                )
            )
            value = (
                (
                    await conn.execute(
                        select(versions, documents.c.base_id)
                        .select_from(
                            versions.join(documents, versions.c.document_id == documents.c.id),
                        )
                        .where(versions.c.id == row["version_id"])
                    )
                )
                .mappings()
                .one()
            )
            return dict(value) | {"lease_token": token, "attempts": row["attempts"] + 1}

    async def heartbeat(self, version_id: str, token: str) -> bool:
        async with self.engine.begin() as conn:
            result = await conn.execute(
                update(tasks)
                .where(
                    tasks.c.version_id == version_id,
                    tasks.c.lease_token == token,
                    tasks.c.state == "RUNNING",
                    tasks.c.lease_until > now(),
                )
                .values(lease_until=now() + timedelta(seconds=60), updated_at=now())
            )
            return result.rowcount == 1

    async def finish(self, value: dict[str, Any], count: int, error: str | None) -> bool:
        # 和上传/启停保持相同锁顺序：document → task，避免死锁。
        async with self.engine.begin() as conn:
            doc = (
                (
                    await conn.execute(
                        select(documents)
                        .where(
                            documents.c.id == value["document_id"],
                        )
                        .with_for_update()
                    )
                )
                .mappings()
                .one()
            )
            task = (
                (
                    await conn.execute(
                        select(tasks)
                        .where(
                            tasks.c.version_id == value["id"],
                        )
                        .with_for_update()
                    )
                )
                .mappings()
                .one()
            )
            if task["lease_token"] != value["lease_token"] or task["lease_until"] <= now():
                return False  # 失租 worker 不得发布或覆盖新 worker 结果。
            state = "READY" if error is None else ("FAILED" if task["attempts"] >= 4 else "PENDING")
            await conn.execute(
                update(tasks)
                .where(tasks.c.version_id == value["id"])
                .values(
                    state=state,
                    chunk_count=count,
                    error_code=error,
                    lease_token=None,
                    lease_until=None,
                    next_attempt=now() + timedelta(seconds=2 ** task["attempts"]),
                    updated_at=now(),
                )
            )
            if error is None and doc["current_version"] == value["id"]:
                await conn.execute(
                    update(documents)
                    .where(documents.c.id == value["document_id"])
                    .values(active_version=value["id"])
                )
        return True
