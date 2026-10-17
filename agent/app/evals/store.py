"""评测数据集持久化；只保存校验后的 Case 投影并把原始 JSONL 放入 MinIO。"""

import hashlib
import json
from datetime import UTC, datetime
from typing import Any
from uuid import uuid4

from sqlalchemy import and_, func, insert, select, update
from sqlalchemy import case as sql_case
from sqlalchemy.exc import IntegrityError
from sqlalchemy.ext.asyncio import AsyncEngine

from app.evals.models import (
    EvalDatasetInvalid,
    EvalDatasetUpload,
    EvalModelReviewPage,
    EvalPage,
    EvalResultPage,
    EvalReviewPage,
    EvalReviewWrite,
    EvalRunCreate,
    EvalRunPage,
    parse_jsonl,
)
from app.evals.schema import cases, datasets, model_reviews, results, reviews, runs
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


def run_public(row: dict[str, Any], *, summary: bool = False) -> dict[str, Any]:
    value = public(row)
    value.pop("fingerprint", None)
    value.pop("cancel_request_id", None)
    snapshot = value.get("config_snapshot")
    if isinstance(snapshot, dict):
        value["config_snapshot"] = {
            key: snapshot[key]
            for key in (
                "source",
                "version",
                "agent_key",
                "skill_key",
                "prompt_id",
                "skill_id",
                "provider_id",
                "model_profile_id",
                "config_hash",
                "knowledge_scope",
                "knowledge_versions",
                "budget",
                "execution_mode",
            )
            if key in snapshot
        }
    reviewer = value.pop("reviewer_config_snapshot", None)
    if isinstance(reviewer, dict):
        value["reviewer_snapshot"] = {
            key: reviewer[key]
            for key in (
                "version",
                "agent_key",
                "prompt_id",
                "skill_id",
                "provider_id",
                "model_profile_id",
                "config_hash",
            )
            if key in reviewer
        }
    else:
        value["reviewer_snapshot"] = None
    if summary and isinstance(snapshot, dict):
        value["config_snapshot"].pop("knowledge_versions", None)
        value["config_snapshot"]["knowledge_version_count"] = len(
            snapshot.get("knowledge_versions", [])
        )
    return value


