"""统一 ReAct Runtime 的显式、可审计状态边界。"""

from dataclasses import dataclass, field
from typing import Any, Literal, TypedDict

from app.graphs.direct_answer import PreparedAnswer
from app.models.provider import FunctionCall, ModelResponse, ToolTraceStep
from app.models.runtime import AgentState


@dataclass(frozen=True)
class ReActDecision:
    kind: Literal["finish", "action"]
    answer: str = ""
    calls: tuple[FunctionCall, ...] = ()
    reasoning_summary: str | None = None


@dataclass(frozen=True)
class ToolObservation:
    call_id: str
    tool_name: str
    ok: bool
    error_code: str | None
    output: str
    evidence_refs: tuple[str, ...] = ()


class ReActState(TypedDict, total=False):
    agent_state: AgentState
    prepared: PreparedAnswer
    current: PreparedAnswer
    tools: tuple[dict[str, Any], ...]
    model_response: ModelResponse
    decision: ReActDecision
    observations: list[ToolObservation]
    evidence: list[dict[str, Any]]
    step_count: int
    model_call_count: int
    tool_call_count: int
    spent_input: int
    spent_output: int
    extra_input: int
    seen_call_ids: set[str]
    seen_tool_fingerprints: set[str]
    tool_steps: list[ToolTraceStep]
    final_answer: str
    failure_code: str | None
    pending_arguments: list[dict[str, Any]]
    token_parts: tuple[str, ...]


@dataclass
class ReActContext:
    """请求局部的 Provider 续接信息；禁止进入图快照、审计和 SSE。"""

    provider_items: list[dict[str, Any]] = field(default_factory=list)
    continuation_items: tuple[dict[str, Any], ...] = ()


@dataclass(frozen=True)
class StreamItem:
    """带显式消费确认的 LangGraph 自定义流事件。"""

    event: Any
    consumed: Any
