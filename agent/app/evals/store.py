"""评测数据集持久化；只保存校验后的 Case 投影并把原始 JSONL 放入 MinIO。"""

import hashlib
from datetime import UTC, datetime
from typing import Any
from uuid import uuid4

from sqlalchemy import func, insert, select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.ext.asyncio import AsyncEngine

from app.evals.models import EvalDatasetUpload, EvalPage, parse_jsonl
from app.evals.schema import cases, datasets
from app.models.configuration import ConfigError
from app.models.runtime import Principal
from app.rag.clients import RagClients


def now() -> datetime:
    return datetime.now(UTC).replace(tzinfo=None)


def public(row: dict[str, Any]) -> dict[str, Any]:
    return {
        key: (value.isoformat() + "Z" if isinstance(value, datetime) else value)
        for key, value in row.items()
        if key != "object_key"
    }


class EvalStore:
    def __init__(self, engine: AsyncEngine, clients: RagClients) -> None:
        self.engine = engine
        self.clients = clients

    async def upload(self, principal: Principal, body: EvalDatasetUpload) -> dict[str, Any]:
        try:
            parsed = parse_jsonl(body.content)
        except ValueError as exc:
            raise ConfigError("AGENT_EVAL_DATASET_INVALID") from exc
        encoded = body.content.encode("utf-8")
        digest = hashlib.sha256(encoded).hexdigest()
        async with self.engine.connect() as conn:
            existing = (
                (await conn.execute(select(datasets).where(datasets.c.content_hash == digest)))
                .mappings()
                .one_or_none()
            )
        if existing is not None:
            return {"dataset": public(dict(existing)), "duplicate": True}
        dataset_id = str(uuid4())
        key = f"eval-datasets/{dataset_id}/{digest}.jsonl"
        await self.clients.put(key, encoded)
        value = {
            "id": dataset_id,
            "name": body.name,
            "description": body.description,
            "format_version": "v1",
            "content_hash": digest,
            "object_key": key,
            "case_count": len(parsed),
            "created_by": principal.user_id,
            "created_at": now(),
            "status": "READY",
        }
        try:
            async with self.engine.begin() as conn:
                await conn.execute(insert(datasets).values(**value))
                await conn.execute(
                    insert(cases),
                    [
                        {
                            "dataset_id": dataset_id,
                            "case_id": case.id,
                            "category": case.category,
                            "title": case.title,
                            "case_json": case.model_dump(mode="json"),
                            "created_at": value["created_at"],
                        }
                        for case in parsed
                    ],
                )
        except IntegrityError:
            async with self.engine.connect() as conn:
                existing = (
                    (await conn.execute(select(datasets).where(datasets.c.content_hash == digest)))
                    .mappings()
                    .one_or_none()
                )
            if existing is None:
                raise ConfigError("AGENT_EVAL_UNAVAILABLE", 503) from None
            return {"dataset": public(dict(existing)), "duplicate": True}
        return {"dataset": public(value), "duplicate": False}

    async def page(self, page: EvalPage) -> dict[str, Any]:
        async with self.engine.connect() as conn:
            query = select(datasets).order_by(datasets.c.created_at.desc(), datasets.c.id.desc())
            count = select(func.count()).select_from(datasets)
            if page.search:
                query = query.where(datasets.c.name.like(f"%{page.search}%"))
                count = count.where(datasets.c.name.like(f"%{page.search}%"))
            total = int((await conn.execute(count)).scalar_one())
            rows = (
                (
                    await conn.execute(
                        query.offset((page.page - 1) * page.page_size).limit(page.page_size)
                    )
                )
                .mappings()
                .all()
            )
        return {
            "items": [public(dict(row)) for row in rows],
            "page": {"page": page.page, "page_size": page.page_size, "total": total},
        }

    async def detail(self, dataset_id: str) -> dict[str, Any]:
        async with self.engine.connect() as conn:
            row = (
                (await conn.execute(select(datasets).where(datasets.c.id == dataset_id)))
                .mappings()
                .one_or_none()
            )
            if row is None:
                raise ConfigError("AGENT_EVAL_NOT_FOUND", 404)
        return {"dataset": public(dict(row))}

    async def cases_page(self, dataset_id: str, page: EvalPage) -> dict[str, Any]:
        async with self.engine.connect() as conn:
            exists = await conn.execute(select(datasets.c.id).where(datasets.c.id == dataset_id))
            if exists.scalar_one_or_none() is None:
                raise ConfigError("AGENT_EVAL_NOT_FOUND", 404)
            count = select(func.count()).select_from(cases).where(cases.c.dataset_id == dataset_id)
            query = (
                select(cases)
                .where(cases.c.dataset_id == dataset_id)
                .order_by(cases.c.case_id)
                .offset((page.page - 1) * page.page_size)
                .limit(page.page_size)
            )
            total = int((await conn.execute(count)).scalar_one())
            rows = (await conn.execute(query)).mappings().all()
        return {
            "items": [
                {
                    "id": item["case_id"],
                    "category": item["category"],
                    "title": item["title"],
                    "case": item["case_json"],
                }
                for item in rows
            ],
            "page": {"page": page.page, "page_size": page.page_size, "total": total},
        }
