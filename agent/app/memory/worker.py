"""Non-blocking background persistence for conversation summaries."""

import asyncio
import logging
from dataclasses import dataclass
from typing import Protocol
from uuid import UUID

from app.memory.generator import IncrementalSummaryGenerator, SummaryGenerator
from app.models.memory import ConversationSummary
from app.storage.repository import (
    CompletedTurnRecord,
    ConversationSummaryRecord,
    SummaryVersionConflict,
)

logger = logging.getLogger(__name__)


class MemoryTrigger(Protocol):
    def notify_completed(self, *, user_id: int, conversation_id: str) -> bool: ...


class MemoryStore(Protocol):
    async def get_conversation_summary(
        self, user_id: int, conversation_id: str | UUID
    ) -> ConversationSummaryRecord | None: ...

    async def list_completed_turn_records(
        self,
        user_id: int,
        conversation_id: str | UUID,
        *,
        after_message_id: int | None = None,
        limit: int = 20,
        oldest_first: bool = False,
    ) -> list[CompletedTurnRecord]: ...

    async def save_conversation_summary(
        self,
        *,
        user_id: int,
        conversation_id: str | UUID,
        summary: ConversationSummary,
        covered_until_message_id: int | None,
        expected_version: int | None,
    ) -> ConversationSummaryRecord: ...


@dataclass(frozen=True)
class MemoryJob:
    user_id: int
    conversation_id: str


class ConversationMemoryWorker:
    """Coalesce completed runs and archive old turns in bounded batches."""

    def __init__(
        self,
        store: MemoryStore,
        *,
        generator: SummaryGenerator | None = None,
        queue_size: int = 256,
        recent_turns: int = 6,
        batch_size: int = 6,
        max_retries: int = 3,
        retry_delay_seconds: float = 0.05,
        close_timeout_seconds: float = 5,
    ) -> None:
        if queue_size < 1 or recent_turns < 1 or batch_size < 1 or max_retries < 1:
            raise ValueError("Memory worker limits must be positive")
        if retry_delay_seconds < 0 or close_timeout_seconds <= 0:
            raise ValueError("Memory worker delays must be valid")
        self._store = store
        self._generator = generator or IncrementalSummaryGenerator()
        self._queue: asyncio.Queue[MemoryJob] = asyncio.Queue(maxsize=queue_size)
        self._pending: set[tuple[int, str]] = set()
        self._rerun: set[tuple[int, str]] = set()
        self._overflow: set[tuple[int, str]] = set()
        self._recent_turns = recent_turns
        self._batch_size = batch_size
        self._max_retries = max_retries
        self._retry_delay = retry_delay_seconds
        self._close_timeout = close_timeout_seconds
        self._task: asyncio.Task[None] | None = None
        self._stopping = False

    @property
    def pending_count(self) -> int:
        return len(self._pending)

    async def start(self) -> None:
        if self._task is not None and not self._task.done():
            return
        if self._stopping:
            raise RuntimeError("Memory worker is stopping")
        self._task = asyncio.create_task(self._run(), name="agent-conversation-memory")

    def notify_completed(self, *, user_id: int, conversation_id: str) -> bool:
        """Enqueue quickly; never waits for summary generation or the database."""
        if self._stopping or user_id <= 0 or not conversation_id:
            return False
        key = (user_id, conversation_id)
        if key in self._pending:
            # The active/queued job may have observed a stale database snapshot.
            # Keep one follow-up scan so a completion during processing is not lost.
            self._rerun.add(key)
            return True
        if key in self._overflow:
            return True
        self._pending.add(key)
        try:
            self._queue.put_nowait(MemoryJob(user_id, conversation_id))
        except asyncio.QueueFull:
            self._overflow.add(key)
            logger.warning("Conversation memory queue is full")
            return False
        return True

    async def drain_once(self) -> bool:
        """Process one queued conversation; useful for deterministic tests."""
        if self._queue.empty():
            return False
        job = await self._queue.get()
        try:
            await self._process_with_retry(job)
        finally:
            self._finish_job(job)
            self._queue.task_done()
        return True

    async def close(self) -> None:
        self._stopping = True
        task = self._task
        if task is None:
            return
        try:
            await asyncio.wait_for(self._queue.join(), timeout=self._close_timeout)
            await asyncio.wait_for(task, timeout=self._close_timeout)
        except TimeoutError:
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)
        except asyncio.CancelledError:
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)
            raise
        finally:
            self._task = None

    async def _run(self) -> None:
        while not self._stopping or not self._queue.empty() or self._overflow:
            self._promote_overflow()
            try:
                job = await asyncio.wait_for(self._queue.get(), timeout=0.2)
            except TimeoutError:
                continue
            try:
                await self._process_with_retry(job)
            finally:
                self._finish_job(job)
                self._queue.task_done()

    def _finish_job(self, job: MemoryJob) -> None:
        key = (job.user_id, job.conversation_id)
        if key not in self._rerun:
            self._pending.discard(key)
            return

        self._rerun.discard(key)
        try:
            self._queue.put_nowait(job)
        except asyncio.QueueFull:
            self._overflow.add(key)

    def _promote_overflow(self) -> None:
        while self._overflow and not self._queue.full():
            user_id, conversation_id = self._overflow.pop()
            self._queue.put_nowait(MemoryJob(user_id, conversation_id))

    async def _process_with_retry(self, job: MemoryJob) -> None:
        for attempt in range(self._max_retries):
            try:
                await self._process(job)
                return
            except Exception as exc:
                if attempt + 1 >= self._max_retries:
                    logger.warning(
                        "Conversation memory update failed",
                        extra={"error_type": type(exc).__name__},
                    )
                    return
                await asyncio.sleep(self._retry_delay * (2**attempt))

    async def _process(self, job: MemoryJob) -> None:
        while True:
            record = await self._store.get_conversation_summary(job.user_id, job.conversation_id)
            turns = await self._store.list_completed_turn_records(
                job.user_id,
                job.conversation_id,
                after_message_id=record.covered_until_message_id if record else None,
                limit=self._batch_size + self._recent_turns,
                oldest_first=True,
            )
            archive = turns[: -self._recent_turns]
            if not archive:
                return
            batch = archive[: self._batch_size]
            summary = await self._generator.generate(
                record.summary if record else None,
                batch,
                {},
            )
            try:
                await self._store.save_conversation_summary(
                    user_id=job.user_id,
                    conversation_id=job.conversation_id,
                    summary=summary,
                    covered_until_message_id=batch[-1].assistant_message_id,
                    expected_version=record.version if record else None,
                )
            except SummaryVersionConflict:
                raise
