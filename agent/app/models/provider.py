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
    # 仅持有校验后的函数调用，不保存推理、Provider 原始响应或凭据。
    calls: tuple[FunctionCall, ...] = field(default_factory=tuple)
    spent_input: int = 0
    spent_output: int = 0


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


class ModelSummary(BaseModel):
    model_config = ConfigDict(extra="forbid")
    attempts: list[ModelAttempt] = Field(default_factory=list, max_length=16)
    input_estimation: Literal["utf8_bytes_upper_bound_v1"] = "utf8_bytes_upper_bound_v1"
