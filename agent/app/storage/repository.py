"""会话、消息和 Run 的事务边界与 owner 隔离。"""

from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, Literal, cast
from uuid import UUID, uuid4

from sqlalchemy import Select, and_, func, insert, select, update
from sqlalchemy.exc import IntegrityError
from sqlalchemy.ext.asyncio import AsyncConnection, AsyncEngine

from app.models.memory import ConversationSummary
from app.models.runtime import RunStatus, validate_run_transition
from app.storage.schema import (
    agent_conversation_summaries,
    agent_conversations,
    agent_messages,
    agent_runs,
)


class StoreError(RuntimeError):
    """存储层稳定错误基类。"""


class ConversationNotFound(StoreError):
    """会话不存在或不属于当前用户。"""


class ActiveRunConflict(StoreError):
    """同一会话已有活动运行。"""


class ConversationAgentConflict(StoreError):
    """会话不能在不同 Agent 之间复用。"""


class RunNotFound(StoreError):
    """运行不存在或不属于当前用户。"""


class RunStateConflict(StoreError):
    """运行已经进入终态，不能重复提交终态。"""


class SummaryVersionConflict(StoreError):
    """会话摘要版本或覆盖游标已经被其他更新推进。"""


@dataclass(frozen=True)
class ConversationRecord:
    id: str
    user_id: int
    title: str | None
    agent_key: str | None
    created_at: datetime
    updated_at: datetime
    purpose: str = "chat"


@dataclass(frozen=True)
class MessageRecord:
    id: int
    conversation_id: str
    run_id: str | None
    role: str
    content: str
    tool_name: str | None
    created_at: datetime


@dataclass(frozen=True)
class CompletedTurnRecord:
    run_id: str
    user_message_id: int
    assistant_message_id: int
    user_content: str
    assistant_content: str


@dataclass(frozen=True)
class ConversationSummaryRecord:
    conversation_id: str
    user_id: int
    purpose: str
    summary: ConversationSummary
    covered_until_message_id: int | None
    schema_version: int
    version: int
    created_at: datetime
    updated_at: datetime


@dataclass(frozen=True)
class RunRecord:
    id: str
    conversation_id: str
    user_id: int
    request_id: str
    config_source: str
    config_snapshot: dict[str, Any]
    status: RunStatus
    started_at: datetime
    finished_at: datetime | None
    deadline_at: datetime
    error_code: str | None
    model_summary: dict[str, Any] | None = None
    trace_id: str | None = None


_RUN_COLUMNS = tuple(column for column in agent_runs.c if column.name != "active_conversation_id")


def utc_now() -> datetime:
    return datetime.now(UTC).replace(tzinfo=None)


