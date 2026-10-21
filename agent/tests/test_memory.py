"""Conversation memory worker behavior tests."""

from typing import Any
from uuid import UUID, uuid4

from app.graphs.runtime import FakeRuntime
from app.graphs.service import RunService
from app.memory.generator import IncrementalSummaryGenerator
from app.memory.worker import ConversationMemoryWorker
from app.models.memory import ConversationSummary
from app.models.runtime import ChatRequest
from app.storage.repository import (
    CompletedTurnRecord,
    ConversationSummaryRecord,
    utc_now,
)

from .test_runtime import Reader, state


def turn(index: int) -> CompletedTurnRecord:
    return CompletedTurnRecord(
        run_id=str(uuid4()),
        user_message_id=index * 2 - 1,
        assistant_message_id=index * 2,
        user_content=f"问题 {index}",
        assistant_content=f"回答 {index}",
    )


async def test_incremental_generator_compacts_old_context_and_keeps_recent_turns() -> None:
    generator = IncrementalSummaryGenerator()
    first = await generator.generate(None, [turn(index) for index in range(1, 11)], {})
    updated = await generator.generate(first, [turn(index) for index in range(11, 21)], {})

    assert updated.goal == "问题 20"
    assert len(updated.context_notes) <= 9
    assert sum(map(len, updated.context_notes)) <= 6_000
    assert updated.context_notes[0].startswith("较早对话摘要：")
    assert "问题 20" in updated.context_notes[-1]
    assert updated.attempted_actions == ()


async def test_incremental_generator_without_new_turns_returns_previous_summary() -> None:
    generator = IncrementalSummaryGenerator()
    previous = await generator.generate(None, [turn(1)], {})

    assert await generator.generate(previous, [], {}) is previous


class MemoryWorkerStore:
    def __init__(self, turns: list[CompletedTurnRecord]) -> None:
        self.turns = turns
        self.summary: ConversationSummaryRecord | None = None
        self.saves = 0
        self.fail_saves = 0

    async def get_conversation_summary(
        self, user_id: int, conversation_id: str | UUID
    ) -> ConversationSummaryRecord | None:
        del user_id, conversation_id
        return self.summary

    async def list_completed_turn_records(
        self,
        user_id: int,
        conversation_id: str | UUID,
        *,
        after_message_id: int | None = None,
        limit: int = 20,
        oldest_first: bool = False,
    ) -> list[CompletedTurnRecord]:
        del user_id, conversation_id
        rows = [
            item
            for item in self.turns
            if after_message_id is None or item.assistant_message_id > after_message_id
        ]
        if not oldest_first:
            rows.reverse()
        return rows[:limit]

    async def save_conversation_summary(
        self,
        *,
        user_id: int,
        conversation_id: str | UUID,
        summary: ConversationSummary,
        covered_until_message_id: int | None,
        expected_version: int | None,
    ) -> ConversationSummaryRecord:
        del user_id, conversation_id
        if self.fail_saves:
            self.fail_saves -= 1
            raise RuntimeError("temporary database failure")
        current_version = self.summary.version if self.summary is not None else None
        assert expected_version == current_version
        self.saves += 1
        now = utc_now()
        self.summary = ConversationSummaryRecord(
            conversation_id="conversation",
            user_id=7,
            purpose="chat",
            summary=summary,
            covered_until_message_id=covered_until_message_id,
            schema_version=summary.schema_version,
            version=(current_version or 0) + 1,
            created_at=self.summary.created_at if self.summary else now,
            updated_at=now,
        )
        return self.summary


async def test_worker_keeps_recent_turns_and_coalesces_notifications() -> None:
    store = MemoryWorkerStore([turn(index) for index in range(1, 9)])
    worker = ConversationMemoryWorker(store, recent_turns=2, batch_size=3, retry_delay_seconds=0)
    assert worker.notify_completed(user_id=7, conversation_id="conversation")
    assert worker.notify_completed(user_id=7, conversation_id="conversation")
    assert worker.pending_count == 1
    assert await worker.drain_once()
    assert store.summary is not None
    assert store.summary.covered_until_message_id == 12
    assert store.saves == 2
    assert "问题 6" in store.summary.summary.context_notes[-1]


async def test_worker_retries_transient_generation_persistence_failure() -> None:
    store = MemoryWorkerStore([turn(index) for index in range(1, 4)])
    store.fail_saves = 2
    worker = ConversationMemoryWorker(store, recent_turns=1, max_retries=3, retry_delay_seconds=0)
    worker.notify_completed(user_id=7, conversation_id="conversation")
    assert await worker.drain_once()
    assert store.summary is not None
    assert store.saves == 1


