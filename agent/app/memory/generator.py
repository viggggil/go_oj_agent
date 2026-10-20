"""Summary generators used by the conversation memory worker.

The first implementation is deliberately extractive. It keeps the worker useful in
fake/test deployments without making an untracked model request. The protocol is
the extension point for a model-backed summarizer in a later PR.
"""

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


class ExtractiveSummaryGenerator:
    """Build a bounded, non-instructional summary from completed turns.

    This is intentionally conservative: it never invents facts or claims that a
    tool was called. The stored text remains auxiliary context and is treated as
    untrusted by the prompt assembler.
    """

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
        actions = list(base.attempted_actions)
        for turn in turns:
            notes.append(_clip(f"用户：{turn.user_content}\n助手：{turn.assistant_content}"))
            actions.append(_clip(f"已完成对话轮次：{turn.user_content}", 512))
        return ConversationSummary(
            goal=_clip(turns[-1].user_content) if turns else base.goal,
            user_constraints=base.user_constraints,
            confirmed_facts=base.confirmed_facts,
            attempted_actions=tuple(actions[-32:]),
            open_questions=base.open_questions,
            context_notes=tuple(notes[-32:]),
        )
