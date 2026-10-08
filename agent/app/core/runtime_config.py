"""运行配置快照接口；PR2 的 demo 配置不代表已发布控制面。"""

from typing import Literal, Protocol

from pydantic import BaseModel, ConfigDict, Field

from app.core.settings import Settings


class RuntimeBudget(BaseModel):
    model_config = ConfigDict(frozen=True)

    max_run_seconds: float = Field(default=30, gt=0, le=300)
    max_output_chars: int = Field(default=8_000, ge=1, le=32_000)
    max_events: int = Field(default=1_000, ge=3, le=10_000)


class ConfigSnapshot(BaseModel):
    model_config = ConfigDict(frozen=True)

    source: Literal["demo_environment", "database"] = "demo_environment"
    version: str = "pr2-demo-v1"
    runtime: Literal["fake", "langgraph_fake"]
    budget: RuntimeBudget
    allowed_tools: tuple[str, ...] = ()
    agent_key: str = "demo"
    skill_key: str = "demo"
    prompt_id: str | None = None
    skill_id: str | None = None
    model_profile_id: str | None = None
    prompt_text: str | None = None
    skill_prompt_id: str | None = None
    skill_prompt_text: str | None = None


class ConfigSnapshotReader(Protocol):
    async def read(self, agent_key: str = "demo", skill_key: str | None = None) -> ConfigSnapshot: ...


class DemoConfigReader:
    def __init__(self, settings: Settings) -> None:
        if settings.runtime_mode == "disabled" or settings.environment == "production":
            raise ValueError("Demo runtime must be explicitly enabled outside production")
        self._snapshot = ConfigSnapshot(
            runtime=settings.runtime_mode,
            budget=RuntimeBudget(
                max_run_seconds=settings.max_run_seconds,
                max_output_chars=settings.max_output_chars,
                max_events=settings.max_run_events,
            ),
            allowed_tools=(
                "get_problem",
                "list_problems",
                "get_submission",
                "get_submission_source",
                "get_judge_result",
                "list_submissions",
            )
            if settings.business_tools_enabled
            else (),
        )

    async def read(self, agent_key: str = "demo", skill_key: str | None = None) -> ConfigSnapshot:
        if agent_key != "demo" or (skill_key is not None and skill_key != "demo"):
            raise ValueError("Unknown demo Agent")
        return self._snapshot.model_copy(deep=True)
