"""工具注册、Schema、策略、异常和取消不能授予额外权限。"""

import asyncio

import pytest
from pydantic import BaseModel, ConfigDict

from app.models.runtime import Principal
from app.tools.registry import (
    ToolContext,
    ToolDefinition,
    ToolExecutor,
    ToolRegistry,
    ToolRegistryError,
    ToolSpec,
)


class Input(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)
    value: int


class Output(BaseModel):
    value: int


def definition(spec: ToolSpec | None = None) -> ToolDefinition:
    async def handler(arguments: BaseModel, principal: Principal) -> BaseModel:
        assert isinstance(arguments, Input)
        return Output(value=arguments.value + principal.user_id)

    return ToolDefinition(
        spec=spec or ToolSpec(name="test_tool", description="测试工具", read_scope="current_user"),
        input_model=Input,
        output_model=Output,
        handler=handler,
    )


def context(*, roles: frozenset[str] = frozenset(), allowed: bool = True) -> ToolContext:
    return ToolContext(
        principal=Principal(user_id=1, request_id="request", roles=roles),
        remaining_seconds=1,
        allowed_tools=frozenset({"test_tool"}) if allowed else frozenset(),
    )


async def test_registry_catalog_and_typed_execution() -> None:
    registry = ToolRegistry()
    registry.register(definition())
    with pytest.raises(ToolRegistryError):
        registry.register(definition())
    catalog = registry.catalog()
    assert catalog[0]["name"] == "test_tool"
    assert catalog[0]["input_schema"]["additionalProperties"] is False
    assert catalog[0]["output_schema"]["properties"]["value"]["type"] == "integer"
    result = await ToolExecutor(registry).execute("test_tool", {"value": 2}, context())
    assert result.ok and result.data == {"value": 3}


@pytest.mark.parametrize("arguments", [{}, {"value": "2"}, {"value": 2, "user_id": 999}])
async def test_invalid_arguments_do_not_execute(arguments: dict[str, object]) -> None:
    registry = ToolRegistry()
    registry.register(definition())
    result = await ToolExecutor(registry).execute("test_tool", arguments, context())
    assert not result.ok and result.error_code == "TOOL_INVALID_ARGUMENT"


async def test_allowlist_admin_and_disabled_policy() -> None:
    for spec, invocation in (
        (
            ToolSpec(name="test_tool", description="tool", read_scope="public"),
            context(allowed=False),
        ),
        (
            ToolSpec(
                name="test_tool",
                description="tool",
                read_scope="admin",
                allowed_roles=frozenset({"system_admin"}),
            ),
            context(),
        ),
        (
            ToolSpec(name="test_tool", description="tool", read_scope="public", enabled=False),
            context(),
        ),
    ):
        registry = ToolRegistry()
        registry.register(definition(spec))
        result = await ToolExecutor(registry).execute("test_tool", {"value": 2}, invocation)
        assert not result.ok and result.error_code == "TOOL_PERMISSION_DENIED"
    assert not ToolRegistry().names()  # 生产注册表没有真实业务工具。


async def test_timeout_and_cancel_propagate_to_handler() -> None:
    started = asyncio.Event()
    closed = asyncio.Event()

    async def handler(arguments: BaseModel, principal: Principal) -> BaseModel:
        try:
            started.set()
            await asyncio.Event().wait()
            return Output(value=1)
        finally:
            closed.set()

    registry = ToolRegistry()
    registry.register(
        ToolDefinition(
            spec=ToolSpec(
                name="test_tool", description="tool", read_scope="public", timeout_seconds=0.05
            ),
            input_model=Input,
            output_model=Output,
            handler=handler,
        )
    )
    executor = ToolExecutor(registry)
    task = asyncio.create_task(executor.execute("test_tool", {"value": 1}, context()))
    await asyncio.wait_for(started.wait(), 1)
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task
    assert closed.is_set()
    result = await executor.execute("test_tool", {"value": 1}, context())
    assert result.error_code == "TOOL_TIMEOUT" and not result.ok


def test_write_tools_cannot_be_enabled_or_skip_confirmation() -> None:
    with pytest.raises(ValueError):
        ToolSpec(
            name="test_tool",
            description="tool",
            read_scope="admin",
            allowed_roles=frozenset({"system_admin"}),
            side_effect="write",
        )
