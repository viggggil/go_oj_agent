"""所有真实模型请求共用显式 ReAct 图，包括零工具回答。"""

import asyncio
import hashlib
import json
from dataclasses import replace
from datetime import UTC
from time import monotonic
from typing import Any, Literal

from langgraph.config import get_stream_writer
from langgraph.graph import END, START, StateGraph
from langgraph.graph.state import CompiledStateGraph
from langgraph.runtime import Runtime
from pydantic import ValidationError

from app.graphs.direct_answer import DirectAnswer
from app.graphs.react_state import (
    ReActContext,
    ReActDecision,
    ReActState,
    StreamItem,
    ToolObservation,
)
from app.graphs.tool_loop import parse_arguments, tool_catalog
from app.models.provider import (
    KnowledgeAttempt,
    KnowledgeSource,
    ModelFailure,
    ModelResponse,
    ToolTraceStep,
)
from app.models.runtime import StreamEvent
from app.storage.repository import utc_now
from app.tools.registry import ToolContext, ToolExecutor, ToolPermissionDenied, ToolRegistry


async def emit(event: StreamEvent) -> None:
    consumed = asyncio.Event()
    get_stream_writer()(StreamItem(event, consumed))
    await consumed.wait()


def remaining_seconds(state: ReActState) -> float:
    deadline = state["agent_state"].deadline_at
    now = utc_now()
    if deadline.tzinfo is not None:
        now = now.replace(tzinfo=UTC)
    remaining = (deadline - now).total_seconds()
    if remaining <= 0:
        raise ModelFailure("AGENT_MODEL_TIMEOUT")
    return remaining


