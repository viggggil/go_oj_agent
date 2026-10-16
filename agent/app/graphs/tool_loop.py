"""只读工具循环的有界会话；图节点保留调度、取消和事件职责。"""

import json
from dataclasses import dataclass, field, replace
from datetime import UTC
from typing import Any

from app.graphs.direct_answer import PreparedAnswer
from app.models.provider import (
    FunctionCall,
    KnowledgeAttempt,
    KnowledgeSource,
    ModelFailure,
    ModelResponse,
)
from app.models.runtime import AgentState
from app.storage.repository import utc_now
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
                "description": spec.description,
                "parameters": definition.input_model.model_json_schema(),
                "strict": False,
            }
        )
    return tuple(tools)


@dataclass
class ToolSession:
    tools: tuple[dict[str, Any], ...]
    input_items: list[dict[str, Any]]
    result: ModelResponse = field(default_factory=ModelResponse)
    spent_input: int = 0
    spent_output: int = 0
    tool_calls: int = 0
    extra_input: int = 0
    seen_call_ids: set[str] = field(default_factory=set)

    def round_config(self, prepared: PreparedAnswer) -> PreparedAnswer:
        remaining = prepared.budget.max_output_tokens - self.spent_output
        if remaining <= 0:
            raise ModelFailure("AGENT_MODEL_OUTPUT_LIMIT")
        value = replace(
            prepared,
            input_estimate=prepared.input_estimate + self.extra_input,
            max_output=min(prepared.max_output, remaining),
        )
        if self.spent_input + value.input_estimate > prepared.budget.max_input_tokens:
            raise ModelFailure("AGENT_MODEL_INPUT_LIMIT")
        if value.input_estimate + value.max_output > prepared.profile.context_window_tokens:
            raise ModelFailure("AGENT_MODEL_INPUT_LIMIT")
        return value

    async def execute(
        self,
        state: AgentState,
        prepared: PreparedAnswer,
        executor: ToolExecutor | None,
        maximum: int,
        timeout_seconds: float,
    ) -> None:
        calls = self.result.calls
        if executor is None or not calls:
            raise ModelFailure("AGENT_MODEL_UNSUPPORTED_OUTPUT")
        allowed = frozenset(tool["name"] for tool in self.tools)
        # 整批先检查，避免重复 call_id 或越权调用导致已执行部分工具。
        if self.tool_calls + len(calls) > prepared.budget.max_tool_calls:
            raise ModelFailure("AGENT_TOOL_CALL_LIMIT")
        if any(call.name not in allowed or call.call_id in self.seen_call_ids for call in calls):
            raise ModelFailure("AGENT_MODEL_UNSUPPORTED_OUTPUT")
        arguments = [parse_arguments(call) for call in calls]
        for call, parsed in zip(calls, arguments, strict=True):
            deadline = state.deadline_at
            now = utc_now()
            if deadline.tzinfo is not None:
                now = now.replace(tzinfo=UTC)
            remaining = (deadline - now).total_seconds()
            if remaining <= 0:
                raise ModelFailure("AGENT_MODEL_TIMEOUT")
            self.tool_calls += 1
            self.seen_call_ids.add(call.call_id)
            result = await executor.execute(
                call.name,
                parsed,
                ToolContext(
                    principal=state.principal,
                    remaining_seconds=min(60, timeout_seconds, remaining),
                    allowed_tools=allowed,
                    state=state,
                ),
            )
            if call.name == "retrieve_knowledge" and state.model_summary is not None:
                state.model_summary.knowledge.append(
                    KnowledgeAttempt(
                        call_id=call.call_id,
                        ok=result.ok,
                        error_code=result.error_code,
                        sources=[
                            KnowledgeSource.model_validate(
                                {key: value for key, value in source.items() if key != "text"}
                            )
                            for source in (result.data or {}).get("matches", [])
                        ]
                        if result.ok
                        else [],
                    )
                )
            output = result.model_dump_json()
            if len(output.encode()) > maximum:
                raise ModelFailure("AGENT_MODEL_OUTPUT_LIMIT")
            for item in (
                call.input_item(),
                {"type": "function_call_output", "call_id": call.call_id, "output": output},
            ):
                self.input_items.append(item)
                self.extra_input += len(json.dumps(item, ensure_ascii=False).encode()) + 128
        self.result = ModelResponse()


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
