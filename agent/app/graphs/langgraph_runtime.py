"""最小 LangGraph 入口；图中使用可注入 responder，不内置真实模型。"""

from collections.abc import AsyncGenerator
from typing import Any, TypedDict

from langgraph.graph import END, START, StateGraph
from langgraph.graph.state import CompiledStateGraph
from langsmith import tracing_context

from app.graphs.runtime import ModelClient
from app.models.runtime import AgentState, StreamEvent


class GraphState(TypedDict, total=False):
    agent_state: AgentState
    events: list[StreamEvent]


def build_graph(responder: ModelClient) -> CompiledStateGraph[GraphState, Any, Any, Any]:
    async def thinking_node(state: GraphState) -> GraphState:
        agent_state = state["agent_state"]
        return {"events": [StreamEvent.thinking(agent_state, "langgraph_started", 1)]}

    async def response_node(state: GraphState) -> GraphState:
        agent_state = state["agent_state"]
        answer = await responder.answer(agent_state)
        return {
            "events": [
                StreamEvent.token(agent_state, answer, 2),
                StreamEvent.done(agent_state, 3),
            ]
        }

    graph = StateGraph(GraphState)
    graph.add_node("thinking", thinking_node)
    graph.add_node("response", response_node)
    graph.add_edge(START, "thinking")
    graph.add_edge("thinking", "response")
    graph.add_edge("response", END)
    return graph.compile()


class LangGraphRuntime:
    def __init__(self, responder: ModelClient) -> None:
        self._graph = build_graph(responder)

    async def run(self, state: AgentState) -> AsyncGenerator[StreamEvent, None]:
        initial: GraphState = {"agent_state": state}
        stream = self._graph.astream(initial, stream_mode="updates")
        try:
            while True:
                # PR2 不启用外部 Trace：即使本机环境开启 LangSmith，也不上传用户消息。
                try:
                    with tracing_context(enabled=False):
                        update = await anext(stream)
                except StopAsyncIteration:
                    return
                for node_update in update.values():
                    for event in node_update.get("events", []):
                        yield event
        finally:
            close = getattr(stream, "aclose", None)
            if close is not None:
                await close()
