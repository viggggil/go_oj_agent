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
    return value[: maximum - 1].rstrip() + "…"


class IncrementalSummaryGenerator:
    """Roll completed turns into a bounded, non-instructional conversation summary.

    This is intentionally conservative: it never invents facts or claims that a
    tool was called. Older context is compacted as a rolling extractive note while
    recent turns remain separate. The stored text remains auxiliary context and is
    treated as untrusted by the prompt assembler.
    """

    _RECENT_NOTES = 8
    _COMPACT_NOTE_CHARS = 2_400
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
            notes.append(_clip(f"用户：{turn.user_content}\n助手：{turn.assistant_content}"))
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
