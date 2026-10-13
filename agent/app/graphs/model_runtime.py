"""真实 Direct Answer 的 LangGraph 编排；RunService 保留持久化与终态职责。"""

import asyncio
import json
from collections.abc import AsyncGenerator
from dataclasses import dataclass
from typing import Any, TypedDict

from langgraph.config import get_stream_writer
from langgraph.graph import END, START, StateGraph
from langgraph.graph.state import CompiledStateGraph
from langsmith import tracing_context

from app.clients.model import ResponsesClient
from app.graphs.direct_answer import CredentialReader, DirectAnswer, PreparedAnswer
from app.graphs.tool_loop import ToolSession, tool_catalog
from app.models.provider import ModelFailure
from app.models.runtime import AgentState, StreamEvent
from app.tools.registry import ToolExecutor, ToolRegistry


class DirectState(TypedDict, total=False):
    agent_state: AgentState
    prepared: PreparedAnswer
    answer: str
    session: ToolSession | None
    current: PreparedAnswer


@dataclass(frozen=True)
class StreamItem:
    # 仅在 custom 流中传递，不进入图状态或配置快照。
    event: StreamEvent
    consumed: asyncio.Event


def build_direct_graph(
    answerer: DirectAnswer,
    registry: ToolRegistry | None = None,
    *,
    tool_max_result_bytes: int = 256_000,
    tool_timeout_seconds: float = 5,
) -> CompiledStateGraph[DirectState, Any, Any, Any]:
    executor = ToolExecutor(registry) if registry is not None else None

    def prepare(state: DirectState) -> DirectState:
        value = state["agent_state"]
        react = value.config_snapshot.get("execution_mode") == "react"
        tools = tool_catalog(value, registry) if react else ()
        prepared = (
            answerer.prepare(value, tool_bytes=len(json.dumps(tools, ensure_ascii=False).encode()))
            if tools
            else answerer.prepare(value)
        )
        session = (
            ToolSession(
                tools=tools,
                input_items=[
                    *(item.model_dump() for item in prepared.history),
                    {"role": "user", "content": value.user_message},
                ],
            )
            if react
            else None
        )
        return {"prepared": prepared, "session": session}

    def check_budget(state: DirectState) -> DirectState:
        session = state["session"]
        current = session.round_config(state["prepared"]) if session else state["prepared"]
        answerer.check_budget(current)
        return {"current": current}

    async def generate(state: DirectState) -> DirectState:
        agent_state = state["agent_state"]
        writer = get_stream_writer()

        async def emit(event: StreamEvent) -> None:
            consumed = asyncio.Event()
            writer(StreamItem(event, consumed))
            # LangGraph 的 custom writer 自身不提供背压；等消费者继续请求再读取上游。
            await consumed.wait()

        await emit(StreamEvent.thinking(agent_state, "正在生成回答", 1))
        sequence = 1
        parts: list[str] = []
        session = state["session"]
        stream = (
            answerer.stream(
                agent_state,
                state["current"],
                input_items=session.input_items,
                tools=session.tools,
                result=session.result,
                accumulated_input=session.spent_input,
                accumulated_output=session.spent_output,
            )
            if session
            else answerer.stream(agent_state, state["current"])
        )
        try:
            async for text in stream:
                sequence += 1
                parts.append(text)
                await emit(StreamEvent.token(agent_state, text, sequence))
        except asyncio.CancelledError as error:
            # Provider 总超时可能在背压等待中触发；让内层 timeout 区分自身超时与外部取消。
            await stream.athrow(error)
            raise
        finally:
            # 取消可能发生在 emit 等待确认时，显式关闭已暂停的模型生成器。
            await stream.aclose()
        if session:
            session.spent_input += session.result.spent_input
            session.spent_output += session.result.spent_output
        return {"answer": "".join(parts)}

    def next_step(state: DirectState) -> str:
        session = state["session"]
        return "tools" if session and session.result.calls else "validate_answer"

    async def tools(state: DirectState) -> DirectState:
        session = state["session"]
        assert session is not None
        # 结果正文只发回模型；客户端只收到短状态，不接收工具参数或业务原始数据。
        consumed = asyncio.Event()
        get_stream_writer()(
            StreamItem(StreamEvent.thinking(state["agent_state"], "正在读取相关信息", 1), consumed)
        )
        await consumed.wait()
        await session.execute(
            state["agent_state"],
            state["prepared"],
            executor,
            tool_max_result_bytes,
            tool_timeout_seconds,
        )
        return {}

    def validate_answer(state: DirectState) -> DirectState:
        answerer.validate_answer(state["agent_state"], state["answer"])
        return {}

    graph = StateGraph(DirectState)
    graph.add_node("prepare", prepare)
    graph.add_node("check_budget", check_budget)
    graph.add_node("generate", generate)
    graph.add_node("validate_answer", validate_answer)
    graph.add_node("tools", tools)
    graph.add_edge(START, "prepare")
    graph.add_edge("prepare", "check_budget")
    graph.add_edge("check_budget", "generate")
    graph.add_conditional_edges(
        "generate", next_step, {"tools": "tools", "validate_answer": "validate_answer"}
    )
    graph.add_edge("tools", "check_budget")
    graph.add_edge("validate_answer", END)
    # 不启用 checkpointer、节点自动重试或持久化恢复。
    return graph.compile()


class ModelRuntime:
    def __init__(
        self,
        client: ResponsesClient,
        credentials: CredentialReader,
        registry: ToolRegistry | None = None,
        *,
        tool_max_result_bytes: int = 256_000,
        tool_timeout_seconds: float = 5,
    ) -> None:
        self._graph = build_direct_graph(
            DirectAnswer(client, credentials),
            registry,
            tool_max_result_bytes=tool_max_result_bytes,
            tool_timeout_seconds=tool_timeout_seconds,
        )

    async def run(self, state: AgentState) -> AsyncGenerator[StreamEvent, None]:
        state.model_summary = None
        stream = self._graph.astream(
            {"agent_state": state}, stream_mode="custom", config={"recursion_limit": 128}
        )
        sequence = 0
        try:
            while True:
                try:
                    # 禁止本机 LangSmith 环境变量把消息、配置或模型输出上传到外部。
                    with tracing_context(enabled=False):
                        item = await anext(stream)
                except StopAsyncIteration:
                    break
                if not isinstance(item, StreamItem):
                    raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE")
                sequence += 1
                yield item.event.model_copy(update={"sequence": sequence})
                item.consumed.set()
            # 只有图正常经过结果验证并到达 END 才给 RunService 完成信号。
            yield StreamEvent.done(state, sequence + 1)
        finally:
            with tracing_context(enabled=False):
                close = getattr(stream, "aclose", None)
                if close is not None:
                    await close()
