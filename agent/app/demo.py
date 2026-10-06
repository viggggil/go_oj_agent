"""明确启用的本地持久化 demo；不开放匿名 HTTP Chat。"""

import argparse
import asyncio
import logging
from uuid import UUID, uuid4

from app.core.database import DatabaseProbe
from app.core.logging import configure_logging
from app.core.runtime_config import DemoConfigReader
from app.core.settings import ConfigurationError, Settings, load_settings
from app.graphs.langgraph_runtime import LangGraphRuntime
from app.graphs.runtime import FakeModelClient, FakeRuntime
from app.graphs.service import RunService
from app.models.runtime import ChatRequest, Principal
from app.storage.repository import AgentStore


async def run_demo(settings: Settings, request: ChatRequest, principal: Principal) -> None:
    reader = DemoConfigReader(settings)
    database = DatabaseProbe(settings)
    try:
        if database.engine is None or await database.check() != "ready":
            raise ConfigurationError("Demo requires the Agent database and applied migrations")
        store = AgentStore(database.engine)
        runtime = (
            LangGraphRuntime(FakeModelClient())
            if settings.runtime_mode == "langgraph_fake"
            else FakeRuntime()
        )
        service = RunService(store, runtime, reader)
        # Demo 也按单实例启动：不与其他 Agent/demo 进程共享同一数据库运行。
        await service.initialize()
        accepted = await service.accept(request, principal)
        completed = False
        async with accepted:
            async for event in accepted.events:
                print(event.model_dump_json(), flush=True)
                completed = event.type == "done"
        if not completed:
            raise ConfigurationError("Demo run did not complete")
    finally:
        await database.close()


def main() -> int:
    parser = argparse.ArgumentParser(description="Agent PR2 本地 Fake Runtime 演示")
    parser.add_argument("--message", required=True)
    parser.add_argument("--user-id", type=int, required=True, help="仅供本机演示的逻辑用户 ID")
    parser.add_argument("--conversation-id", type=UUID)
    arguments = parser.parse_args()
    try:
        settings = load_settings()
        configure_logging(settings.log_level)
        request = ChatRequest(message=arguments.message, conversation_id=arguments.conversation_id)
        principal = Principal(user_id=arguments.user_id, request_id=str(uuid4()))
        asyncio.run(run_demo(settings, request, principal))
        return 0
    except Exception as exc:
        # CLI 只输出稳定错误类型；SQL/驱动错误可能附带内容和凭据。
        configure_logging("ERROR")
        logging.getLogger(__name__).error(
            "Agent demo failed",
            extra={"error_code": "AGENT_DEMO_FAILED", "error_type": type(exc).__name__},
        )
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
