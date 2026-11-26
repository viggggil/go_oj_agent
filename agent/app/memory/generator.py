"""Bounded incremental summary generation for the conversation memory worker."""

from collections.abc import Sequence
from typing import Any, Protocol

from app.models.memory import ConversationSummary
from app.storage.repository import CompletedTurnRecord


class SummaryGenerator(Protocol):
    async def generate(
        self,
        previous: ConversationSummary | None,
        turns: Sequence[CompletedTurnRecord],
        config_snapshot: dict[str, Any],
    ) -> ConversationSummary:
        """Return a bounded summary for the supplied completed turns."""


def _clip(value: str, maximum: int = 1_800) -> str:
    value = " ".join(value.split())
    if len(value) <= maximum:
        return value
    head = (maximum - 1) // 2
    return value[:head].rstrip() + "…" + value[-(maximum - 1 - head) :].lstrip()


def bounded_summary(summary: ConversationSummary) -> ConversationSummary:
    """整条缩减旧记忆，给前台上下文和后台摘要使用相同的大小边界。"""
    values = summary.model_dump(mode="python")
    result = summary
    while len(result.model_dump_json().encode()) > 12_000:
        fields = (
            "context_notes",
            "attempted_actions",
            "open_questions",
            "confirmed_facts",
            "user_constraints",
        )
        shrink_field = next((name for name in fields if values[name]), None)
        if shrink_field is None:
            raise ValueError("Summary exceeds storage budget")
        values[shrink_field] = values[shrink_field][1:]
        result = ConversationSummary.model_validate(values)
    return result


class IncrementalSummaryGenerator:
    """Roll completed turns into a bounded, non-instructional conversation summary.

    This is intentionally conservative: it never invents facts or claims that a
    tool was called. Older context is compacted as a rolling extractive note while
    recent turns remain separate. The stored text remains auxiliary context and is
    treated as untrusted by the prompt assembler.
    """

    _RECENT_NOTES = 8
    _COMPACT_NOTE_CHARS = 1_800
    _TOTAL_NOTE_CHARS = 6_000

    async def generate(
        self,
        previous: ConversationSummary | None,
        turns: Sequence[CompletedTurnRecord],
        config_snapshot: dict[str, Any],
    ) -> ConversationSummary:
        del config_snapshot
        if not turns and previous is not None:
            return previous
        base = previous or ConversationSummary()
        notes = list(base.context_notes)
        for turn in turns:
            notes.append(
                f"用户原文节选：{_clip(turn.user_content, 850)}\n"
                f"助手原文节选（未经独立验证）：{_clip(turn.assistant_content, 850)}"
            )
        notes = list(dict.fromkeys(note for note in notes if note.strip()))

        # Keep a compact rolling record of older context and preserve the latest
        # turns verbatim enough to remain useful for follow-up questions.
        recent = notes[-self._RECENT_NOTES :]
        older = notes[: -self._RECENT_NOTES]
        compacted: list[str] = []
        if older:
            merged = "；".join(older)
            if len(merged) > self._COMPACT_NOTE_CHARS:
                merged = "…" + merged[-(self._COMPACT_NOTE_CHARS - 1) :]
            compacted.append(_clip("较早对话摘要：" + merged, self._COMPACT_NOTE_CHARS))
        compacted.extend(recent)

        # Keep prompt cost stable as a conversation grows.
        while compacted and sum(map(len, compacted)) > self._TOTAL_NOTE_CHARS:
            if len(compacted) > 1:
                compacted.pop(1)
            else:
                compacted[0] = _clip(compacted[0], self._TOTAL_NOTE_CHARS)
                break
        return ConversationSummary(
            goal=_clip(turns[-1].user_content) if turns else base.goal,
            user_constraints=base.user_constraints,
            confirmed_facts=base.confirmed_facts,
            attempted_actions=base.attempted_actions,
            open_questions=base.open_questions,
            context_notes=tuple(compacted),
        )


# Keep the original import name for deployments or extensions using the first
# extractive implementation directly.
ExtractiveSummaryGenerator = IncrementalSummaryGenerator