class EvalStore:
    def __init__(self, engine: AsyncEngine, clients: RagClients) -> None:
        self.engine = engine
        self.clients = clients

    @staticmethod
    def clock() -> datetime:
        return now()

    async def upload(self, principal: Principal, body: EvalDatasetUpload) -> dict[str, Any]:
        try:
            parsed = parse_jsonl(body.content)
        except EvalDatasetInvalid:
            raise
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
                await self.clients.remove(key)
                raise ConfigError("AGENT_EVAL_UNAVAILABLE", 503) from None
            await self.clients.remove(key)
            return {"dataset": public(dict(existing)), "duplicate": True}
        except Exception:
            await self.clients.remove(key)
            raise
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

    async def find_request(self, principal: Principal, fingerprint: str) -> dict[str, Any] | None:
        async with self.engine.connect() as conn:
            row = (
                (await conn.execute(select(runs).where(runs.c.request_id == principal.request_id)))
                .mappings()
                .one_or_none()
            )
        if row is None:
            return None
        if row["fingerprint"] != fingerprint or row["created_by"] != principal.user_id:
            raise ConfigError("AGENT_EVAL_REQUEST_CONFLICT", 409)
        return run_public(dict(row))

    async def create_run(
        self,
        body: EvalRunCreate,
        principal: Principal,
        snapshot: dict[str, Any],
        request_id: str,
        fingerprint: str,
        reviewer_snapshot: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        dataset_id = str(body.dataset_id)
        async with self.engine.connect() as conn:
            dataset = (
                (
                    await conn.execute(
                        select(datasets).where(
                            and_(datasets.c.id == dataset_id, datasets.c.status == "READY")
                        )
                    )
                )
                .mappings()
                .one_or_none()
            )
        if dataset is None:
            raise ConfigError("AGENT_EVAL_NOT_FOUND", 404)
        total = int(dataset["case_count"]) * body.repeat_count
        value = {
            "id": str(uuid4()),
            "dataset_id": dataset_id,
            "agent_key": body.agent_key,
            "mode": body.mode,
            "reviewer_agent_key": body.reviewer_agent_key,
            "reviewer_config_snapshot": reviewer_snapshot,
            "repeat_count": body.repeat_count,
            "concurrency": body.concurrency,
            "status": "QUEUED",
            "total_cases": total,
            "completed_cases": 0,
            "hard_failures": 0,
            "error_count": 0,
            "config_snapshot": snapshot,
            "created_by": principal.user_id,
            "request_id": request_id,
            "fingerprint": fingerprint,
            "cancel_request_id": None,
            "cancel_requested": False,
            "created_at": now(),
            "started_at": None,
            "finished_at": None,
            "version": 1,
        }
        try:
            async with self.engine.begin() as conn:
                await conn.execute(insert(runs).values(**value))
        except IntegrityError:
            async with self.engine.connect() as conn:
                existing = (
                    (await conn.execute(select(runs).where(runs.c.request_id == request_id)))
                    .mappings()
                    .one_or_none()
                )
            if existing is None:
                raise ConfigError("AGENT_EVAL_UNAVAILABLE", 503) from None
            if (
                existing["fingerprint"] != fingerprint
                or existing["created_by"] != principal.user_id
            ):
                raise ConfigError("AGENT_EVAL_REQUEST_CONFLICT", 409) from None
            return {"run": run_public(dict(existing)), "duplicate": True}
        return {"run": run_public(value), "duplicate": False}

    async def cases_for_run(self, run_id: str) -> list[dict[str, Any]]:
        async with self.engine.connect() as conn:
            row = (
                (await conn.execute(select(runs).where(runs.c.id == run_id)))
                .mappings()
                .one_or_none()
            )
            if row is None:
                raise ConfigError("AGENT_EVAL_NOT_FOUND", 404)
            values = (
                (
                    await conn.execute(
                        select(cases.c.case_id, cases.c.case_json)
                        .where(cases.c.dataset_id == row["dataset_id"])
                        .order_by(cases.c.case_id)
                    )
                )
                .mappings()
                .all()
            )
        return [{"id": value["case_id"], "case": value["case_json"]} for value in values]

    async def get_run(self, run_id: str) -> dict[str, Any]:
        async with self.engine.connect() as conn:
            row = (
                (await conn.execute(select(runs).where(runs.c.id == run_id)))
                .mappings()
                .one_or_none()
            )
        if row is None:
            raise ConfigError("AGENT_EVAL_NOT_FOUND", 404)
        return run_public(dict(row))

    async def page_runs(self, page: EvalRunPage) -> dict[str, Any]:
        async with self.engine.connect() as conn:
            query = select(runs).order_by(runs.c.created_at.desc(), runs.c.id.desc())
            count = select(func.count()).select_from(runs)
            if page.status:
                query = query.where(runs.c.status == page.status)
                count = count.where(runs.c.status == page.status)
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
            "items": [run_public(dict(row), summary=True) for row in rows],
            "page": {"page": page.page, "page_size": page.page_size, "total": total},
        }

    async def request_cancel(
        self, run_id: str, expected_version: int, request_id: str
    ) -> dict[str, Any]:
        async with self.engine.connect() as conn:
            current = (
                (await conn.execute(select(runs).where(runs.c.id == run_id)))
                .mappings()
                .one_or_none()
            )
        if current is None:
            raise ConfigError("AGENT_EVAL_NOT_FOUND", 404)
        if current["cancel_request_id"] == request_id:
            return run_public(dict(current))
        values: dict[str, Any] = {
            "cancel_requested": True,
            "cancel_request_id": request_id,
            "version": runs.c.version + 1,
        }
        condition = and_(runs.c.id == run_id, runs.c.status.in_(("QUEUED", "RUNNING")))
        condition = and_(condition, runs.c.version == expected_version)
        async with self.engine.begin() as conn:
            result = await conn.execute(update(runs).where(condition).values(**values))
        if result.rowcount != 1:
            async with self.engine.connect() as conn:
                retry = (
                    (await conn.execute(select(runs).where(runs.c.id == run_id)))
                    .mappings()
                    .one_or_none()
                )
            if retry is not None and retry["cancel_request_id"] == request_id:
                return run_public(dict(retry))
            raise ConfigError("AGENT_EVAL_VERSION_CONFLICT", 409)
        return await self.get_run(run_id)

    async def mark_running(self, run_id: str) -> None:
        async with self.engine.begin() as conn:
            await conn.execute(
                update(runs)
                .where(and_(runs.c.id == run_id, runs.c.status == "QUEUED"))
                .values(status="RUNNING", started_at=now(), version=runs.c.version + 1)
            )

    async def recover_orphaned_runs(self) -> int:
        async with self.engine.begin() as conn:
            result = await conn.execute(
                update(runs)
                .where(runs.c.status.in_(("QUEUED", "RUNNING")))
                .values(
                    status="FAILED",
                    finished_at=now(),
                    cancel_requested=True,
                    version=runs.c.version + 1,
                )
            )
        return int(result.rowcount or 0)

    async def recover_orphaned_model_reviews(self) -> int:
        timestamp = now()
        async with self.engine.begin() as conn:
            rows = (
                (
                    await conn.execute(
                        select(model_reviews.c.result_id, model_reviews.c.attempt).where(
                            model_reviews.c.status == "RUNNING"
                        )
                    )
                )
                .mappings()
                .all()
            )
            pending = await conn.execute(
                update(results)
                .where(results.c.model_review_status == "PENDING")
                .values(
                    model_review_status="UNAVAILABLE",
                    model_review_error_code="AGENT_EVAL_REVIEW_INTERRUPTED",
                )
            )
            await conn.execute(
                update(model_reviews)
                .where(model_reviews.c.status == "RUNNING")
                .values(
                    status="UNAVAILABLE",
                    error_code="AGENT_EVAL_REVIEW_INTERRUPTED",
                    finished_at=timestamp,
                )
            )
            for row in rows:
                await conn.execute(
                    update(results)
                    .where(
                        results.c.id == row["result_id"],
                        results.c.model_review_attempt == row["attempt"],
                        results.c.model_review_status == "RUNNING",
                    )
                    .values(
                        model_review_status="UNAVAILABLE",
                        model_review_error_code="AGENT_EVAL_REVIEW_INTERRUPTED",
                    )
                )
        return len(rows) + pending.rowcount

    async def cancel_requested(self, run_id: str) -> bool:
        async with self.engine.connect() as conn:
            return bool(
                await conn.scalar(select(runs.c.cancel_requested).where(runs.c.id == run_id))
            )

    async def add_result(self, run_id: str, value: dict[str, Any]) -> None:
        async with self.engine.begin() as conn:
            # 先锁父 Run，再插入子结果；否则并发外键共享锁升级计数写锁会死锁。
            await conn.execute(select(runs.c.id).where(runs.c.id == run_id).with_for_update())
            await conn.execute(insert(results).values(**value))
            await conn.execute(
                update(runs)
                .where(runs.c.id == run_id)
                .values(
                    completed_cases=runs.c.completed_cases + 1,
                    hard_failures=runs.c.hard_failures
                    + int(value["checks"].get("hard_fail", False)),
                    error_count=runs.c.error_count + int(value["status"] == "FAILED"),
                    version=runs.c.version + 1,
                )
            )

    async def finish_run(self, run_id: str, status: str) -> None:
        async with self.engine.begin() as conn:
            await conn.execute(
                update(runs)
                .where(and_(runs.c.id == run_id, runs.c.status.in_(("QUEUED", "RUNNING"))))
                .values(
                    status=sql_case((runs.c.cancel_requested.is_(True), "CANCELLED"), else_=status),
                    finished_at=now(),
                    version=runs.c.version + 1,
                )
            )

    @staticmethod
    def _effective_pass() -> Any:
        return and_(
            results.c.status == "COMPLETED",
            results.c.checks["hard_fail"].as_boolean().is_(False),
            sql_case(
                (results.c.review_version > 0, results.c.manual_review["pass"].as_boolean()),
                (
                    results.c.model_review_status != "NOT_REQUESTED",
                    and_(
                        results.c.model_review_status == "COMPLETED",
                        results.c.model_review["pass"].as_boolean().is_(True),
                        results.c.model_review["hard_fail"].as_boolean().is_(False),
                    ),
                ),
                else_=results.c.checks["pass"].as_boolean(),
            ).is_(True),
        )

    async def run_metrics(self, run_id: str) -> dict[str, Any]:
        async with self.engine.connect() as conn:
            row = (
                (
                    await conn.execute(
                        select(
                            func.count().label("recorded_cases"),
                            func.sum(
                                sql_case(
                                    (results.c.checks["pass"].as_boolean().is_(True), 1), else_=0
                                )
                            ).label("check_passes"),
                            func.sum(sql_case((self._effective_pass(), 1), else_=0)).label(
                                "effective_passes"
                            ),
                            func.sum(sql_case((results.c.review_version > 0, 1), else_=0)).label(
                                "reviewed_cases"
                            ),
                            func.sum(
                                sql_case((results.c.model_review_status == "COMPLETED", 1), else_=0)
                            ).label("model_reviewed_cases"),
                            func.sum(
                                sql_case(
                                    (results.c.model_review_status == "UNAVAILABLE", 1), else_=0
                                )
                            ).label("model_review_unavailable"),
                            func.sum(
                                sql_case(
                                    (results.c.model_review["pass"].as_boolean().is_(True), 1),
                                    else_=0,
                                )
                            ).label("model_passes"),
                            func.avg(results.c.latency_ms).label("average_latency_ms"),
                        ).where(results.c.run_id == run_id)
                    )
                )
                .mappings()
                .one()
            )
        return {
            key: (int(value) if value is not None else (None if key == "average_latency_ms" else 0))
            for key, value in row.items()
        }

    @staticmethod
    def result_public(row: dict[str, Any], *, detail: bool) -> dict[str, Any]:
        value = public(row)
        manual = value.get("manual_review")
        if value.get("model_review_status") not in (None, "NOT_REQUESTED"):
            semantic_pass = bool(
                value.get("model_review_status") == "COMPLETED"
                and isinstance(value.get("model_review"), dict)
                and value["model_review"].get("pass") is True
                and value["model_review"].get("hard_fail") is False
            )
        else:
            semantic_pass = bool(value["checks"].get("pass", False))
        value["effective_pass"] = bool(
            value["status"] == "COMPLETED"
            and not value["checks"].get("hard_fail", True)
            and (manual["pass"] if isinstance(manual, dict) else semantic_pass)
        )
        if not detail:
            value["answer_preview"] = (value.pop("answer", None) or "")[:512]
            value.pop("model_summary", None)
            value["checks"] = {key: value["checks"][key] for key in ("pass", "hard_fail")}
            value["manual_review"] = {"pass": manual["pass"]} if isinstance(manual, dict) else None
            value["model_review"] = (
                {
                    "pass": value["model_review"].get("pass"),
                    "hard_fail": value["model_review"].get("hard_fail"),
                }
                if isinstance(value.get("model_review"), dict)
                else None
            )
        return value

    async def get_result(self, run_id: str, result_id: str) -> dict[str, Any]:
        async with self.engine.connect() as conn:
            row = (
                (
                    await conn.execute(
                        select(
                            results,
                            cases.c.case_json.label("case"),
                            cases.c.category,
                            cases.c.title,
                        )
                        .join(runs, results.c.run_id == runs.c.id)
                        .join(
                            cases,
                            and_(
                                cases.c.dataset_id == runs.c.dataset_id,
                                cases.c.case_id == results.c.case_id,
                            ),
                        )
                        .where(results.c.run_id == run_id, results.c.id == result_id)
                    )
                )
                .mappings()
                .one_or_none()
            )
        if row is None:
            raise ConfigError("AGENT_EVAL_NOT_FOUND", 404)
        return self.result_public(dict(row), detail=True)

    async def results_page(self, run_id: str, page: EvalResultPage) -> dict[str, Any]:
        await self.get_run(run_id)
        condition = results.c.run_id == run_id
        if page.status:
            condition = and_(condition, results.c.status == page.status)
        if page.error_code:
            condition = and_(condition, results.c.error_code == page.error_code)
        if page.category:
            condition = and_(condition, cases.c.category == page.category)
        if page.search:
            condition = and_(condition, results.c.case_id.contains(page.search, autoescape=True))
        if page.verdict == "pass":
            condition = and_(condition, self._effective_pass())
        elif page.verdict == "fail":
            condition = and_(condition, ~self._effective_pass())
        elif page.verdict == "hard_fail":
            condition = and_(condition, results.c.checks["hard_fail"].as_boolean().is_(True))
        elif page.verdict == "unreviewed":
            condition = and_(condition, results.c.review_version == 0)
        joined = results.join(runs, results.c.run_id == runs.c.id).join(
            cases,
            and_(cases.c.dataset_id == runs.c.dataset_id, cases.c.case_id == results.c.case_id),
        )
        async with self.engine.connect() as conn:
            total = int(
                await conn.scalar(select(func.count()).select_from(joined).where(condition)) or 0
            )
            rows = (
                (
                    await conn.execute(
                        select(results, cases.c.category, cases.c.title)
                        .select_from(joined)
                        .where(condition)
                        .order_by(results.c.case_id, results.c.attempt)
                        .offset((page.page - 1) * page.page_size)
                        .limit(page.page_size)
                    )
                )
                .mappings()
                .all()
            )
        return {
            "items": [self.result_public(dict(row), detail=False) for row in rows],
            "page": {"page": page.page, "page_size": page.page_size, "total": total},
        }

    @staticmethod
    def _review_replay(
        row: dict[str, Any], principal: Principal, fingerprint: str
    ) -> dict[str, Any]:
        if row["reviewed_by"] != principal.user_id or row["fingerprint"] != fingerprint:
            raise ConfigError("AGENT_EVAL_REQUEST_CONFLICT", 409)
        return {"review": row["new_value"], "review_version": row["version"], "duplicate": True}

    async def save_review(
        self, result_id: str, body: EvalReviewWrite, principal: Principal
    ) -> dict[str, Any]:
        fingerprint = hashlib.sha256(
            json.dumps(
                {"result_id": result_id, **body.model_dump(mode="json", by_alias=True)},
                sort_keys=True,
            ).encode()
        ).hexdigest()
        try:
            async with self.engine.begin() as conn:
                current = (
                    (
                        await conn.execute(
                            select(results).where(results.c.id == result_id).with_for_update()
                        )
                    )
                    .mappings()
                    .one_or_none()
                )
                if current is None:
                    raise ConfigError("AGENT_EVAL_NOT_FOUND", 404)
                replay = (
                    (
                        await conn.execute(
                            select(reviews).where(reviews.c.request_id == principal.request_id)
                        )
                    )
                    .mappings()
                    .one_or_none()
                )
                if replay is not None:
                    return self._review_replay(dict(replay), principal, fingerprint)
                if current["review_version"] != body.expected_version:
                    raise ConfigError("AGENT_EVAL_VERSION_CONFLICT", 409)
                if body.passed and (
                    current["status"] != "COMPLETED" or current["checks"].get("hard_fail", True)
                ):
                    raise ConfigError("AGENT_EVAL_HARD_FAILURE", 409)
                version = body.expected_version + 1
                timestamp = now()
                value = {
                    **body.model_dump(
                        mode="json", by_alias=True, exclude={"expected_version", "reason"}
                    ),
                    "reviewed_by": principal.user_id,
                    "reviewed_at": timestamp.isoformat() + "Z",
                }
                await conn.execute(
                    update(results)
                    .where(results.c.id == result_id)
                    .values(manual_review=value, review_version=version)
                )
                await conn.execute(
                    insert(reviews).values(
                        id=str(uuid4()),
                        result_id=result_id,
                        version=version,
                        reviewed_by=principal.user_id,
                        request_id=principal.request_id,
                        fingerprint=fingerprint,
                        old_value=current["manual_review"],
                        new_value=value,
                        reason=body.reason,
                        created_at=timestamp,
                    )
                )
        except IntegrityError:
            async with self.engine.connect() as conn:
                replay = (
                    (
                        await conn.execute(
                            select(reviews).where(reviews.c.request_id == principal.request_id)
                        )
                    )
                    .mappings()
                    .one_or_none()
                )
            if replay is None:
                raise ConfigError("AGENT_EVAL_UNAVAILABLE", 503) from None
            return self._review_replay(dict(replay), principal, fingerprint)
        return {"review": value, "review_version": version, "duplicate": False}

    async def reviews_page(self, result_id: str, page: EvalReviewPage) -> dict[str, Any]:
        async with self.engine.connect() as conn:
            if await conn.scalar(select(results.c.id).where(results.c.id == result_id)) is None:
                raise ConfigError("AGENT_EVAL_NOT_FOUND", 404)
            total = int(
                await conn.scalar(
                    select(func.count())
                    .select_from(reviews)
                    .where(reviews.c.result_id == result_id)
                )
                or 0
            )
            rows = (
                (
                    await conn.execute(
                        select(reviews)
                        .where(reviews.c.result_id == result_id)
                        .order_by(reviews.c.version.desc())
                        .offset((page.page - 1) * page.page_size)
                        .limit(page.page_size)
                    )
                )
                .mappings()
                .all()
            )
        return {
            "items": [
                public(
                    {
                        key: value
                        for key, value in row.items()
                        if key not in {"fingerprint", "request_id"}
                    }
                )
                for row in rows
            ],
            "page": {"page": page.page, "page_size": page.page_size, "total": total},
        }

    @staticmethod
    def _model_attempt_public(value: dict[str, Any]) -> dict[str, Any]:
        return public(
            {key: item for key, item in value.items() if key not in {"fingerprint", "request_id"}}
        )

    async def begin_model_review(
        self,
        result_id: str,
        principal: Principal,
        expected_attempt: int,
        *,
        retry: bool,
    ) -> dict[str, Any]:
        fingerprint = hashlib.sha256(
            json.dumps(
                {"result_id": result_id, "expected_attempt": expected_attempt, "retry": retry},
                sort_keys=True,
            ).encode()
        ).hexdigest()
        try:
            async with self.engine.begin() as conn:
                current = (
                    (
                        await conn.execute(
                            select(results, runs.c.mode, runs.c.reviewer_agent_key)
                            .join(runs, runs.c.id == results.c.run_id)
                            .where(results.c.id == result_id)
                            .with_for_update(of=results)
                        )
                    )
                    .mappings()
                    .one_or_none()
                )
                if current is None:
                    raise ConfigError("AGENT_EVAL_NOT_FOUND", 404)
                replay = (
                    (
                        await conn.execute(
                            select(model_reviews).where(
                                model_reviews.c.request_id == principal.request_id
                            )
                        )
                    )
                    .mappings()
                    .one_or_none()
                )
                if replay is not None:
                    if (
                        replay["requested_by"] != principal.user_id
                        or replay["fingerprint"] != fingerprint
                    ):
                        raise ConfigError("AGENT_EVAL_REQUEST_CONFLICT", 409)
                    return {"attempt": self._model_attempt_public(dict(replay)), "duplicate": True}
                if (
                    current["mode"] != "programmatic_review"
                    or current["reviewer_agent_key"] != "eval_reviewer"
                ):
                    raise ConfigError("AGENT_EVAL_REVIEW_NOT_ENABLED", 409)
                if current["status"] != "COMPLETED" or current["checks"].get("hard_fail", True):
                    raise ConfigError("AGENT_EVAL_REVIEW_NOT_READY", 409)
                attempt = int(current["model_review_attempt"])
                if retry:
                    if (
                        attempt != expected_attempt
                        or current["model_review_status"] != "UNAVAILABLE"
                    ):
                        raise ConfigError("AGENT_EVAL_VERSION_CONFLICT", 409)
                elif attempt != 0 or expected_attempt != 0:
                    raise ConfigError("AGENT_EVAL_VERSION_CONFLICT", 409)
                timestamp = now()
                value = {
                    "id": str(uuid4()),
                    "result_id": result_id,
                    "attempt": attempt + 1,
                    "requested_by": principal.user_id,
                    "request_id": principal.request_id,
                    "fingerprint": fingerprint,
                    "status": "RUNNING",
                    "review": None,
                    "error_code": None,
                    "created_at": timestamp,
                    "finished_at": None,
                }
                await conn.execute(insert(model_reviews).values(**value))
                await conn.execute(
                    update(results)
                    .where(results.c.id == result_id)
                    .values(
                        model_review_status="RUNNING",
                        model_review_attempt=value["attempt"],
                        model_review=None,
                        model_review_error_code=None,
                    )
                )
        except IntegrityError:
            async with self.engine.connect() as conn:
                replay = (
                    (
                        await conn.execute(
                            select(model_reviews).where(
                                model_reviews.c.request_id == principal.request_id
                            )
                        )
                    )
                    .mappings()
                    .one_or_none()
                )
            if replay is None:
                raise ConfigError("AGENT_EVAL_VERSION_CONFLICT", 409) from None
            if replay["requested_by"] != principal.user_id or replay["fingerprint"] != fingerprint:
                raise ConfigError("AGENT_EVAL_REQUEST_CONFLICT", 409) from None
            return {"attempt": self._model_attempt_public(dict(replay)), "duplicate": True}
        return {"attempt": self._model_attempt_public(value), "duplicate": False}

    async def finish_model_review(
        self,
        result_id: str,
        attempt: int,
        review: dict[str, Any] | None,
        error_code: str | None,
    ) -> None:
        status = "COMPLETED" if review is not None else "UNAVAILABLE"
        timestamp = now()
        async with self.engine.begin() as conn:
            changed = await conn.execute(
                update(results)
                .where(
                    results.c.id == result_id,
                    results.c.model_review_attempt == attempt,
                    results.c.model_review_status == "RUNNING",
                )
                .values(
                    model_review_status=status,
                    model_review=review,
                    model_review_error_code=error_code,
                )
            )
            if changed.rowcount != 1:
                return
            await conn.execute(
                update(model_reviews)
                .where(model_reviews.c.result_id == result_id, model_reviews.c.attempt == attempt)
                .values(status=status, review=review, error_code=error_code, finished_at=timestamp)
            )

    async def review_context(self, result_id: str) -> dict[str, Any]:
        async with self.engine.connect() as conn:
            row = (
                (
                    await conn.execute(
                        select(
                            results,
                            cases.c.case_json.label("case"),
                            runs.c.reviewer_config_snapshot,
                            runs.c.mode,
                        )
                        .join(runs, runs.c.id == results.c.run_id)
                        .join(
                            cases,
                            and_(
                                cases.c.dataset_id == runs.c.dataset_id,
                                cases.c.case_id == results.c.case_id,
                            ),
                        )
                        .where(results.c.id == result_id)
                    )
                )
                .mappings()
                .one_or_none()
            )
        if row is None:
            raise ConfigError("AGENT_EVAL_NOT_FOUND", 404)
        return dict(row)

    async def model_reviews_page(self, result_id: str, page: EvalModelReviewPage) -> dict[str, Any]:
        async with self.engine.connect() as conn:
            if await conn.scalar(select(results.c.id).where(results.c.id == result_id)) is None:
                raise ConfigError("AGENT_EVAL_NOT_FOUND", 404)
            total = int(
                await conn.scalar(
                    select(func.count())
                    .select_from(model_reviews)
                    .where(model_reviews.c.result_id == result_id)
                )
                or 0
            )
            rows = (
                (
                    await conn.execute(
                        select(model_reviews)
                        .where(model_reviews.c.result_id == result_id)
                        .order_by(model_reviews.c.attempt.desc())
                        .offset((page.page - 1) * page.page_size)
                        .limit(page.page_size)
                    )
                )
                .mappings()
                .all()
            )
        return {
            "items": [self._model_attempt_public(dict(row)) for row in rows],
            "page": {"page": page.page, "page_size": page.page_size, "total": total},
        }
