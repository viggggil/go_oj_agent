"""多 Agent 的不可变配置；不接受任意代码、模板表达式或部署凭据。"""

import hashlib
import json
from datetime import datetime
from string import Formatter
from typing import Annotated, Any, Literal
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, model_validator

ConfigKind = Literal["prompt", "skill", "model", "agent", "provider"]
ConfigKey = Annotated[str, Field(pattern=r"^[a-z][a-z0-9_]{1,63}$")]
ToolName = Annotated[str, Field(pattern=r"^[a-z][a-z0-9_]{1,127}$")]
PromptVariable = Literal["language", "problem_id", "submission_id"]


class ConfigError(ValueError):
    def __init__(self, code: str, status: int = 400) -> None:
        super().__init__(code)
        self.code = code
        self.status = status


class ImmutableConfig(BaseModel):
    model_config = ConfigDict(frozen=True, extra="forbid", allow_inf_nan=False)


class RuntimeBudget(ImmutableConfig):
    max_run_seconds: float = Field(default=30, gt=0, le=300)
    max_output_chars: int = Field(default=8_000, ge=1, le=32_000, strict=True)
    max_events: int = Field(default=1_000, ge=3, le=10_000, strict=True)
    max_tool_calls: int = Field(default=8, ge=0, le=32, strict=True)
    max_model_calls: int = Field(default=8, ge=1, le=16, strict=True)
    max_input_tokens: int = Field(default=32_000, ge=1, le=128_000, strict=True)
    max_output_tokens: int = Field(default=8_000, ge=1, le=32_000, strict=True)


class PromptConfig(ImmutableConfig):
    text: str = Field(min_length=1, max_length=32_000)
    variables: tuple[PromptVariable, ...] = ()

    @model_validator(mode="after")
    def validate_template(self) -> "PromptConfig":
        if not self.text.strip() or len(set(self.variables)) != len(self.variables):
            raise ValueError("Invalid prompt text or variables")
        found: set[str] = set()
        for _, name, spec, conversion in Formatter().parse(self.text):
            if name is not None:
                if name not in self.variables or spec or conversion:
                    raise ValueError("Unsupported prompt variable or expression")
                found.add(name)
        if found != set(self.variables):
            raise ValueError("Prompt variable declarations must match the template")
        return self


class SkillConfig(ImmutableConfig):
    name: str = Field(min_length=1, max_length=128)
    prompt_id: UUID
    execution_mode: Literal["direct", "react"] = "direct"
    allowed_tools: tuple[ToolName, ...] = Field(default=(), max_length=32)
    budget: RuntimeBudget = Field(default_factory=RuntimeBudget)

    @model_validator(mode="after")
    def distinct_tools(self) -> "SkillConfig":
        if len(set(self.allowed_tools)) != len(self.allowed_tools):
            raise ValueError("Duplicate tools")
        return self


class ModelProfileConfig(ImmutableConfig):
    provider: Literal["fake", "responses"] = "fake"
    model: str = Field(
        default="fake", min_length=1, max_length=200, pattern=r"^[a-zA-Z0-9][a-zA-Z0-9_./:@+-]*$"
    )
    provider_id: UUID | None = None
    temperature: float | None = Field(default=None, ge=0, le=2)
    verbosity: Literal["low", "medium", "high"] | None = None
    max_output_tokens: int = Field(default=4096, ge=1, le=32_000, strict=True)
    context_window_tokens: int = Field(default=32_000, ge=2, le=256_000, strict=True)

    @model_validator(mode="after")
    def validate_provider(self) -> "ModelProfileConfig":
        if self.provider == "fake":
            if self.model != "fake" or self.provider_id is not None or self.temperature is not None:
                raise ValueError("Fake profiles cannot contain real model settings")
        elif self.provider_id is None:
            raise ValueError("Real profiles require a Provider version")
        if self.max_output_tokens >= self.context_window_tokens:
            raise ValueError("Output must leave room for input")
        return self


