"""仅为本地 Fake 部署补齐示例配置；保留所有已有配置版本与状态。"""

import asyncio
import json
import os
from pathlib import Path
from typing import cast

from app.core.database import DatabaseProbe
from app.core.settings import load_settings
from app.models.configuration import (
    ConfigError,
    ConfigKind,
    ConfigWrite,
    ModelProfileConfig,
)
from app.storage.configuration import ConfigurationStore
from app.storage.schema import agent_config_resources as resources
from app.tools.registry import ToolRegistry
from sqlalchemy import select


async def main() -> None:
    settings = load_settings()
    if (
        os.getenv("AGENT_BOOTSTRAP_DEMO", "false").lower() != "true"
        or settings.environment != "development"
        or settings.runtime_mode not in {"fake", "langgraph_fake"}
        or settings.config_mode != "database"
        or settings.default_agent_key != "learning_assistant"
    ):
        return
    database = DatabaseProbe(settings)
    try:
        assert database.engine is not None
        store = ConfigurationStore(database.engine, ToolRegistry())
        document = json.loads(Path("/bootstrap/learning-assistant.json").read_text())
        # 即使管理员已归档/停用默认 Agent，也不会在重启时重建或恢复。
        async with database.engine.connect() as connection:
            exists = await connection.scalar(
                select(resources.c.id).where(
                    resources.c.kind == "agent",
                    resources.c.config_key == "learning_assistant",
                )
            )
        if exists:
            print("默认 Agent 配置已存在，保留版本与状态。", flush=True)
            return
        identifiers: dict[str, str] = {}
        for name, kind in (
            ("prompt", "prompt"),
            ("skill_prompt", "prompt"),
            ("model", "model"),
            ("skill", "skill"),
            ("agent", "agent"),
        ):
            item = document[name]
            async with database.engine.connect() as connection:
                existing = (
                    await connection.execute(
                        select(resources.c.current_id, resources.c.disabled).where(
                            resources.c.kind == kind,
                            resources.c.config_key == item["key"],
                        )
                    )
                ).first()
            if existing is not None:
                if existing.disabled or existing.current_id is None:
                    print("示例依赖已停用或归档，保留状态并跳过配置准备。", flush=True)
                    return
                version = await store.get_version(existing.current_id)
                if version.archived or (
                    isinstance(version.content, ModelProfileConfig)
                    and version.content.provider != "fake"
                ):
                    print("示例依赖已有自定义状态，保留配置并跳过准备。", flush=True)
                    return
                identifiers[name] = version.id
                continue
            content = dict(item["content"])
            if kind == "skill":
                content["prompt_id"] = identifiers["skill_prompt"]
            elif kind == "agent":
                content.update(
                    prompt_id=identifiers["prompt"],
                    skill_ids=[identifiers["skill"]],
                    default_skill_id=identifiers["skill"],
                    model_profile_id=identifiers["model"],
                )
            version = await store.create_or_replace(
                ConfigWrite(
                    kind=cast(ConfigKind, kind),
                    key=item["key"],
                    actor_id=1,
                    request_id=f"compose.demo.v1.{name}",
                ),
                content,
            )
            identifiers[name] = version.id
        print("本地学习助手已准备，回答使用明确标记的 Fake runtime。", flush=True)
    except ConfigError:
        # 管理员已有配置与示例不兼容时保留配置，允许管理服务启动以便调整。
        print("示例配置与已有配置不兼容，保留数据并跳过配置准备。", flush=True)
    finally:
        await database.close()


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except Exception as exc:  # noqa: BLE001 -- 启动失败必须脱敏，不输出原始驱动异常。
        # 数据库驱动异常可能包含敏感参数，只输出稳定类型。
        print(f"本地 Agent 配置准备失败：{type(exc).__name__}", flush=True)
        raise SystemExit(1) from None
