"""统一工作流的授权工具目录及严格参数解码。"""

import json
from typing import Any

from app.models.provider import (
    FunctionCall,
    ModelFailure,
)
from app.models.runtime import AgentState
from app.tools.registry import ToolContext, ToolExecutor, ToolPermissionDenied, ToolRegistry


def tool_catalog(state: AgentState, registry: ToolRegistry | None) -> tuple[dict[str, Any], ...]:
    if registry is None or state.config_snapshot["budget"]["max_tool_calls"] == 0:
        return ()
    tools = []
    for name in sorted(set(state.allowed_tools) & set(registry.names())):
        definition = registry.get(name)
        spec = definition.spec
        if spec.side_effect != "read":
            continue
        context = ToolContext(
            principal=state.principal, remaining_seconds=1, allowed_tools=frozenset({name})
        )
        try:
            ToolExecutor._authorize(spec, context)
        except ToolPermissionDenied:
            continue
        tools.append(
            {
                "type": "function",
                "name": name,
                "description": registry.description(name),
                "parameters": definition.input_model.model_json_schema(),
                "strict": False,
            }
        )
    return tuple(tools)


def parse_arguments(call: FunctionCall) -> dict[str, Any]:
    def unique(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
        result: dict[str, Any] = {}
        for name, value in pairs:
            if name in result:
                raise ValueError
            result[name] = value
        return result

    def invalid_constant(_: str) -> Any:
        raise ValueError

    try:
        value = json.loads(
            call.arguments,
            object_pairs_hook=unique,
            parse_constant=invalid_constant,
        )
        if not isinstance(value, dict):
            raise ValueError
    except (ValueError, RecursionError):
        raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE") from None
    return value
