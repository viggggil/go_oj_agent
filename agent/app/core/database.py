"""只检查 Agent 自有数据库连接；不创建表或访问其他服务数据。"""

import asyncio
import logging
import math
from typing import Literal, Protocol

from sqlalchemy import text
from sqlalchemy.ext.asyncio import AsyncEngine, create_async_engine

from app.core.settings import Settings

DatabaseStatus = Literal["ready", "not_configured", "unavailable"]
logger = logging.getLogger(__name__)


class ReadinessProbe(Protocol):
    async def check(self) -> DatabaseStatus: ...

    async def close(self) -> None: ...


class DatabaseProbe:
    def __init__(self, settings: Settings) -> None:
        self._timeout = settings.readiness_timeout_seconds
        self._engine: AsyncEngine | None = None
        if settings.database_url is not None:
            self._engine = create_async_engine(
                settings.database_url.get_secret_value(),
                echo=False,
                hide_parameters=True,
                pool_size=2,
                max_overflow=0,
                pool_timeout=self._timeout,
                pool_pre_ping=True,
                connect_args={"connect_timeout": math.ceil(self._timeout)},
            )

    @property
    def engine(self) -> AsyncEngine | None:
        """复用 lifespan 管理的连接池，存储仓库不会再建立第二个池。"""
        return self._engine

    async def check(self) -> DatabaseStatus:
        if self._engine is None:
            return "not_configured"
        try:
            async with asyncio.timeout(self._timeout):
                async with self._engine.connect() as connection:
                    result = await connection.execute(text("SELECT 1"))
                    return "ready" if result.scalar_one() == 1 else "unavailable"
        except Exception as exc:
            logger.warning(
                "Database readiness check failed",
                extra={
                    "error_code": "AGENT_DATABASE_UNAVAILABLE",
                    "error_type": type(exc).__name__,
                },
            )
            return "unavailable"

    async def close(self) -> None:
        if self._engine is not None:
            await self._engine.dispose()