def build_react_graph(
    answerer: DirectAnswer,
    registry: ToolRegistry | None = None,
    *,
    tool_max_result_bytes: int = 256_000,
    tool_timeout_seconds: float = 5,
) -> CompiledStateGraph[ReActState, Any, Any, Any]:
    executor = ToolExecutor(registry) if registry else None

    async def prepare(state: ReActState, runtime: Runtime[ReActContext]) -> ReActState:
        value = state["agent_state"]
        tools = tool_catalog(value, registry)
        prepared = answerer.prepare(
            value,
            tool_bytes=len(json.dumps(tools, ensure_ascii=False).encode()) if tools else 0,
            has_tools=bool(tools),
        )
        runtime.context.provider_items = [
            *(item.model_dump() for item in prepared.history),
            {"role": "user", "content": value.user_message},
        ]
        await emit(StreamEvent.progress(value, "preparing", "正在准备上下文", 1))
        return {
            "prepared": prepared,
            "tools": tools,
            "step_count": 0,
            "model_call_count": 0,
            "tool_call_count": 0,
            "spent_input": 0,
            "spent_output": 0,
            "extra_input": 0,
            "seen_call_ids": set(),
            "seen_tool_fingerprints": set(),
            "observations": [],
            "evidence": [],
            "tool_steps": [],
            "final_answer": "",
        }

    def check_budget(state: ReActState) -> ReActState:
        remaining_seconds(state)
        prepared = state["prepared"]
        if state["model_call_count"] >= prepared.budget.max_model_calls:
            raise ModelFailure("AGENT_MODEL_CALL_LIMIT")
        remaining = prepared.budget.max_output_tokens - state["spent_output"]
        if remaining <= 0:
            raise ModelFailure("AGENT_MODEL_OUTPUT_LIMIT")
        current = replace(
            prepared,
            input_estimate=prepared.input_estimate + state["extra_input"],
            max_output=min(prepared.max_output, remaining),
        )
        answerer.check_budget(current)
        if state["spent_input"] + current.input_estimate > prepared.budget.max_input_tokens:
            raise ModelFailure("AGENT_MODEL_INPUT_LIMIT")
        return {"current": current}

    async def reason(state: ReActState, runtime: Runtime[ReActContext]) -> ReActState:
        value = state["agent_state"]
        await emit(StreamEvent.progress(value, "analyzing", "正在分析任务并选择下一步", 1))
        response = ModelResponse()
        parts: list[str] = []
        stream = answerer.stream(
            value,
            state["current"],
            input_items=runtime.context.provider_items,
            tools=state["tools"],
            result=response,
            accumulated_input=state["spent_input"],
            accumulated_output=state["spent_output"],
        )
        try:
            async for text in stream:
                # 工具轮次可能在正式调用前产生不可信正文；确认 Finish 后才能发送。
                parts.append(text)
                # 无工具时唯一合法决策为 Finish，保留即时 token 流和背压。
                if not state["tools"]:
                    if len(parts) == 1:
                        await emit(StreamEvent.progress(value, "answering", "正在生成最终回答", 1))
                    await emit(StreamEvent.token(value, text, 1))
        except asyncio.CancelledError as error:
            await stream.athrow(error)
            raise
        finally:
            await stream.aclose()
        runtime.context.continuation_items = response.continuation_items
        response.continuation_items = ()
        step = state["step_count"] + 1
        if response.reasoning_summary:
            await emit(
                StreamEvent.reasoning_summary(value, response.reasoning_summary, 1, step=step)
            )
        return {
            "model_response": response,
            "decision": ReActDecision(
                "action" if response.calls else "finish",
                answer="".join(parts),
                calls=response.calls,
            ),
            "step_count": step,
            "model_call_count": len(value.model_summary.attempts) if value.model_summary else step,
            "spent_input": state["spent_input"] + response.spent_input,
            "spent_output": state["spent_output"] + response.spent_output,
            "token_parts": tuple(parts),
        }

    async def validate_action(state: ReActState) -> ReActState:
        remaining_seconds(state)
        calls = state["decision"].calls
        if executor is None or registry is None or not calls:
            raise ModelFailure("AGENT_MODEL_UNSUPPORTED_OUTPUT")
        if state["tool_call_count"] + len(calls) > state["prepared"].budget.max_tool_calls:
            raise ModelFailure("AGENT_TOOL_CALL_LIMIT")
        allowed = frozenset(tool["name"] for tool in state["tools"])
        ids = set(state["seen_call_ids"])
        fingerprints = set(state["seen_tool_fingerprints"])
        parsed_calls = []

        async def reject(call: Any, code: str, failure: str, *, invalid: bool = False) -> None:
            value = state["agent_state"]
            step = state["tool_call_count"] + 1
            trace = ToolTraceStep(
                run_id=str(value.run_id),
                step=step,
                call_id=call.call_id,
                tool_name=call.name,
                parameter_validation="invalid" if invalid else "rejected",
                status="rejected",
                error_code=code,
                duration_ms=0,
                summary="调用未通过校验：" + code,
            )
            if value.model_summary is not None:
                value.model_summary.tool_steps.append(trace)
            # 重复 ID 不能再次生成同一公开开始/结果事件对。
            if call.call_id not in state["seen_call_ids"]:
                await emit(
                    StreamEvent.tool_call(
                        value,
                        step=step,
                        call_id=call.call_id,
                        tool=call.name,
                        label="校验工具请求",
                        sequence=1,
                    )
                )
                await emit(
                    StreamEvent.tool_result(
                        value,
                        step=step,
                        call_id=call.call_id,
                        tool=call.name,
                        status="rejected",
                        duration_ms=0,
                        summary=trace.summary,
                        sequence=1,
                    )
                )
            raise ModelFailure(failure)

        for call in calls:
            if call.name not in allowed:
                await reject(call, "TOOL_PERMISSION_DENIED", "AGENT_MODEL_UNSUPPORTED_OUTPUT")
            if call.call_id in ids:
                await reject(call, "TOOL_DUPLICATE_CALL_ID", "AGENT_MODEL_UNSUPPORTED_OUTPUT")
            try:
                parsed = parse_arguments(call)
            except ModelFailure:
                await reject(
                    call, "TOOL_INVALID_ARGUMENT", "AGENT_MODEL_INVALID_RESPONSE", invalid=True
                )
            definition = registry.get(call.name)
            try:
                ToolExecutor._authorize(
                    definition.spec,
                    ToolContext(
                        principal=state["agent_state"].principal,
                        remaining_seconds=1,
                        allowed_tools=allowed,
                    ),
                )
                definition.input_model.model_validate(parsed)
            except ValidationError:
                await reject(
                    call, "TOOL_INVALID_ARGUMENT", "AGENT_MODEL_UNSUPPORTED_OUTPUT", invalid=True
                )
            except ToolPermissionDenied:
                await reject(call, "TOOL_PERMISSION_DENIED", "AGENT_MODEL_UNSUPPORTED_OUTPUT")
            fingerprint = hashlib.sha256(
                json.dumps([call.name, parsed], sort_keys=True, ensure_ascii=False).encode()
            ).hexdigest()
            if fingerprint in fingerprints:
                await reject(call, "TOOL_REPEATED_CALL", "AGENT_TOOL_CALL_LIMIT")
            ids.add(call.call_id)
            fingerprints.add(fingerprint)
            parsed_calls.append(parsed)
        return {
            "seen_call_ids": ids,
            "seen_tool_fingerprints": fingerprints,
            "pending_arguments": parsed_calls,
        }

    async def act(state: ReActState) -> ReActState:
        assert executor is not None
        value = state["agent_state"]
        observations: list[ToolObservation] = []
        traces = list(state["tool_steps"])
        allowed = frozenset(tool["name"] for tool in state["tools"])
        for call, parsed in zip(state["decision"].calls, state["pending_arguments"], strict=True):
            remaining_seconds(state)
            step = state["tool_call_count"] + len(observations) + 1
            await emit(StreamEvent.progress(value, "executing_tool", "正在读取授权信息", 1))
            await emit(
                StreamEvent.tool_call(
                    value,
                    step=step,
                    call_id=call.call_id,
                    tool=call.name,
                    label=call.name,
                    sequence=1,
                )
            )
            started = monotonic()
            result = await executor.execute(
                call.name,
                parsed,
                ToolContext(
                    principal=value.principal,
                    remaining_seconds=min(60, tool_timeout_seconds, remaining_seconds(state)),
                    allowed_tools=allowed,
                    state=value,
                ),
            )
            duration = min(300_000, int((monotonic() - started) * 1000))
            status: Literal["succeeded", "failed"] = "succeeded" if result.ok else "failed"
            summary = "已获取授权信息" if result.ok else "读取失败：" + str(result.error_code)
            output = result.model_dump_json()
            if len(output.encode()) > tool_max_result_bytes:
                raise ModelFailure("AGENT_MODEL_OUTPUT_LIMIT")
            # 原始业务结果仅供下一轮 Provider 请求使用。
            observations.append(
                ToolObservation(call.call_id, call.name, result.ok, result.error_code, output)
            )
            trace = ToolTraceStep(
                run_id=str(value.run_id),
                step=step,
                call_id=call.call_id,
                tool_name=call.name,
                parameter_validation="valid",
                status=status,
                error_code=result.error_code,
                duration_ms=duration,
                summary=summary,
            )
            traces.append(trace)
            if value.model_summary is not None:
                if call.name not in value.model_summary.tools:
                    value.model_summary.tools.append(call.name)
                value.model_summary.tool_steps.append(trace)
                if call.name == "retrieve_knowledge":
                    value.model_summary.knowledge.append(
                        KnowledgeAttempt(
                            call_id=call.call_id,
                            ok=result.ok,
                            error_code=result.error_code,
                            sources=[
                                KnowledgeSource.model_validate(
                                    {k: v for k, v in source.items() if k != "text"}
                                )
                                for source in (result.data or {}).get("matches", [])
                            ]
                            if result.ok
                            else [],
                        )
                    )
            await emit(
                StreamEvent.tool_result(
                    value,
                    step=step,
                    call_id=call.call_id,
                    tool=call.name,
                    status=status,
                    duration_ms=duration,
                    summary=summary,
                    sequence=1,
                )
            )
        return {
            "observations": observations,
            "tool_steps": traces,
            "tool_call_count": state["tool_call_count"] + len(observations),
        }

    def observe(state: ReActState, runtime: Runtime[ReActContext]) -> ReActState:
        additions = list(runtime.context.continuation_items)
        if not additions:
            additions = [call.input_item() for call in state["decision"].calls]
        additions.extend(
            {"type": "function_call_output", "call_id": item.call_id, "output": item.output}
            for item in state["observations"]
        )
        runtime.context.provider_items.extend(additions)
        runtime.context.continuation_items = ()
        return {
            "extra_input": state["extra_input"]
            + sum(len(json.dumps(item, ensure_ascii=False).encode()) + 128 for item in additions),
            "evidence": [
                *state["evidence"],
                *(
                    {"call_id": item.call_id, "tool": item.tool_name}
                    for item in state["observations"]
                    if item.ok
                ),
            ],
            # Provider 续接数据仅保存在请求上下文，不持久化。
            "model_response": ModelResponse(),
        }

    def validate_answer(state: ReActState) -> ReActState:
        answerer.validate_answer(state["agent_state"], state["decision"].answer)
        return {"final_answer": state["decision"].answer}

    async def finalize(state: ReActState) -> ReActState:
        value = state["agent_state"]
        if not state["tools"]:
            return {}
        await emit(StreamEvent.progress(value, "answering", "正在整理最终回答", 1))
        for text in state["token_parts"]:
            if text:
                await emit(StreamEvent.token(value, text, 1))
        return {}

    graph = StateGraph(ReActState, context_schema=ReActContext)
    graph.add_node("prepare", prepare)
    graph.add_node("check_budget", check_budget)
    graph.add_node("reason", reason)
    graph.add_node("validate_action", validate_action)
    graph.add_node("act", act)
    graph.add_node("observe", observe)
    graph.add_node("validate_answer", validate_answer)
    graph.add_node("finalize", finalize)
    graph.add_edge(START, "prepare")
    graph.add_edge("prepare", "check_budget")
    graph.add_edge("check_budget", "reason")
    graph.add_conditional_edges(
        "reason",
        lambda state: state["decision"].kind,
        {"action": "validate_action", "finish": "validate_answer"},
    )
    graph.add_edge("validate_action", "act")
    graph.add_edge("act", "observe")
    graph.add_edge("observe", "check_budget")
    graph.add_edge("validate_answer", "finalize")
    graph.add_edge("finalize", END)
    return graph.compile()
