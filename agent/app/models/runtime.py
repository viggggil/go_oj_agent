"""运行状态、可信主体和流式事件模型。"""

import re
from datetime import datetime
from typing import Any, Literal
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator

from app.models.memory import ConversationSummary
from app.models.provider import ModelSummary

RunStatus = Literal["RUNNING", "COMPLETED", "FAILED", "CANCELLED", "INTERRUPTED"]
EventType = Literal[
    "thinking",
    "progress",
    "reasoning_summary",
    "tool_call",
    "tool_result",
    "token",
    "done",
    "error",
]
_TOOL_NAME = re.compile(r"^[a-z][a-z0-9_]{1,127}$")
_CALL_ID = re.compile(r"^[a-zA-Z0-9_-]{1,128}$")
_ERROR_CODE = re.compile(r"^[A-Z][A-Z0-9_]{0,63}$")


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
    memory_summary: ConversationSummary | None = None
    model_summary: ModelSummary | None = None
    knowledge_queries: int = Field(default=0, ge=0)
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
            maximum = {"thinking": 256, "reasoning_summary": 400, "token": 32_000}[self.type]
            if set(self.data) != {"text"} or not isinstance(text, str) or len(text) > maximum:
                raise ValueError("Invalid event text")
        elif self.type == "reasoning_summary":
            if (
                set(self.data) != {"step", "text"}
                or type(self.data.get("step")) is not int
                or not 1 <= self.data["step"] <= 128
                or not isinstance(self.data.get("text"), str)
                or not 1 <= len(self.data["text"]) <= 400
            ):
                raise ValueError("Invalid reasoning summary")
        elif self.type == "progress":
            phase, message = self.data.get("phase"), self.data.get("message")
            if (
                set(self.data) != {"phase", "message"}
                or phase not in {"preparing", "analyzing", "executing_tool", "answering"}
                or not isinstance(message, str)
                or not 1 <= len(message) <= 256
            ):
                raise ValueError("Invalid progress event")
        elif self.type == "tool_call":
            if (
                set(self.data) != {"step", "call_id", "tool", "status", "label"}
                or not isinstance(self.data.get("step"), int)
                or isinstance(self.data.get("step"), bool)
                or not 1 <= self.data["step"] <= 128
                or not isinstance(self.data.get("call_id"), str)
                or not _CALL_ID.fullmatch(self.data["call_id"])
                or not isinstance(self.data.get("tool"), str)
                or not _TOOL_NAME.fullmatch(self.data["tool"])
                or self.data.get("status") != "started"
                or not isinstance(self.data.get("label"), str)
                or not 1 <= len(self.data["label"]) <= 128
            ):
                raise ValueError("Invalid tool call event")
        elif self.type == "tool_result":
            if (
                set(self.data) != {"step", "call_id", "tool", "status", "duration_ms", "summary"}
                or not isinstance(self.data.get("step"), int)
                or isinstance(self.data.get("step"), bool)
                or not 1 <= self.data["step"] <= 128
                or not isinstance(self.data.get("call_id"), str)
                or not _CALL_ID.fullmatch(self.data["call_id"])
                or not isinstance(self.data.get("tool"), str)
                or not _TOOL_NAME.fullmatch(self.data["tool"])
                or self.data.get("status") not in {"succeeded", "failed", "rejected"}
                or not isinstance(self.data.get("duration_ms"), int)
                or isinstance(self.data.get("duration_ms"), bool)
                or not 0 <= self.data["duration_ms"] <= 300_000
                or not isinstance(self.data.get("summary"), str)
                or len(self.data["summary"]) > 256
            ):
                raise ValueError("Invalid tool result event")
        elif self.type == "error":
            code = self.data.get("code")
            if (
                set(self.data) != {"code"}
                or not isinstance(code, str)
                or not _ERROR_CODE.fullmatch(code)
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
    def progress(cls, state: AgentState, phase: str, message: str, sequence: int) -> "StreamEvent":
        return cls(
            type="progress",
            run_id=state.run_id,
            conversation_id=state.conversation_id,
            data={"phase": phase, "message": message},
            sequence=sequence,
        )

    @classmethod
    def reasoning_summary(
        cls, state: AgentState, text: str, sequence: int, *, step: int = 1
    ) -> "StreamEvent":
        return cls(
            type="reasoning_summary",
            run_id=state.run_id,
            conversation_id=state.conversation_id,
            data={"step": step, "text": text},
            sequence=sequence,
        )

    @classmethod
    def tool_call(
        cls, state: AgentState, *, step: int, call_id: str, tool: str, label: str, sequence: int
    ) -> "StreamEvent":
        return cls(
            type="tool_call",
            run_id=state.run_id,
            conversation_id=state.conversation_id,
            data={
                "step": step,
                "call_id": call_id,
                "tool": tool,
                "status": "started",
                "label": label,
            },
            sequence=sequence,
        )

    @classmethod
    def tool_result(
        cls,
        state: AgentState,
        *,
        step: int,
        call_id: str,
        tool: str,
        status: str,
        duration_ms: int,
        summary: str,
        sequence: int,
    ) -> "StreamEvent":
        return cls(
            type="tool_result",
            run_id=state.run_id,
            conversation_id=state.conversation_id,
            data={
                "step": step,
                "call_id": call_id,
                "tool": tool,
                "status": status,
                "duration_ms": duration_ms,
                "summary": summary,
            },
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
