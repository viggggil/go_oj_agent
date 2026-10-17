"""模型调用公开错误及有界运行摘要，不保存原始 Provider 响应。"""

from dataclasses import dataclass, field
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

MODEL_ERROR_CODES = frozenset(
    {
        "AGENT_MODEL_AUTH_FAILED",
        "AGENT_MODEL_QUOTA",
        "AGENT_MODEL_RATE_LIMIT",
        "AGENT_MODEL_UNAVAILABLE",
        "AGENT_MODEL_TIMEOUT",
        "AGENT_MODEL_INVALID_RESPONSE",
        "AGENT_MODEL_REJECTED",
        "AGENT_MODEL_OUTPUT_LIMIT",
        "AGENT_MODEL_INPUT_LIMIT",
        "AGENT_MODEL_CALL_LIMIT",
        "AGENT_TOOL_CALL_LIMIT",
        "AGENT_MODEL_BLOCKED",
        "AGENT_MODEL_UNSUPPORTED_OUTPUT",
        "AGENT_CREDENTIAL_UNAVAILABLE",
        "AGENT_MODEL_ENDPOINT_DENIED",
    }
)


class ModelFailure(RuntimeError):
    def __init__(self, code: str, *, retryable: bool = False) -> None:
        super().__init__(code)
        self.code = code
        self.retryable = retryable


class FunctionCall(BaseModel):
    model_config = ConfigDict(frozen=True, extra="forbid", strict=True)
    call_id: str = Field(min_length=1, max_length=128, pattern=r"^[a-zA-Z0-9_-]+$")
    name: str = Field(pattern=r"^[a-z][a-z0-9_]{1,127}$")
    arguments: str = Field(max_length=8192)

    def input_item(self) -> dict[str, Any]:
        return {"type": "function_call", **self.model_dump()}


@dataclass
class ModelResponse:
    # 只在当前请求内短暂持有已校验动作及 Responses 后续请求所需的 opaque items。
    calls: tuple[FunctionCall, ...] = field(default_factory=tuple)
    continuation_items: tuple[dict[str, Any], ...] = ()
    reasoning_summary: str | None = None
    spent_input: int = 0
    spent_output: int = 0


class ToolTraceStep(BaseModel):
    """可持久化的脱敏 Tool 轨迹；不包含原始参数或工具返回正文。"""

    model_config = ConfigDict(extra="forbid")

    run_id: str | None = Field(default=None, pattern=r"^[0-9a-f-]{36}$")
    step: int = Field(ge=1, le=128)
    call_id: str = Field(min_length=1, max_length=128, pattern=r"^[a-zA-Z0-9_-]+$")
    tool_name: str = Field(min_length=1, max_length=128, pattern=r"^[a-z][a-z0-9_]{1,127}$")
    parameter_validation: Literal["valid", "invalid", "rejected"]
    status: Literal["succeeded", "failed", "rejected"]
    error_code: str | None = Field(default=None, max_length=64)
    duration_ms: int = Field(ge=0, le=300_000)
    summary: str = Field(max_length=256)
    evidence_refs: list[str] = Field(default_factory=list, max_length=16)


class ModelAttempt(BaseModel):
    model_config = ConfigDict(extra="forbid")
    provider_id: str = Field(max_length=36)
    model_profile_id: str = Field(max_length=36)
    model: str = Field(max_length=200)
    returned_model: str | None = Field(default=None, max_length=200)
    input_tokens: int = Field(default=0, ge=0, le=10_000_000)
    output_tokens: int = Field(default=0, ge=0, le=10_000_000)
    usage_source: Literal["estimated", "provider"] = "estimated"
    first_token_ms: int | None = Field(default=None, ge=0)
    duration_ms: int = Field(default=0, ge=0)
    finish_reason: Literal["completed", "failed", "incomplete", "cancelled"] | None = None
    error_code: str | None = Field(default=None, max_length=64)


class KnowledgeSource(BaseModel):
    model_config = ConfigDict(extra="forbid", allow_inf_nan=False)

    source_id: str = Field(max_length=36)
    base_id: str = Field(max_length=36)
    document_id: str = Field(max_length=36)
    version_id: str = Field(max_length=36)
    problem_id: int | None = None
    title: str = Field(max_length=128)
    number: int = Field(ge=0, le=1000)
    section: str = Field(max_length=128)
    start: int = Field(ge=0, le=120_000)
    end: int = Field(ge=0, le=120_000)
    score: float = Field(ge=-1, le=1)


class KnowledgeAttempt(BaseModel):
    model_config = ConfigDict(extra="forbid")

    call_id: str = Field(max_length=128)
    ok: bool
    error_code: str | None = Field(default=None, max_length=64)
    sources: list[KnowledgeSource] = Field(default_factory=list, max_length=5)


class ModelSummary(BaseModel):
    model_config = ConfigDict(extra="forbid")
    attempts: list[ModelAttempt] = Field(default_factory=list, max_length=16)
    tools: list[str] = Field(default_factory=list, max_length=32)
    tool_steps: list[ToolTraceStep] = Field(default_factory=list, max_length=128)
    input_estimation: Literal["utf8_bytes_upper_bound_v1"] = "utf8_bytes_upper_bound_v1"
    knowledge: list[KnowledgeAttempt] = Field(default_factory=list, max_length=32)
