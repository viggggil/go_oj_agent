"""会话、消息和 Run 的事务边界与 owner 隔离。"""

from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, cast
from uuid import UUID, uuid4

from sqlalchemy import Select, and_, insert, select, update
from sqlalchemy.exc import IntegrityError
from sqlalchemy.ext.asyncio import AsyncConnection, AsyncEngine

from app.models.runtime import RunStatus, validate_run_transition
from app.storage.schema import agent_conversations, agent_messages, agent_runs


class StoreError(RuntimeError):
    """存储层稳定错误基类。"""


class ConversationNotFound(StoreError):
    """会话不存在或不属于当前用户。"""


class ActiveRunConflict(StoreError):
    """同一会话已有活动运行。"""


class RunNotFound(StoreError):
    """运行不存在或不属于当前用户。"""


class RunStateConflict(StoreError):
    """运行已经进入终态，不能重复提交终态。"""


@dataclass(frozen=True)
class ConversationRecord:
    id: str
    user_id: int
    title: str | None
    created_at: datetime
    updated_at: datetime


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


_RUN_COLUMNS = tuple(column for column in agent_runs.c if column.name != "active_conversation_id")


def utc_now() -> datetime:
    return datetime.now(UTC).replace(tzinfo=None)


class AgentStore:
    def __init__(self, engine: AsyncEngine, *, clock: Callable[[], datetime] = utc_now) -> None:
        if engine.url.database != "oj_agent" or engine.url.drivername != "mysql+asyncmy":
            raise ValueError("Agent store requires its own oj_agent MySQL schema")
        self._engine = engine
        self._clock = clock

    async def create_conversation(
        self, user_id: int, title: str | None = None
    ) -> ConversationRecord:
        if user_id <= 0 or (title is not None and len(title) > 255):
            raise ValueError("Invalid conversation owner or title")
        conversation_id = str(uuid4())
        now = self._clock()
        async with self._engine.begin() as connection:
            await connection.execute(
                insert(agent_conversations).values(
                    id=conversation_id,
                    user_id=user_id,
                    title=title,
                    created_at=now,
                    updated_at=now,
                )
            )
        return ConversationRecord(conversation_id, user_id, title, now, now)

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

    async def create_run_with_user_message(
        self,
        *,
        user_id: int,
        conversation_id: str | UUID | None,
        request_id: str,
        content: str,
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
            or deadline_at <= now
        ):
            raise ValueError("Invalid run input")
        async with self._engine.begin() as connection:
            if conversation_id is None:
                conversation_id = str(uuid4())
                await connection.execute(
                    insert(agent_conversations).values(
                        id=conversation_id, user_id=user_id, created_at=now, updated_at=now
                    )
                )
            else:
                await self._lock_owned_conversation(connection, user_id, conversation_id)
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
            )

    async def finish_run(
        self,
        *,
        user_id: int,
        run_id: str | UUID,
        status: RunStatus,
        answer: str | None = None,
        error_code: str | None = None,
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
                .values(status=status, finished_at=now, error_code=error_code)
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
            values.update(status=status, finished_at=now, error_code=error_code)
            return RunRecord(**values)

    async def get_run(self, user_id: int, run_id: str | UUID) -> RunRecord:
        async with self._engine.connect() as connection:
            row = (
                (
                    await connection.execute(
                        select(*_RUN_COLUMNS).where(
                            and_(agent_runs.c.id == str(run_id), agent_runs.c.user_id == user_id)
                        )
                    )
                )
                .mappings()
                .first()
            )
        if row is None:
            raise RunNotFound("Run not found")
        return RunRecord(**row)

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
                    )
                )
                .with_for_update()
            )
        ).first()
        if row is None:
            raise ConversationNotFound("Conversation not found")
