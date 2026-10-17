"""统一 ReAct Runtime；RunService 保留持久化与终态职责。"""

from collections.abc import AsyncGenerator

from langsmith import tracing_context

from app.clients.model import ResponsesClient
from app.graphs.direct_answer import CredentialReader, DirectAnswer
from app.graphs.react import build_react_graph
from app.graphs.react_state import ReActContext, StreamItem
from app.models.provider import ModelFailure
from app.models.runtime import AgentState, StreamEvent
from app.tools.registry import ToolRegistry

# 兼容历史导入名称，所有调用仍构建同一张 ReAct 图。
build_direct_graph = build_react_graph


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
        self._graph = build_react_graph(
            DirectAnswer(client, credentials),
            registry,
            tool_max_result_bytes=tool_max_result_bytes,
            tool_timeout_seconds=tool_timeout_seconds,
        )

    async def run(self, state: AgentState) -> AsyncGenerator[StreamEvent, None]:
        state.model_summary = None
        stream = self._graph.astream(
            {"agent_state": state},
            stream_mode="custom",
            config={"recursion_limit": 128},
            context=ReActContext(),
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
