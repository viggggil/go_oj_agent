"""结构化的单会话持久化记忆；内容是辅助上下文，不是系统指令。"""

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field


class ConfirmedFact(BaseModel):
    model_config = ConfigDict(frozen=True, extra="forbid")

    content: str = Field(min_length=1, max_length=2_000)
    source_message_id: int = Field(gt=0)
    source_type: Literal["user_statement", "conversation_conclusion", "tool_fact"]


class ConversationSummary(BaseModel):
    """可增量更新、大小有界且不承载模型内部推理的摘要。"""

    model_config = ConfigDict(frozen=True, extra="forbid")

    schema_version: Literal[1] = 1
    goal: str = Field(default="", max_length=2_000)
    user_constraints: tuple[str, ...] = Field(default=(), max_length=32)
    confirmed_facts: tuple[ConfirmedFact, ...] = Field(default=(), max_length=32)
    attempted_actions: tuple[str, ...] = Field(default=(), max_length=32)
    open_questions: tuple[str, ...] = Field(default=(), max_length=32)
    context_notes: tuple[str, ...] = Field(default=(), max_length=32)

    @staticmethod
    def _validate_items(items: tuple[str, ...], maximum: int) -> None:
        if any(not item.strip() or len(item) > 2_000 for item in items):
            raise ValueError("Summary items must be non-empty and bounded")
        if len(items) > maximum:
            raise ValueError("Summary has too many items")

    def model_post_init(self, __context: object) -> None:
        for items in (
            self.user_constraints,
            self.attempted_actions,
            self.open_questions,
            self.context_notes,
        ):
            self._validate_items(items, 32)

    def prompt_payload(self) -> dict[str, object]:
        """只返回给模型的辅助内容；明确不暴露来源消息 ID。"""
        return {
            "schema_version": self.schema_version,
            "goal": self.goal,
            "user_constraints": list(self.user_constraints),
            "confirmed_facts": [
                {"content": fact.content, "source_type": fact.source_type}
                for fact in self.confirmed_facts
            ],
            "attempted_actions": list(self.attempted_actions),
            "open_questions": list(self.open_questions),
            "context_notes": list(self.context_notes),
        }