class AgentStore:
    def __init__(
        self,
        engine: AsyncEngine,
        *,
        clock: Callable[[], datetime] = utc_now,
        purpose: Literal["chat", "eval"] = "chat",
    ) -> None:
        if engine.url.database != "oj_agent" or engine.url.drivername != "mysql+asyncmy":
            raise ValueError("Agent store requires its own oj_agent MySQL schema")
        self._purpose = purpose
        self._engine = engine
        self._clock = clock

    async def create_conversation(
        self, user_id: int, title: str | None = None, *, agent_key: str | None = "demo"
    ) -> ConversationRecord:
        if (
            user_id <= 0
            or (title is not None and len(title) > 255)
            or (agent_key is not None and not agent_key)
        ):
            raise ValueError("Invalid conversation owner or title")
        conversation_id = str(uuid4())
        now = self._clock()
        async with self._engine.begin() as connection:
            await connection.execute(
                insert(agent_conversations).values(
                    id=conversation_id,
                    user_id=user_id,
                    title=title,
                    agent_key=agent_key,
                    purpose=self._purpose,
                    created_at=now,
                    updated_at=now,
                )
            )
        return ConversationRecord(
            conversation_id, user_id, title, agent_key, now, now, self._purpose
        )

    async def get_conversation(
        self, user_id: int, conversation_id: str | UUID
    ) -> ConversationRecord:
        async with self._engine.connect() as connection:
            row = (
                (
                    await connection.execute(
                        select(agent_conversations).where(
                            and_(
                                agent_conversations.c.id == str(conversation_id),
                                agent_conversations.c.user_id == user_id,
                                agent_conversations.c.purpose == self._purpose,
                            )
                        )
                    )
                )
                .mappings()
                .first()
            )
        if row is None:
            raise ConversationNotFound("Conversation not found")
        return ConversationRecord(**row)

    async def list_messages(
        self, user_id: int, conversation_id: str | UUID, *, limit: int = 100
    ) -> list[MessageRecord]:
        conversation = await self.get_conversation(user_id, conversation_id)
        safe_limit = min(max(limit, 1), 200)
        query: Select[Any] = (
            select(agent_messages)
            .where(agent_messages.c.conversation_id == conversation.id)
            .order_by(agent_messages.c.id.desc())
            .limit(safe_limit)
        )
        async with self._engine.connect() as connection:
            rows = (await connection.execute(query)).mappings().all()
        # 取最近的有界历史，再恢复稳定的时间顺序。
        return [MessageRecord(**row) for row in reversed(rows)]

    async def list_conversations(self, user_id: int, page: int) -> dict[str, Any]:
        # 旧会话没有标题；只取首条用户消息的短摘要，不返回配置快照。
        first_message = (
            select(func.left(agent_messages.c.content, 80))
            .where(
                agent_messages.c.conversation_id == agent_conversations.c.id,
                agent_messages.c.role == "user",
            )
            .order_by(agent_messages.c.id)
            .limit(1)
            .scalar_subquery()
        )
        async with self._engine.connect() as connection:
            total = await connection.scalar(
                select(func.count())
                .select_from(agent_conversations)
                .where(
                    agent_conversations.c.user_id == user_id,
                    agent_conversations.c.purpose == self._purpose,
                )
            )
            rows = (
                (
                    await connection.execute(
                        select(
                            agent_conversations.c.id,
                            agent_conversations.c.agent_key,
                            func.coalesce(
                                agent_conversations.c.title, first_message, "新对话"
                            ).label("title"),
                            agent_conversations.c.updated_at,
                        )
                        .where(
                            agent_conversations.c.user_id == user_id,
                            agent_conversations.c.purpose == self._purpose,
                        )
                        .order_by(
                            agent_conversations.c.updated_at.desc(), agent_conversations.c.id.desc()
                        )
                        .offset((page - 1) * 20)
                        .limit(20)
                    )
                )
                .mappings()
                .all()
            )
        return {
            "items": [dict(row) for row in rows],
            "page": {"page": page, "page_size": 20, "total": total or 0},
        }

    async def conversation_messages(
        self, user_id: int, conversation_id: str, before: int | None = None
    ) -> dict[str, Any]:
        conversation = await self.get_conversation(user_id, conversation_id)
        query = (
            select(
                agent_messages.c.id,
                agent_messages.c.role,
                agent_messages.c.content,
                agent_runs.c.status,
                agent_runs.c.error_code,
                agent_runs.c.model_summary,
            )
            .select_from(
                agent_messages.outerjoin(agent_runs, agent_messages.c.run_id == agent_runs.c.id)
            )
            .where(
                agent_messages.c.conversation_id == conversation.id,
                agent_messages.c.role.in_(("user", "assistant")),
            )
            .order_by(agent_messages.c.id.desc())
            .limit(5)
        )
        if before is not None:
            query = query.where(agent_messages.c.id < before)
        async with self._engine.connect() as connection:
            rows = (await connection.execute(query)).mappings().all()
        # 四条完整消息，即使 JSON 转义达到每字符六字节，也小于 Gateway 的 1 MiB 上限。
        items = []
        for row in reversed(rows[:4]):
            item = dict(row)
            summary = item.pop("model_summary", None)
            item["tool_steps"] = []
            if (item["role"] == "assistant" or item["status"] != "COMPLETED") and isinstance(
                summary, dict
            ):
                # 上方会话归属校验同时保护持久化的脱敏工具轨迹。
                for raw in summary.get("tool_steps", [])[:32]:
                    item["tool_steps"].append(
                        {
                            key: raw[key]
                            for key in (
                                "step",
                                "call_id",
                                "tool_name",
                                "status",
                                "duration_ms",
                                "summary",
                            )
                            if key in raw
                        }
                    )
            items.append(item)
        return {
            "conversation": {
                "id": conversation.id,
                "agent_key": conversation.agent_key,
                "title": conversation.title or "历史对话",
            },
            "items": items,
            "next_before": items[0]["id"] if len(rows) > 4 else None,
        }

    async def list_completed_turn_records(
        self,
        user_id: int,
        conversation_id: str | UUID,
        *,
        after_message_id: int | None = None,
        limit: int = 20,
        oldest_first: bool = False,
    ) -> list[CompletedTurnRecord]:
        """返回成功 Run 的完整轮次，并支持按 assistant 消息游标增量读取。"""
        conversation = await self.get_conversation(user_id, conversation_id)
        users = agent_messages.alias("history_user")
        answers = agent_messages.alias("history_assistant")
        query = (
            select(
                agent_runs.c.id.label("run_id"),
                users.c.id.label("user_message_id"),
                answers.c.id.label("assistant_message_id"),
                users.c.content.label("user_content"),
                answers.c.content.label("assistant_content"),
            )
            .select_from(
                agent_runs.join(users, users.c.run_id == agent_runs.c.id).join(
                    answers, answers.c.run_id == agent_runs.c.id
                )
            )
            .where(
                and_(
                    agent_runs.c.user_id == user_id,
                    agent_runs.c.conversation_id == conversation.id,
                    agent_runs.c.status == "COMPLETED",
                    users.c.conversation_id == conversation.id,
                    answers.c.conversation_id == conversation.id,
                    users.c.role == "user",
                    answers.c.role == "assistant",
                    users.c.id < answers.c.id,
                )
            )
        )
        if after_message_id is not None:
            if after_message_id <= 0:
                raise ValueError("Invalid completed turn cursor")
            query = query.where(answers.c.id > after_message_id).order_by(
                users.c.id.asc() if oldest_first else users.c.id.desc()
            )
        else:
            query = query.order_by(users.c.id.desc())
        query = query.limit(min(max(limit, 1), 200))
        async with self._engine.connect() as connection:
            rows = (await connection.execute(query)).mappings().all()
        if after_message_id is None or not oldest_first:
            rows = list(reversed(rows))
        return [CompletedTurnRecord(**row) for row in rows]

    async def list_completed_turns(
        self, user_id: int, conversation_id: str | UUID, *, limit: int = 20
    ) -> list[tuple[str, str]]:
        """兼容旧调用方的文本投影；模型上下文使用带 ID 的完整记录。"""
        # 保持 PR2 旧文本投影的最多 20 轮约束；新游标查询单独允许更大的批次。
        safe_limit = min(max(limit, 1), 20)
        turns = await self.list_completed_turn_records(user_id, conversation_id, limit=safe_limit)
        return [(turn.user_content, turn.assistant_content) for turn in turns]

    async def get_conversation_summary(
        self, user_id: int, conversation_id: str | UUID
    ) -> ConversationSummaryRecord | None:
        """按 owner 和用途读取摘要；不存在摘要表示尚未完成归档。"""
        conversation = await self.get_conversation(user_id, conversation_id)
        async with self._engine.connect() as connection:
            row = (
                (
                    await connection.execute(
                        select(agent_conversation_summaries).where(
                            and_(
                                agent_conversation_summaries.c.conversation_id == conversation.id,
                                agent_conversation_summaries.c.user_id == user_id,
                                agent_conversation_summaries.c.purpose == self._purpose,
                            )
                        )
                    )
                )
                .mappings()
                .first()
            )
        return self._summary_record(row) if row is not None else None

    async def save_conversation_summary(
        self,
        *,
        user_id: int,
        conversation_id: str | UUID,
        summary: ConversationSummary,
        covered_until_message_id: int | None,
        expected_version: int | None,
    ) -> ConversationSummaryRecord:
        """在会话锁内以版本号推进摘要；失败不会改变旧摘要和游标。"""
        if covered_until_message_id is not None and covered_until_message_id <= 0:
            raise ValueError("Invalid summary cursor")
        now = self._clock()
        # DATETIME(3) truncates sub-millisecond precision; return the same value we persist.
        now = now.replace(microsecond=(now.microsecond // 1_000) * 1_000)
        async with self._engine.begin() as connection:
            await self._lock_owned_conversation(connection, user_id, conversation_id)
            existing = (
                (
                    await connection.execute(
                        select(agent_conversation_summaries)
                        .where(
                            and_(
                                agent_conversation_summaries.c.conversation_id
                                == str(conversation_id),
                                agent_conversation_summaries.c.user_id == user_id,
                                agent_conversation_summaries.c.purpose == self._purpose,
                            )
                        )
                        .with_for_update()
                    )
                )
                .mappings()
                .first()
            )
            if existing is None:
                if expected_version not in (None, 0):
                    raise SummaryVersionConflict("Summary version does not match")
                version = 1
                await self._validate_summary_cursor(
                    connection,
                    user_id,
                    str(conversation_id),
                    covered_until_message_id,
                )
                values = {
                    "conversation_id": str(conversation_id),
                    "user_id": user_id,
                    "purpose": self._purpose,
                    "summary_json": summary.model_dump(mode="json"),
                    "covered_until_message_id": covered_until_message_id,
                    "schema_version": summary.schema_version,
                    "version": version,
                    "created_at": now,
                    "updated_at": now,
                }
                await connection.execute(insert(agent_conversation_summaries).values(values))
                return ConversationSummaryRecord(
                    conversation_id=str(conversation_id),
                    user_id=user_id,
                    purpose=self._purpose,
                    summary=summary,
                    covered_until_message_id=covered_until_message_id,
                    schema_version=summary.schema_version,
                    version=version,
                    created_at=now,
                    updated_at=now,
                )

            current_version = int(existing["version"])
            if expected_version != current_version:
                raise SummaryVersionConflict("Summary version does not match")
            current_cursor = existing["covered_until_message_id"]
            if (
                current_cursor is not None
                and covered_until_message_id is not None
                and covered_until_message_id < current_cursor
            ):
                raise SummaryVersionConflict("Summary cursor cannot move backwards")
            await self._validate_summary_cursor(
                connection,
                user_id,
                str(conversation_id),
                covered_until_message_id,
            )
            version = current_version + 1
            await connection.execute(
                update(agent_conversation_summaries)
                .where(
                    and_(
                        agent_conversation_summaries.c.conversation_id == str(conversation_id),
                        agent_conversation_summaries.c.version == current_version,
                    )
                )
                .values(
                    summary_json=summary.model_dump(mode="json"),
                    covered_until_message_id=covered_until_message_id,
                    schema_version=summary.schema_version,
                    version=version,
                    updated_at=now,
                )
            )
            return ConversationSummaryRecord(
                conversation_id=str(conversation_id),
                user_id=user_id,
                purpose=self._purpose,
                summary=summary,
                covered_until_message_id=covered_until_message_id,
                schema_version=summary.schema_version,
                version=version,
                created_at=existing["created_at"],
                updated_at=now,
            )

    async def create_run_with_user_message(
        self,
        *,
        user_id: int,
        conversation_id: str | UUID | None,
        request_id: str,
        trace_id: str | None = None,
        content: str,
        agent_key: str | None = "demo",
        config_source: str,
        config_snapshot: dict[str, Any],
        deadline_at: datetime,
    ) -> RunRecord:
        run_id = str(uuid4())
        now = self._clock()
        if (
            user_id <= 0
            or not content.strip()
            or len(content) > 32_000
            or not 1 <= len(request_id) <= 128
            or not 1 <= len(config_source) <= 64
            or (agent_key is not None and not 1 <= len(agent_key) <= 64)
            or deadline_at <= now
        ):
            raise ValueError("Invalid run input")
        async with self._engine.begin() as connection:
            if conversation_id is None:
                conversation_id = str(uuid4())
                await connection.execute(
                    insert(agent_conversations).values(
                        id=conversation_id,
                        user_id=user_id,
                        agent_key=agent_key,
                        title=content.strip()[:80],
                        purpose=self._purpose,
                        created_at=now,
                        updated_at=now,
                    )
                )
            else:
                await self._lock_owned_conversation(connection, user_id, conversation_id)
                conversation_row = (
                    await connection.execute(
                        select(agent_conversations.c.agent_key).where(
                            agent_conversations.c.id == str(conversation_id)
                        )
                    )
                ).first()
                if conversation_row is not None and (conversation_row[0] or "demo") != agent_key:
                    raise ConversationAgentConflict("Conversation is bound to another Agent")
            active = (
                await connection.execute(
                    select(agent_runs.c.id).where(
                        agent_runs.c.active_conversation_id == str(conversation_id)
                    )
                )
            ).first()
            if active is not None:
                raise ActiveRunConflict("Conversation already has an active run")
            try:
                await connection.execute(
                    insert(agent_runs).values(
                        id=run_id,
                        conversation_id=str(conversation_id),
                        user_id=user_id,
                        request_id=request_id,
                        trace_id=trace_id,
                        config_source=config_source,
                        config_snapshot=config_snapshot,
                        status="RUNNING",
                        started_at=now,
                        deadline_at=deadline_at,
                    )
                )
            except IntegrityError as exc:
                if "uk_agent_runs_one_active_conversation" in str(exc.orig):
                    raise ActiveRunConflict("Conversation already has an active run") from None
                raise
            await connection.execute(
                insert(agent_messages).values(
                    conversation_id=str(conversation_id),
                    run_id=run_id,
                    role="user",
                    content=content,
                    created_at=now,
                )
            )
            await connection.execute(
                update(agent_conversations)
                .where(agent_conversations.c.id == str(conversation_id))
                .values(updated_at=now)
            )
            return RunRecord(
                id=run_id,
                conversation_id=str(conversation_id),
                user_id=user_id,
                request_id=request_id,
                config_source=config_source,
                config_snapshot=config_snapshot,
                status="RUNNING",
                started_at=now,
                finished_at=None,
                deadline_at=deadline_at,
                error_code=None,
                trace_id=trace_id,
            )

    async def finish_run(
        self,
        *,
        user_id: int,
        run_id: str | UUID,
        status: RunStatus,
        answer: str | None = None,
        error_code: str | None = None,
        model_summary: dict[str, Any] | None = None,
    ) -> RunRecord:
        if status == "RUNNING":
            raise RunStateConflict("RUNNING is not a terminal status")
        if status == "COMPLETED" and (not answer or len(answer) > 32_000):
            raise RunStateConflict("Completed run requires a bounded answer")
        if status != "COMPLETED" and answer is not None:
            raise RunStateConflict("Incomplete answers cannot be persisted")
        now = self._clock()
        async with self._engine.begin() as connection:
            # 所有更新按 conversation -> run 的锁顺序，避免与新 Run 相互死锁。
            identity = (
                await connection.execute(
                    select(agent_runs.c.conversation_id).where(
                        and_(agent_runs.c.id == str(run_id), agent_runs.c.user_id == user_id)
                    )
                )
            ).first()
            if identity is None:
                raise RunNotFound("Run not found")
            await self._lock_owned_conversation(connection, user_id, identity[0])
            row = (
                (
                    await connection.execute(
                        select(*_RUN_COLUMNS)
                        .where(
                            and_(agent_runs.c.id == str(run_id), agent_runs.c.user_id == user_id)
                        )
                        .with_for_update()
                    )
                )
                .mappings()
                .first()
            )
            if row is None:
                raise RunNotFound("Run not found")
            current = cast(RunStatus, row["status"])
            if status == "COMPLETED" and row["deadline_at"] <= now:
                raise RunStateConflict("Expired run cannot complete")
            try:
                validate_run_transition(current, status)
            except ValueError as exc:
                raise RunStateConflict(str(exc)) from exc
            await connection.execute(
                update(agent_runs)
                .where(agent_runs.c.id == str(run_id))
                .values(
                    status=status,
                    finished_at=now,
                    error_code=error_code,
                    model_summary=model_summary,
                )
            )
            if status == "COMPLETED":
                await connection.execute(
                    insert(agent_messages).values(
                        conversation_id=row["conversation_id"],
                        run_id=str(run_id),
                        role="assistant",
                        content=answer,
                        created_at=now,
                    )
                )
            await connection.execute(
                update(agent_conversations)
                .where(agent_conversations.c.id == row["conversation_id"])
                .values(updated_at=now)
            )
            values = dict(row)
            values.update(
                status=status, finished_at=now, error_code=error_code, model_summary=model_summary
            )
            return RunRecord(**values)

    async def get_run(self, user_id: int, run_id: str | UUID) -> RunRecord:
        async with self._engine.connect() as connection:
            row = (
                (
                    await connection.execute(
                        select(*_RUN_COLUMNS)
                        .join(
                            agent_conversations,
                            agent_conversations.c.id == agent_runs.c.conversation_id,
                        )
                        .where(
                            agent_runs.c.id == str(run_id),
                            agent_runs.c.user_id == user_id,
                            agent_conversations.c.purpose == self._purpose,
                        )
                    )
                )
                .mappings()
                .first()
            )
        if row is None:
            raise RunNotFound("Run not found")
        return RunRecord(**row)

    async def list_admin_runs(
        self,
        *,
        page: int = 1,
        page_size: int = 20,
        status: RunStatus | None = None,
    ) -> dict[str, Any]:
        """返回仅供管理员使用的 Run 安全投影，不读取消息正文或配置快照。"""
        page = min(max(page, 1), 10000)
        page_size = min(max(page_size, 1), 100)
        predicates = [agent_conversations.c.purpose == self._purpose]
        if status is not None:
            predicates.append(agent_runs.c.status == status)
        async with self._engine.connect() as connection:
            total = int(
                await connection.scalar(
                    select(func.count())
                    .select_from(agent_runs.join(agent_conversations))
                    .where(*predicates)
                )
                or 0
            )
            rows = (
                (
                    await connection.execute(
                        select(
                            agent_runs.c.id,
                            agent_runs.c.trace_id,
                            agent_runs.c.user_id,
                            agent_runs.c.status,
                            agent_runs.c.started_at,
                            agent_runs.c.finished_at,
                            agent_runs.c.error_code,
                            agent_runs.c.model_summary,
                        )
                        .select_from(agent_runs.join(agent_conversations))
                        .where(*predicates)
                        .order_by(agent_runs.c.started_at.desc(), agent_runs.c.id.desc())
                        .offset((page - 1) * page_size)
                        .limit(page_size)
                    )
                )
                .mappings()
                .all()
            )
        return {
            "items": [self._safe_run(dict(row)) for row in rows],
            "page": {"page": page, "page_size": page_size, "total": total},
        }

    async def get_admin_run(self, run_id: str | UUID) -> dict[str, Any]:
        async with self._engine.connect() as connection:
            row = (
                (
                    await connection.execute(
                        select(
                            agent_runs.c.id,
                            agent_runs.c.trace_id,
                            agent_runs.c.user_id,
                            agent_runs.c.status,
                            agent_runs.c.started_at,
                            agent_runs.c.finished_at,
                            agent_runs.c.error_code,
                            agent_runs.c.model_summary,
                        )
                        .select_from(agent_runs.join(agent_conversations))
                        .where(
                            agent_runs.c.id == str(run_id),
                            agent_conversations.c.purpose == self._purpose,
                        )
                    )
                )
                .mappings()
                .first()
            )
        if row is None:
            raise RunNotFound("Run not found")
        return self._safe_run(dict(row))

    @staticmethod
    def _safe_run(row: dict[str, Any]) -> dict[str, Any]:
        started = row["started_at"]
        finished = row["finished_at"]
        duration = None
        if finished is not None:
            duration = max(0, int((finished - started).total_seconds() * 1000))
        raw_summary = row.get("model_summary")
        summary: dict[str, Any] = (
            cast(dict[str, Any], raw_summary) if isinstance(raw_summary, dict) else {}
        )
        return {
            "run_id": row["id"],
            "trace_id": row.get("trace_id"),
            "status": row["status"],
            "purpose": "chat",
            "started_at": started,
            "finished_at": finished,
            "duration_ms": duration,
            "error_code": row.get("error_code"),
            "tool_step_count": len(summary.get("tool_steps", []))
            if isinstance(summary.get("tool_steps"), list)
            else 0,
            "knowledge_query_count": len(summary.get("knowledge", []))
            if isinstance(summary.get("knowledge"), list)
            else 0,
        }

    async def interrupt_running_runs(self) -> int:
        now = self._clock()
        async with self._engine.begin() as connection:
            result = await connection.execute(
                update(agent_runs)
                .where(agent_runs.c.status == "RUNNING")
                .values(status="INTERRUPTED", finished_at=now, error_code="AGENT_RESTARTED")
            )
        return int(getattr(result, "rowcount", 0) or 0)

    async def expire_running_runs(self) -> int:
        now = self._clock()
        async with self._engine.begin() as connection:
            result = await connection.execute(
                update(agent_runs)
                .where(and_(agent_runs.c.status == "RUNNING", agent_runs.c.deadline_at <= now))
                .values(status="INTERRUPTED", finished_at=now, error_code="AGENT_DEADLINE_EXCEEDED")
            )
        return int(getattr(result, "rowcount", 0) or 0)

    @staticmethod
    def _summary_record(row: Any) -> ConversationSummaryRecord:
        try:
            summary = ConversationSummary.model_validate(row["summary_json"])
        except Exception:
            raise StoreError("Invalid conversation summary") from None
        return ConversationSummaryRecord(
            conversation_id=row["conversation_id"],
            user_id=row["user_id"],
            purpose=row["purpose"],
            summary=summary,
            covered_until_message_id=row["covered_until_message_id"],
            schema_version=row["schema_version"],
            version=row["version"],
            created_at=row["created_at"],
            updated_at=row["updated_at"],
        )

    async def _validate_summary_cursor(
        self,
        connection: AsyncConnection,
        user_id: int,
        conversation_id: str,
        cursor: int | None,
    ) -> None:
        if cursor is None:
            return
        users = agent_messages.alias("summary_user")
        answers = agent_messages.alias("summary_assistant")
        row = (
            await connection.execute(
                select(answers.c.id)
                .select_from(
                    agent_runs.join(users, users.c.run_id == agent_runs.c.id).join(
                        answers, answers.c.run_id == agent_runs.c.id
                    )
                )
                .where(
                    and_(
                        agent_runs.c.id == users.c.run_id,
                        agent_runs.c.user_id == user_id,
                        agent_runs.c.conversation_id == conversation_id,
                        agent_runs.c.status == "COMPLETED",
                        users.c.conversation_id == conversation_id,
                        answers.c.conversation_id == conversation_id,
                        users.c.role == "user",
                        answers.c.role == "assistant",
                        answers.c.id == cursor,
                        users.c.id < answers.c.id,
                    )
                )
            )
        ).first()
        if row is None:
            raise ValueError("Summary cursor must reference a completed assistant message")

    async def _lock_owned_conversation(
        self, connection: AsyncConnection, user_id: int, conversation_id: str | UUID
    ) -> None:
        row = (
            await connection.execute(
                select(agent_conversations.c.id)
                .where(
                    and_(
                        agent_conversations.c.id == str(conversation_id),
                        agent_conversations.c.user_id == user_id,
                        agent_conversations.c.purpose == self._purpose,
                    )
                )
                .with_for_update()
            )
        ).first()
        if row is None:
            raise ConversationNotFound("Conversation not found")