class ProviderConfig(ImmutableConfig):
    name: str = Field(min_length=1, max_length=128)
    adapter: Literal["responses"] = "responses"
    base_url: str = Field(min_length=1, max_length=512)
    credential_id: UUID
    connect_timeout_seconds: float = Field(default=5, gt=0, le=30)
    first_token_timeout_seconds: float = Field(default=20, gt=0, le=120)
    idle_timeout_seconds: float = Field(default=15, gt=0, le=60)
    request_timeout_seconds: float = Field(default=30, gt=0, le=300)
    max_retries: int = Field(default=0, ge=0, le=2, strict=True)


class AgentConfig(ImmutableConfig):
    name: str = Field(min_length=1, max_length=128)
    prompt_id: UUID
    skill_ids: tuple[UUID, ...] = Field(min_length=1, max_length=16)
    default_skill_id: UUID
    model_profile_id: UUID
    allowed_tools: tuple[ToolName, ...] = Field(default=(), max_length=32)
    budget: RuntimeBudget = Field(default_factory=RuntimeBudget)
    visibility: Literal["user", "admin"] = "user"
    is_test: bool = Field(default=False, strict=True)
    test_expires_at: datetime | None = None
    knowledge_scope: tuple[UUID, ...] = Field(default=(), max_length=0)

    @model_validator(mode="after")
    def validate_bindings(self) -> "AgentConfig":
        if len(set(self.skill_ids)) != len(self.skill_ids):
            raise ValueError("Duplicate skills")
        if self.default_skill_id not in self.skill_ids:
            raise ValueError("Default skill must be bound to the Agent")
        if len(set(self.allowed_tools)) != len(self.allowed_tools):
            raise ValueError("Duplicate tools")
        if self.is_test and (self.visibility != "admin" or self.test_expires_at is None):
            raise ValueError("Test Agents require admin visibility and expiration")
        if not self.is_test and self.test_expires_at is not None:
            raise ValueError("Only test Agents may have expiration")
        if self.test_expires_at is not None and self.test_expires_at.tzinfo is None:
            raise ValueError("Expiration requires an explicit timezone")
        return self


type ConfigBody = PromptConfig | SkillConfig | ModelProfileConfig | AgentConfig | ProviderConfig
CONFIG_MODELS: dict[ConfigKind, type[ConfigBody]] = {
    "prompt": PromptConfig,
    "skill": SkillConfig,
    "model": ModelProfileConfig,
    "agent": AgentConfig,
    "provider": ProviderConfig,
}


class ConfigWrite(ImmutableConfig):
    kind: ConfigKind
    key: ConfigKey
    actor_id: int = Field(gt=0, le=2**63 - 1, strict=True)
    request_id: str = Field(min_length=1, max_length=128, pattern=r"^[a-zA-Z0-9._:-]+$")
    expected_id: UUID | None = None
    source_id: UUID | None = None


def parse_config(kind: ConfigKind, content: Any) -> ConfigBody:
    return CONFIG_MODELS[kind].model_validate(content)


def config_references(body: ConfigBody) -> dict[str, ConfigKind]:
    if isinstance(body, ModelProfileConfig) and body.provider_id is not None:
        return {str(body.provider_id): "provider"}
    if isinstance(body, SkillConfig):
        return {str(body.prompt_id): "prompt"}
    if isinstance(body, AgentConfig):
        identifiers = [body.prompt_id, body.model_profile_id, *body.skill_ids]
        if len(set(identifiers)) != len(identifiers):
            raise ConfigError("AGENT_CONFIGURATION_REFERENCE_INVALID")
        return {
            str(body.prompt_id): "prompt",
            str(body.model_profile_id): "model",
            **{str(skill_id): "skill" for skill_id in body.skill_ids},
        }
    return {}


def config_hash(value: Any) -> str:
    encoded = json.dumps(value, ensure_ascii=True, sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(encoded.encode("utf-8")).hexdigest()
