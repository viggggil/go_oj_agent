"""开发部署的单 su8 Provider 准备；保留已有资源及停用/归档状态。"""

import asyncio
from pathlib import Path
from typing import Any

from pydantic import SecretStr
from sqlalchemy import select

from app.core.credentials import CredentialCipher
from app.core.database import DatabaseProbe
from app.core.provider_policy import ProviderPolicy
from app.core.settings import Settings
from app.models.configuration import ConfigError, ConfigKind, ConfigWrite, ProviderConfig
from app.storage.configuration import ConfigurationStore, ConfigVersion
from app.storage.credentials import CredentialStore, CredentialWrite
from app.storage.schema import agent_config_resources as resources
from app.tools.registry import ToolRegistry

SU8_MODELS = ("deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4.1-flash")


async def bootstrap_su8(
    settings: Settings, *, key_file: Path = Path("/run/model-keys/su8.key")
) -> None:
    database = DatabaseProbe(settings)
    try:
        engine = database.engine
        assert engine is not None
        cipher = CredentialCipher.from_file(settings.credential_keyring_file)
        credentials = CredentialStore(engine, cipher)
        store = ConfigurationStore(engine, ToolRegistry(), provider_policy=ProviderPolicy(settings))

        async def existing(kind: ConfigKind, key: str) -> ConfigVersion | None:
            async with engine.connect() as connection:
                row = (
                    await connection.execute(
                        select(resources.c.current_id).where(
                            resources.c.kind == kind,
                            resources.c.config_key == key,
                        )
                    )
                ).first()
            if row is None:
                return None
            if row.current_id is None:
                raise ConfigError("AGENT_CONFIGURATION_REFERENCE_INVALID")
            value = await store.get_version(row.current_id)
            if value.disabled or value.archived:
                raise ConfigError("AGENT_CONFIGURATION_REFERENCE_INVALID")
            return value

        async def ensure(kind: ConfigKind, key: str, content: dict[str, Any]) -> ConfigVersion:
            value = await existing(kind, key)
            if value is not None:
                return value
            return await store.create_or_replace(
                ConfigWrite(
                    kind=kind,
                    key=key,
                    actor_id=1,
                    request_id=f"compose.su8.v1.{kind}.{key}",
                ),
                content,
            )

        provider = await existing("provider", "su8")
        if provider is None:

            def read_key() -> bytes:
                with key_file.open("rb") as stream:
                    return stream.read(4097)

            raw = await asyncio.to_thread(read_key)
            if len(raw) > 4096:
                raise ConfigError("AGENT_CREDENTIAL_INVALID")
            credential = await credentials.create(
                CredentialWrite(
                    actor_id=1,
                    request_id="compose.su8.v1.credential",
                ),
                "su8",
                SecretStr(raw.decode().strip()),
            )
            provider = await ensure(
                "provider",
                "su8",
                {
                    "name": "su8",
                    "base_url": "https://www.su8.codes/v1",
                    "credential_id": credential["id"],
                    "text_verbosity_supported": False,
                    "first_token_timeout_seconds": 60,
                    "idle_timeout_seconds": 60,
                    "request_timeout_seconds": 120,
                },
            )
        if not isinstance(provider.content, ProviderConfig):
            raise ConfigError("AGENT_CONFIGURATION_INVALID")
        await credentials.read(str(provider.content.credential_id))
        model_versions = []
        for model in SU8_MODELS:
            model_versions.append(
                await ensure(
                    "model",
                    "su8_" + model.replace("-", "_").replace(".", "_"),
                    {
                        "provider": "responses",
                        "provider_id": provider.id,
                        "model": model,
                        "verbosity": "high",
                        "max_output_tokens": 8000,
                        "context_window_tokens": 32000,
                    },
                )
            )
        # 单独准备真实助手，旧 Fake 默认助手和用户定制版本均完整保留。
        if await existing("agent", settings.default_agent_key) is None:
            prompt = await ensure(
                "prompt",
                "su8_learning_identity",
                {
                    "name": "编程学习助手基础规则",
                    "text": (
                        "你是编程学习助手，用中文准确解释算法，根据用户提供的信息回答。"
                        "缺少必要信息时明确说明。"
                    ),
                },
            )
            skill = await ensure(
                "skill",
                "su8_direct_answer",
                {
                    "name": "直接回答",
                    "description": "解释编程与算法问题，给出关键思路和例子。",
                    "text": "直接回答用户的问题，解释关键思路、适用条件、复杂度和必要的例子。",
                },
            )
            await ensure(
                "agent",
                settings.default_agent_key,
                {
                    "name": "学习助手",
                    "prompt_id": prompt.id,
                    "skill_ids": [skill.id],
                    "default_skill_id": skill.id,
                    "model_profile_id": model_versions[0].id,
                    "visibility": "user",
                    "budget": {"max_run_seconds": 120},
                },
            )
        print("su8 Provider 与三个真实模型已准备，保留所有已有配置。", flush=True)
    finally:
        await database.close()
