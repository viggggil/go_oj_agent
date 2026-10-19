"""会话记忆装配；负责去重和有界读取，不参与 ReAct 决策。"""

from dataclasses import dataclass

from app.models.memory import ConversationSummary
from app.models.runtime import HistoryMessage
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


class ContextAssembler:
    """保留摘要之后的最近完整轮次，避免摘要与原文重复。"""

    def __init__(self, *, recent_turns: int = 6, max_history_chars: int = 64_000) -> None:
        if recent_turns < 1 or max_history_chars < 1:
            raise ValueError("Context limits must be positive")
        self._recent_turns = recent_turns
        self._max_history_chars = max_history_chars

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
        selected: list[CompletedTurnRecord] = []
        chars = 0
        for turn in reversed(candidates[-self._recent_turns :]):
            cost = len(turn.user_content) + len(turn.assistant_content)
            if selected and chars + cost > self._max_history_chars:
                break
            if not selected and cost > self._max_history_chars:
                break
            selected.insert(0, turn)
            chars += cost
        return ContextAssembly(summary=summary, turns=tuple(selected))
