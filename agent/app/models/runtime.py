"""运行状态、可信主体和流式事件模型。"""

import re
from datetime import datetime
from typing import Any, Literal
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator

from app.models.provider import ModelSummary

RunStatus = Literal["RUNNING", "COMPLETED", "FAILED", "CANCELLED", "INTERRUPTED"]
EventType = Literal["thinking", "token", "done", "error"]


class Principal(BaseModel):
    """由可信入口构造的调用主体；不接受用户消息中的身份覆盖。"""

    model_config = ConfigDict(frozen=True, extra="forbid")

    user_id: int = Field(gt=0)
    roles: frozenset[str] = frozenset()
    request_id: str = Field(min_length=1, max_length=128)


class ChatContext(BaseModel):
    model_config = ConfigDict(extra="forbid")

    submission_id: int | None = Field(default=None, gt=0)
    problem_id: int | None = Field(default=None, gt=0)
    language: str | None = Field(default=None, min_length=1, max_length=32)


class ChatRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    conversation_id: UUID | None = None
    agent_key: str | None = Field(default=None, pattern=r"^[a-z][a-z0-9_]{1,63}$")
    skill_key: str | None = Field(default=None, pattern=r"^[a-z][a-z0-9_]{1,63}$")
    message: str = Field(min_length=1, max_length=32_000)
    context: ChatContext = Field(default_factory=ChatContext)

    @field_validator("message")
    @classmethod
    def nonempty_message(cls, value: str) -> str:
        if not value.strip():
            raise ValueError("Message cannot be blank")
        return value


class HistoryMessage(BaseModel):
    model_config = ConfigDict(frozen=True)

    role: Literal["user", "assistant", "tool"]
    content: str = Field(max_length=32_000)


class AgentState(BaseModel):
    """一次运行的最小可序列化状态。"""

    user_id: int = Field(gt=0)
    conversation_id: UUID
    run_id: UUID
    user_message: str = Field(min_length=1, max_length=32_000)
    principal: Principal
    status: RunStatus = "RUNNING"
    skill_key: str = Field(default="demo", min_length=1, max_length=128)
    agent_key: str = Field(default="demo", min_length=1, max_length=128)
    allowed_tools: tuple[str, ...] = ()
    config_snapshot: dict[str, Any] = Field(default_factory=dict)
    context: ChatContext = Field(default_factory=ChatContext)
    history: tuple[HistoryMessage, ...] = ()
    model_summary: ModelSummary | None = None
    answer_draft: str = ""
    missing_information: tuple[str, ...] = ()
    started_at: datetime
    deadline_at: datetime

    @model_validator(mode="after")
    def validate_identity_and_deadline(self) -> "AgentState":
        if self.user_id != self.principal.user_id:
            raise ValueError("State owner must match trusted principal")
        if self.deadline_at <= self.started_at:
            raise ValueError("Deadline must follow start")
        return self


class StreamEvent(BaseModel):
    """浏览器可消费的事件模型；不承载思维链或敏感内部数据。"""

    type: EventType
    run_id: UUID
    conversation_id: UUID
    data: dict[str, Any] = Field(default_factory=dict)
    sequence: int = Field(ge=1)

    @model_validator(mode="after")
    def validate_payload(self) -> "StreamEvent":
        if self.type in {"thinking", "token"}:
            text = self.data.get("text")
            maximum = 256 if self.type == "thinking" else 32_000
            if set(self.data) != {"text"} or not isinstance(text, str) or len(text) > maximum:
                raise ValueError("Invalid event text")
        elif self.type == "error":
            code = self.data.get("code")
            if (
                set(self.data) != {"code"}
                or not isinstance(code, str)
                or not re.fullmatch(r"[A-Z][A-Z0-9_]{0,63}", code)
            ):
                raise ValueError("Invalid public error code")
        elif self.data:
            raise ValueError("Done event cannot include internal payload")
        return self

    @classmethod
    def thinking(cls, state: AgentState, text: str, sequence: int) -> "StreamEvent":
        return cls(
            type="thinking",
            run_id=state.run_id,
            conversation_id=state.conversation_id,
            data={"text": text},
            sequence=sequence,
        )

    @classmethod
    def token(cls, state: AgentState, text: str, sequence: int) -> "StreamEvent":
        return cls(
            type="token",
            run_id=state.run_id,
            conversation_id=state.conversation_id,
            data={"text": text},
            sequence=sequence,
        )

    @classmethod
    def done(cls, state: AgentState, sequence: int) -> "StreamEvent":
        return cls(
            type="done",
            run_id=state.run_id,
            conversation_id=state.conversation_id,
            data={},
            sequence=sequence,
        )

    @classmethod
    def error(cls, state: AgentState, error_code: str, sequence: int) -> "StreamEvent":
        return cls(
            type="error",
            run_id=state.run_id,
            conversation_id=state.conversation_id,
            data={"code": error_code},
            sequence=sequence,
        )


_ALLOWED_TRANSITIONS: dict[RunStatus, frozenset[RunStatus]] = {
    "RUNNING": frozenset({"COMPLETED", "FAILED", "CANCELLED", "INTERRUPTED"}),
    "COMPLETED": frozenset(),
    "FAILED": frozenset(),
    "CANCELLED": frozenset(),
    "INTERRUPTED": frozenset(),
}


class InvalidRunTransition(ValueError):
    """运行状态不能从当前状态转移到目标状态。"""


def validate_run_transition(current: RunStatus, target: RunStatus) -> None:
    if target not in _ALLOWED_TRANSITIONS[current]:
        raise InvalidRunTransition(f"Cannot transition run from {current} to {target}")
