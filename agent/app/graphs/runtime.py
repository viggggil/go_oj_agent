"""Runtime 协议和不依赖模型的 Fake Runtime。"""

import asyncio
import json
import logging
from collections.abc import AsyncGenerator, AsyncIterator
from datetime import UTC, datetime
from typing import Protocol

from app.models.runtime import AgentState, StreamEvent
from app.tools.registry import ToolContext, ToolExecutor

logger = logging.getLogger(__name__)


class AgentRuntime(Protocol):
    def run(self, state: AgentState) -> AsyncIterator[StreamEvent]: ...


class ModelClient(Protocol):
    """后续真实 Provider 的适配入口，目前只注入 Fake 实现。"""

    async def answer(self, state: AgentState) -> str: ...


class FakeModelClient:
    def __init__(self, tool_executor: ToolExecutor | None = None) -> None:
        self._tool_executor = tool_executor

    async def answer(self, state: AgentState) -> str:
        await asyncio.sleep(0)
        tool_answer = await execute_demo_tool(state, self._tool_executor)
        if tool_answer is not None:
            return tool_answer
        return "【演示回答】Fake ModelClient 已完成，尚未接入真实模型。"


class FakeRuntime:
    """测试和本地演示用 Runtime；仅接受显式 /tool 命令调用已注册 Tool。"""

    def __init__(
        self,
        answer: str = "【演示回答】Fake Runtime 已完成，尚未接入真实模型。",
        tool_executor: ToolExecutor | None = None,
    ) -> None:
        self._answer = answer
        self._tool_executor = tool_executor

    async def run(self, state: AgentState) -> AsyncGenerator[StreamEvent, None]:
        yield StreamEvent.thinking(state, "fake_runtime_started", 1)
        await asyncio.sleep(0)
        answer = await execute_demo_tool(state, self._tool_executor) or self._answer
        yield StreamEvent.token(state, answer, 2)
        yield StreamEvent.done(state, 3)


async def execute_demo_tool(state: AgentState, executor: ToolExecutor | None) -> str | None:
    request = parse_demo_tool_request(state.user_message)
    if request is None or executor is None:
        return None
    name, arguments = request
    deadline = state.deadline_at
    if deadline.tzinfo is None:
        # MySQL DATETIME is returned without timezone information; the store
        # uses UTC-naive values while tests and external callers may provide
        # timezone-aware datetimes.
        deadline = deadline.replace(tzinfo=UTC)
    remaining = max(0.001, (deadline - datetime.now(UTC)).total_seconds())
    result = await executor.execute(
        name,
        arguments,
        ToolContext(
            principal=state.principal,
            remaining_seconds=min(60.0, remaining),
            allowed_tools=frozenset(state.allowed_tools),
        ),
    )
    if not result.ok:
        return f"【演示工具失败】{result.error_code or 'TOOL_FAILED'}"
    try:
        rendered = "【演示工具结果】" + json.dumps(
            result.data, ensure_ascii=False, separators=(",", ":")
        )
    except Exception as error:
        logger.warning(
            "Tool result rendering failed",
            extra={"tool_name": name, "error_type": type(error).__name__},
        )
        return "【演示工具失败】TOOL_FAILED"
    maximum = int(state.config_snapshot.get("budget", {}).get("max_output_chars", 8_000))
    return rendered[: max(1, maximum)]


def parse_demo_tool_request(message: str) -> tuple[str, dict[str, object]] | None:
    """解析开发 Fake 的显式工具命令；自然语言请求不会隐式触发 Tool。"""
    if not message.startswith("/tool "):
        return None
    parts = message.split(maxsplit=2)
    if len(parts) < 2:
        return None
    name = parts[1]
    if len(parts) == 2:
        return name, {}
    try:
        arguments = json.loads(parts[2])
    except (TypeError, ValueError):
        return name, {"__invalid_json__": parts[2]}
    return name, arguments if isinstance(arguments, dict) else {"__invalid_arguments__": arguments}
