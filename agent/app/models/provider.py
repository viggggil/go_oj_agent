"""模型调用公开错误及有界运行摘要，不保存原始 Provider 响应。"""

from typing import Literal

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
