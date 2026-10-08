"""多 Agent 的不可变配置；不接受任意代码、模板表达式或部署凭据。"""

import hashlib
import json
from datetime import datetime
from string import Formatter
from typing import Annotated, Any, Literal, TypeAlias
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, model_validator

ConfigKind = Literal["prompt", "skill", "model", "agent"]
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
    prompt_key: ConfigKey | None = None
    execution_mode: Literal["direct", "react"] = "direct"
    allowed_tools: tuple[ToolName, ...] = Field(default=(), max_length=32)
    budget: RuntimeBudget = Field(default_factory=RuntimeBudget)

    @model_validator(mode="after")
    def distinct_tools(self) -> "SkillConfig":
        if len(set(self.allowed_tools)) != len(self.allowed_tools):
            raise ValueError("Duplicate tools")
        return self


class ModelProfileConfig(ImmutableConfig):
    provider: Literal["fake"] = "fake"
    model: Literal["fake"] = "fake"


class AgentConfig(ImmutableConfig):
    name: str = Field(min_length=1, max_length=128)
    prompt_id: UUID
    prompt_key: ConfigKey | None = None
    skill_ids: tuple[UUID, ...] = Field(min_length=1, max_length=16)
    default_skill_id: UUID
    default_skill_key: ConfigKey | None = None
    model_profile_id: UUID
    model_profile_key: ConfigKey | None = None
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


ConfigBody: TypeAlias = PromptConfig | SkillConfig | ModelProfileConfig | AgentConfig
CONFIG_MODELS: dict[ConfigKind, type[ConfigBody]] = {
    "prompt": PromptConfig,
    "skill": SkillConfig,
    "model": ModelProfileConfig,
    "agent": AgentConfig,
}


class ConfigWrite(ImmutableConfig):
    kind: ConfigKind
    key: ConfigKey
    actor_id: int = Field(gt=0, le=2**63 - 1, strict=True)
    request_id: str = Field(min_length=1, max_length=128, pattern=r"^[a-zA-Z0-9._:-]+$")
    expected_id: UUID | None = None


def parse_config(kind: ConfigKind, content: Any) -> ConfigBody:
    return CONFIG_MODELS[kind].model_validate(content)


def config_references(body: ConfigBody) -> dict[str, ConfigKind]:
    if isinstance(body, SkillConfig):
        return {str(body.prompt_id): "prompt"}
    if isinstance(body, AgentConfig):
        return {
            str(body.prompt_id): "prompt",
            str(body.model_profile_id): "model",
            **{str(skill_id): "skill" for skill_id in body.skill_ids},
        }
    return {}


def config_hash(value: Any) -> str:
    encoded = json.dumps(value, ensure_ascii=True, sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(encoded.encode("utf-8")).hexdigest()
