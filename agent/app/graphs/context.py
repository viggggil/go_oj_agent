"""会话记忆装配；负责去重和有界读取，不参与 ReAct 决策。"""

from collections.abc import Callable
from dataclasses import dataclass
from typing import Protocol
from uuid import UUID

from app.memory.generator import IncrementalSummaryGenerator, bounded_summary
from app.models.memory import ConversationSummary
from app.models.runtime import AgentState, HistoryMessage
from app.storage.repository import CompletedTurnRecord, ConversationSummaryRecord


@dataclass(frozen=True)
class ContextAssembly:
    summary: ConversationSummary | None
    turns: tuple[CompletedTurnRecord, ...]

    @property
    def history(self) -> tuple[HistoryMessage, ...]:
        messages: list[HistoryMessage] = []
        for turn in self.turns:
            messages.extend(
                (
                    HistoryMessage(role="user", content=turn.user_content),
                    HistoryMessage(role="assistant", content=turn.assistant_content),
                )
            )
        return tuple(messages)


class ContextStore(Protocol):
    async def list_completed_turn_records(
        self,
        user_id: int,
        conversation_id: str | UUID,
        *,
        after_message_id: int | None = None,
        limit: int = 20,
        oldest_first: bool = False,
    ) -> list[CompletedTurnRecord]: ...


class ContextAssembler:
    """保留摘要之后的最近完整轮次，避免摘要与原文重复。"""

    _PAGE_SIZE = 24

    def __init__(
        self,
        *,
        recent_turns: int = 6,
        max_history_chars: int = 64_000,
        history_selector: Callable[[AgentState], tuple[HistoryMessage, ...]] | None = None,
    ) -> None:
        if recent_turns < 1 or max_history_chars < 1:
            raise ValueError("Context limits must be positive")
        self._recent_turns = recent_turns
        self._max_history_chars = max_history_chars
        self._history_selector = history_selector
        self._gap_generator = IncrementalSummaryGenerator()

    async def load(
        self,
        store: ContextStore,
        state: AgentState,
        summary_record: ConversationSummaryRecord | None,
    ) -> ContextAssembly:
        """顺序读完未覆盖历史；窗口外原文先补接摘要，不留下摘要和近轮之间的空洞。"""
        summary = bounded_summary(summary_record.summary) if summary_record is not None else None
        cursor = summary_record.covered_until_message_id if summary_record is not None else None
        pending: list[CompletedTurnRecord] = []

        async def bridge(turns: list[CompletedTurnRecord]) -> None:
            nonlocal summary
            for offset in range(0, len(turns), 6):
                # 临时衔接只做本地抽取，不阻塞前台等待 LLM，也不推进持久化游标。
                goal = summary.goal if summary is not None else ""
                summary = await self._gap_generator.generate(
                    summary, turns[offset : offset + 6], {}
                )
                # 衔接原文不推断新目标，避免把某个历史问题升级为当前任务。
                summary = bounded_summary(summary.model_copy(update={"goal": goal}))

        # 有界分页，超过上限显式失败，绝不跳到最近记录后假装历史已完整覆盖。
        for _ in range(128):
            page = await store.list_completed_turn_records(
                state.user_id,
                state.conversation_id,
                after_message_id=cursor,
                limit=self._PAGE_SIZE,
                oldest_first=True,
            )
            if page:
                if any(
                    turn.assistant_message_id <= (cursor or 0)
                    or (index and turn.assistant_message_id <= page[index - 1].assistant_message_id)
                    for index, turn in enumerate(page)
                ):
                    raise ValueError("Completed history must advance in cursor order")
                cursor = page[-1].assistant_message_id
                pending.extend(page)
                if len(pending) > self._recent_turns:
                    await bridge(pending[: -self._recent_turns])
                    pending = pending[-self._recent_turns :]
            if len(page) < self._PAGE_SIZE:
                break
        else:
            raise ValueError("Conversation history exceeds context pagination limit")

        while pending:
            selected = self._select_turns(pending)
            if self._history_selector is not None:
                candidate = state.model_copy(
                    update={
                        "memory_summary": summary,
                        "history": ContextAssembly(summary, tuple(selected)).history,
                    }
                )
                kept = self._history_selector(candidate)
                selected = selected[-(len(kept) // 2) :] if kept else []
            omitted = len(pending) - len(selected)
            if not omitted:
                break
            await bridge(pending[:omitted])
            pending = pending[omitted:]
        return ContextAssembly(summary=summary, turns=tuple(pending))

    def _select_turns(self, candidates: list[CompletedTurnRecord]) -> list[CompletedTurnRecord]:
        selected: list[CompletedTurnRecord] = []
        chars = 0
        for turn in reversed(candidates[-self._recent_turns :]):
            cost = len(turn.user_content) + len(turn.assistant_content)
            if chars + cost > self._max_history_chars:
                break
            selected.insert(0, turn)
            chars += cost
        return selected

    def assemble(
        self,
        summary_record: ConversationSummaryRecord | None,
        turns: list[CompletedTurnRecord],
    ) -> ContextAssembly:
        summary = summary_record.summary if summary_record is not None else None
        covered = summary_record.covered_until_message_id if summary_record is not None else None
        candidates = [
            turn for turn in turns if covered is None or turn.assistant_message_id > covered
        ]
        selected = self._select_turns(candidates)
        return ContextAssembly(summary=summary, turns=tuple(selected))
