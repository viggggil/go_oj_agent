"""Tool 元数据注册、输入校验和最小权限执行器。"""

import asyncio
from collections.abc import Awaitable, Callable, Mapping
from dataclasses import dataclass
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError, model_validator

from app.models.runtime import Principal

ReadScope = Literal["public", "current_user", "admin"]
SideEffect = Literal["read", "write"]
Sensitivity = Literal["public", "private", "source", "internal"]


class ToolRegistryError(ValueError):
    """Registry 约束错误。"""


class ToolNotFound(ToolRegistryError):
    """工具未注册。"""


class ToolPermissionDenied(ToolRegistryError):
    """调用主体不满足工具策略。"""


class ToolSpec(BaseModel):
    model_config = ConfigDict(frozen=True)

    name: str = Field(pattern=r"^[a-z][a-z0-9_]{1,127}$")
    description: str = Field(min_length=1, max_length=1000)
    read_scope: ReadScope
    allowed_roles: frozenset[str] = frozenset()
    side_effect: SideEffect = "read"
    sensitivity: Sensitivity = "public"
    timeout_seconds: float = Field(default=5, gt=0, le=60)
    requires_confirmation: bool = False
    enabled: bool = True

    @model_validator(mode="after")
    def protect_writes(self) -> "ToolSpec":
        if self.side_effect == "write" and (self.enabled or not self.requires_confirmation):
            raise ValueError("Write tools must be disabled and require confirmation")
        if self.read_scope == "admin" and not self.allowed_roles:
            raise ValueError("Admin tool requires explicit allowed roles")
        return self


@dataclass(frozen=True)
class ToolDefinition:
    spec: ToolSpec
    input_model: type[BaseModel]
    output_model: type[BaseModel]
    handler: Callable[[BaseModel, Principal], Awaitable[BaseModel]]

    def catalog(self) -> dict[str, Any]:
        return {
            **self.spec.model_dump(mode="json"),
            "input_schema": self.input_model.model_json_schema(),
            "output_schema": self.output_model.model_json_schema(),
        }


class ToolResult(BaseModel):
    tool_name: str
    ok: bool
    data: Any = None
    error_code: str | None = None


class ToolContext(BaseModel):
    principal: Principal
    remaining_seconds: float = Field(gt=0, le=60)
    confirmed: bool = False
    allowed_tools: frozenset[str] = frozenset()


class ToolRegistry:
    def __init__(self) -> None:
        self._definitions: dict[str, ToolDefinition] = {}

    def register(self, definition: ToolDefinition) -> None:
        name = definition.spec.name
        if name in self._definitions:
            raise ToolRegistryError(f"Tool already registered: {name}")
        self._definitions[name] = definition

    def get(self, name: str) -> ToolDefinition:
        try:
            return self._definitions[name]
        except KeyError:
            raise ToolNotFound(f"Tool is not registered: {name}") from None

    def catalog(self) -> list[dict[str, Any]]:
        return [self._definitions[name].catalog() for name in sorted(self._definitions)]

    def names(self) -> tuple[str, ...]:
        return tuple(sorted(self._definitions))


class ToolExecutor:
    def __init__(self, registry: ToolRegistry) -> None:
        self._registry = registry

    async def execute(
        self, name: str, arguments: Mapping[str, Any], context: ToolContext
    ) -> ToolResult:
        try:
            definition = self._registry.get(name)
            if name not in context.allowed_tools:
                raise ToolPermissionDenied("Tool is not in run allowlist")
            self._authorize(definition.spec, context)
            try:
                parsed = definition.input_model.model_validate(dict(arguments))
            except ValidationError:
                return ToolResult(tool_name=name, ok=False, error_code="TOOL_INVALID_ARGUMENT")
            timeout = min(definition.spec.timeout_seconds, context.remaining_seconds)
            output = await asyncio.wait_for(definition.handler(parsed, context.principal), timeout)
            validated = definition.output_model.model_validate(output)
            return ToolResult(tool_name=name, ok=True, data=validated.model_dump())
        except ToolNotFound:
            return ToolResult(tool_name=name, ok=False, error_code="TOOL_NOT_FOUND")
        except ToolPermissionDenied:
            return ToolResult(tool_name=name, ok=False, error_code="TOOL_PERMISSION_DENIED")
        except TimeoutError:
            return ToolResult(tool_name=name, ok=False, error_code="TOOL_TIMEOUT")
        except Exception:
            return ToolResult(tool_name=name, ok=False, error_code="TOOL_FAILED")

    @staticmethod
    def _authorize(spec: ToolSpec, context: ToolContext) -> None:
        if not spec.enabled:
            raise ToolPermissionDenied("Tool is disabled")
        if spec.read_scope == "current_user" and context.principal.user_id <= 0:
            raise ToolPermissionDenied("Current user is required")
        if spec.read_scope == "admin":
            if not context.principal.roles.intersection(spec.allowed_roles):
                raise ToolPermissionDenied("Admin role is required")
        if spec.allowed_roles and not context.principal.roles.intersection(spec.allowed_roles):
            raise ToolPermissionDenied("Role is not allowed")
        if spec.side_effect == "write" and spec.requires_confirmation and not context.confirmed:
            raise ToolPermissionDenied("Confirmation is required")