async def test_worker_failure_does_not_raise_into_chat() -> None:
    class BrokenGenerator:
        async def generate(self, previous: Any, turns: Any, config_snapshot: Any) -> Any:
            raise RuntimeError("summary provider unavailable")

    store = MemoryWorkerStore([turn(index) for index in range(1, 4)])
    worker = ConversationMemoryWorker(
        store,
        generator=BrokenGenerator(),
        recent_turns=1,
        max_retries=2,
        retry_delay_seconds=0,
    )
    worker.notify_completed(user_id=7, conversation_id="conversation")
    assert await worker.drain_once()
    assert store.summary is None


async def test_worker_close_drains_queued_jobs() -> None:
    store = MemoryWorkerStore([turn(index) for index in range(1, 3)])
    worker = ConversationMemoryWorker(store, recent_turns=1, retry_delay_seconds=0)
    await worker.start()
    assert worker.notify_completed(user_id=7, conversation_id="conversation")
    await worker.close()
    assert worker.pending_count == 0
    assert worker._task is None  # noqa: SLF001 - lifecycle assertion


async def test_worker_retries_queue_overflow_after_space_is_available() -> None:
    store = MemoryWorkerStore([turn(index) for index in range(1, 3)])
    worker = ConversationMemoryWorker(store, queue_size=1, recent_turns=1, retry_delay_seconds=0)
    assert worker.notify_completed(user_id=7, conversation_id="first")
    assert not worker.notify_completed(user_id=7, conversation_id="second")
    await worker.start()
    await worker.close()
    assert worker.pending_count == 0


async def test_worker_rescans_when_completion_arrives_during_final_read() -> None:
    class LateCompletionStore(MemoryWorkerStore):
        worker: ConversationMemoryWorker | None = None
        injected = False

        async def list_completed_turn_records(
            self, *args: Any, **kwargs: Any
        ) -> list[CompletedTurnRecord]:
            rows = await super().list_completed_turn_records(*args, **kwargs)
            if kwargs.get("after_message_id") == 2 and len(rows) == 1 and not self.injected:
                self.injected = True
                self.turns.extend([turn(3), turn(4)])
                assert self.worker is not None
                assert self.worker.notify_completed(user_id=7, conversation_id="conversation")
            return rows

    store = LateCompletionStore([turn(1), turn(2)])
    worker = ConversationMemoryWorker(store, recent_turns=1, retry_delay_seconds=0)
    store.worker = worker
    assert worker.notify_completed(user_id=7, conversation_id="conversation")
    assert await worker.drain_once()
    assert worker.pending_count == 1
    assert await worker.drain_once()
    assert worker.pending_count == 0
    assert store.summary is not None
    assert store.summary.covered_until_message_id == 6
    assert "问题 3" in "\n".join(store.summary.summary.context_notes)


async def test_run_service_notifies_after_completed_persistence() -> None:
    class Trigger:
        def __init__(self, store: Any) -> None:
            self.store = store
            self.calls: list[tuple[int, str, str]] = []

        def notify_completed(self, *, user_id: int, conversation_id: str) -> bool:
            assert self.store.record is not None
            assert self.store.record.status == "COMPLETED"
            self.calls.append((user_id, conversation_id, self.store.answers[-1]))
            return True

    from .test_runtime import MemoryStore

    store = MemoryStore()
    trigger = Trigger(store)
    service = RunService(store, FakeRuntime("完成"), Reader(), memory_trigger=trigger)
    accepted = await service.accept(ChatRequest(message="你好"), state().principal)
    async with accepted:
        events = [event async for event in accepted.events]
    assert events[-1].type == "done"
    assert len(trigger.calls) == 1


async def test_memory_enqueue_failure_does_not_change_completed_response() -> None:
    from .test_runtime import MemoryStore

    class BrokenTrigger:
        def notify_completed(self, *, user_id: int, conversation_id: str) -> bool:
            del user_id, conversation_id
            raise RuntimeError("queue unavailable")

    store = MemoryStore()
    service = RunService(store, FakeRuntime("完成"), Reader(), memory_trigger=BrokenTrigger())
    accepted = await service.accept(ChatRequest(message="你好"), state().principal)
    async with accepted:
        events = [event async for event in accepted.events]
    assert [event.type for event in events][-1] == "done"
    assert store.record is not None and store.record.status == "COMPLETED"
