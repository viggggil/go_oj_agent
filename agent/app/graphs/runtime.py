"""Runtime 协议和不依赖模型的 Fake Runtime。"""

import asyncio
from collections.abc import AsyncGenerator, AsyncIterator
from typing import Protocol

from app.models.runtime import AgentState, StreamEvent


class AgentRuntime(Protocol):
    def run(self, state: AgentState) -> AsyncIterator[StreamEvent]: ...


class ModelClient(Protocol):
    """后续真实 Provider 的适配入口，目前只注入 Fake 实现。"""

    async def answer(self, state: AgentState) -> str: ...


class FakeModelClient:
    async def answer(self, state: AgentState) -> str:
        await asyncio.sleep(0)
        return "【演示回答】Fake ModelClient 已完成，尚未接入真实模型。"


class FakeRuntime:
    """测试和本地演示用 Runtime，不代表真实模型或业务 Tool 已被调用。"""

    def __init__(self, answer: str = "【演示回答】Fake Runtime 已完成，尚未接入真实模型。") -> None:
        self._answer = answer

    async def run(self, state: AgentState) -> AsyncGenerator[StreamEvent, None]:
        yield StreamEvent.thinking(state, "fake_runtime_started", 1)
        await asyncio.sleep(0)
        yield StreamEvent.token(state, self._answer, 2)
        yield StreamEvent.done(state, 3)
