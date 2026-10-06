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

    source: Literal["demo_environment"] = "demo_environment"
    version: str = "pr2-demo-v1"
    runtime: Literal["fake", "langgraph_fake"]
    budget: RuntimeBudget
    allowed_tools: tuple[str, ...] = ()


class ConfigSnapshotReader(Protocol):
    async def read(self) -> ConfigSnapshot: ...


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
        )

    async def read(self) -> ConfigSnapshot:
        return self._snapshot.model_copy(deep=True)
