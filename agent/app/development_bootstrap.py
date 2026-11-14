"""开发部署的单 su8 Provider 准备；保留已有资源及停用/归档状态。"""

import asyncio
from pathlib import Path
from typing import Any
from uuid import UUID

from pydantic import SecretStr
from sqlalchemy import select

from app.answer_policy import ALGORITHM_SKILL, PUBLIC_ANSWER_PROMPT
from app.clients.business import build_business_clients
from app.core.credentials import CredentialCipher
from app.core.database import DatabaseProbe
from app.core.provider_policy import ProviderPolicy
from app.core.settings import Settings
from app.models.configuration import (
    AgentConfig,
    ConfigError,
    ConfigKind,
    ConfigWrite,
    ProviderConfig,
    config_hash,
)
from app.storage.configuration import ConfigurationStore, ConfigVersion
from app.storage.credentials import CredentialStore, CredentialWrite
from app.storage.schema import agent_config_audits as audits
from app.storage.schema import agent_config_resources as resources
from app.tools.business import build_business_tool_registry
from app.tools.registry import ToolRegistry

SU8_MODELS = ("deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4.1-flash")


def seed_upgrade_request(value: ConfigVersion, content: dict[str, Any]) -> str:
    """相同版本转换可重放，不同依赖绑定不能复用审计请求 ID。"""
    digest = config_hash({"source_id": value.id, "content": content})[:32]
    return f"compose.su8.v6.{value.kind}.{value.key}.{digest}"


def is_seed_request(value: ConfigVersion, request_id: str) -> bool:
    legacy = {f"compose.su8.v{n}.{value.kind}.{value.key}" for n in range(1, 7)}
    if request_id in legacy:
        return True
    prefix = f"compose.su8.v6.{value.kind}.{value.key}."
    digest = request_id.removeprefix(prefix)
    return (
        request_id.startswith(prefix)
        and len(digest) == 32
        and all(char in "0123456789abcdef" for char in digest)
    )


async def bootstrap_su8(
    settings: Settings, *, key_file: Path = Path("/run/model-keys/su8.key")
) -> None:
    database = DatabaseProbe(settings)
    clients = None
    try:
        engine = database.engine
        assert engine is not None
        cipher = CredentialCipher.from_file(settings.credential_keyring_file)
        credentials = CredentialStore(engine, cipher)
        registry = ToolRegistry()
        if settings.business_tools_enabled:
            clients = build_business_clients(settings)
            registry = build_business_tool_registry(
                clients, settings.tool_max_result_bytes, settings.tool_max_page_size
            )
        store = ConfigurationStore(engine, registry, provider_policy=ProviderPolicy(settings))

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

        async def is_seed_version(value: ConfigVersion) -> bool:
            async with engine.connect() as connection:
                requests = await connection.scalars(
                    select(audits.c.request_id).where(audits.c.new_id == value.id)
                )
            return any(is_seed_request(value, request_id) for request_id in requests)

        async def upgrade_seed(value: ConfigVersion, content: dict[str, Any]) -> ConfigVersion:
            if value.content.model_dump(mode="json") == content or not await is_seed_version(value):
                return value
            return await store.create_or_replace(
                ConfigWrite(
                    kind=value.kind,
                    key=value.key,
                    actor_id=1,
                    request_id=seed_upgrade_request(value, content),
                    expected_id=UUID(value.id),
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
                        "context_window_tokens": 64000,
                    },
                )
            )
        current = await existing("agent", settings.default_agent_key)
        # 只升级 Compose 自动创建的版本；管理员另存过的 Agent 与依赖保持原样。
        if current is None or await is_seed_version(current):
            prompt = await ensure(
                "prompt",
                "su8_learning_identity",
                {
                    "name": "编程学习助手基础规则",
                    "text": PUBLIC_ANSWER_PROMPT,
                },
            )
            prompt = await upgrade_seed(
                prompt, {**prompt.content.model_dump(mode="json"), "text": PUBLIC_ANSWER_PROMPT}
            )
            direct = await ensure(
                "skill",
                "su8_direct_answer",
                {
                    "name": "直接回答",
                    "description": "解释编程与算法问题，给出关键思路和例子。",
                    "text": ALGORITHM_SKILL,
                },
            )
            direct = await upgrade_seed(
                direct, {**direct.content.model_dump(mode="json"), "text": ALGORITHM_SKILL}
            )
            tools = ["get_problem", "list_problems"] if settings.business_tools_enabled else []
            skill = direct
            if tools:
                skill = await ensure(
                    "skill",
                    "su8_problem_answer",
                    {
                        "name": "题目讲解",
                        "description": "按题号读取公开题目，解释题意、算法与复杂度。",
                        "allowed_tools": tools,
                        "text": (
                            "用户提到题号时，先通过 get_problem 读取真实题目，再根据题意回答。"
                            "需要查找题目时使用 list_problems。不能编造题目内容；"
                            "信息不可用时说明具体缺少什么，不展示内部工具、检索和权限流程。"
                            + ALGORITHM_SKILL
                        ),
                    },
                )
                skill = await upgrade_seed(
                    skill,
                    {
                        **skill.content.model_dump(mode="json"),
                        "text": (
                            "用户提到题号时，先读取真实题目，再根据题意回答。不能编造题目内容；"
                            "信息不可用时说明具体缺少什么，不展示内部工具、检索和权限流程。\n"
                            + ALGORITHM_SKILL
                        ),
                    },
                )
            content = {
                "name": "学习助手",
                "prompt_id": prompt.id,
                "skill_ids": list(dict.fromkeys([skill.id, direct.id])),
                "default_skill_id": skill.id,
                "model_profile_id": model_versions[0].id,
                "allowed_tools": tools,
                "visibility": "user",
                "budget": {
                    "max_run_seconds": 120,
                    "max_input_tokens": 64000,
                    "max_tool_calls": 16,
                    "max_model_calls": 16,
                },
            }
            if current is None:
                await ensure("agent", settings.default_agent_key, content)
            else:
                # 保留运行参数与模型绑定，同步受管 Skill 引用及当前部署可用的工具权限。
                content = {
                    **current.content.model_dump(mode="json"),
                    "prompt_id": prompt.id,
                    "skill_ids": list(dict.fromkeys([skill.id, direct.id])),
                    "default_skill_id": skill.id,
                    "allowed_tools": tools,
                }
                await upgrade_seed(current, content)
        # 专用评审资源固定为只读、无知识库、单次模型调用的管理员 Agent。
        from app.evals.reviewer import (
            REVIEWER_AGENT_KEY,
            REVIEWER_MODEL_KEY,
            REVIEWER_PROMPT,
            REVIEWER_PROMPT_KEY,
            REVIEWER_SKILL,
            REVIEWER_SKILL_KEY,
        )

        reviewer_prompt = await ensure(
            "prompt",
            REVIEWER_PROMPT_KEY,
            {"name": "Eval 结构化评审规则", "text": REVIEWER_PROMPT},
        )
        reviewer_skill = await ensure(
            "skill",
            REVIEWER_SKILL_KEY,
            {
                "name": "Eval 评审量表",
                "description": "离线质量评分维度与安全边界。",
                "text": REVIEWER_SKILL,
            },
        )

        reviewer_prompt = await upgrade_seed(
            reviewer_prompt,
            {**reviewer_prompt.content.model_dump(mode="json"), "text": REVIEWER_PROMPT},
        )
        reviewer_skill = await upgrade_seed(
            reviewer_skill,
            {**reviewer_skill.content.model_dump(mode="json"), "text": REVIEWER_SKILL},
        )

        reviewer_model = await existing("model", REVIEWER_MODEL_KEY)
        if reviewer_model is None:
            reviewer_model = await ensure(
                "model",
                REVIEWER_MODEL_KEY,
                {
                    "provider": "responses",
                    "provider_id": provider.id,
                    "model": "deepseek-v4-pro",
                    "verbosity": "high",
                    "max_output_tokens": 8000,
                    "context_window_tokens": 48000,
                },
            )
        elif (
            getattr(reviewer_model.content, "max_output_tokens", None) == 2000
            or getattr(reviewer_model.content, "context_window_tokens", 0) < 48000
        ):
            reviewer_model = await upgrade_seed(
                reviewer_model,
                {
                    **reviewer_model.content.model_dump(mode="json"),
                    "max_output_tokens": 8000,
                    "context_window_tokens": 48000,
                },
            )
        reviewer_agent = await existing("agent", REVIEWER_AGENT_KEY)
        if reviewer_agent is None:
            await ensure(
                "agent",
                REVIEWER_AGENT_KEY,
                {
                    "name": "Eval 专用评审员",
                    "prompt_id": reviewer_prompt.id,
                    "skill_ids": [reviewer_skill.id],
                    "default_skill_id": reviewer_skill.id,
                    "model_profile_id": reviewer_model.id,
                    "allowed_tools": [],
                    "budget": {
                        "max_run_seconds": 60,
                        "max_output_chars": 8000,
                        "max_events": 10000,
                        "max_tool_calls": 0,
                        "max_model_calls": 1,
                        "max_input_tokens": 32000,
                        "max_output_tokens": 8000,
                    },
                    "visibility": "admin",
                },
            )
        elif isinstance(reviewer_agent.content, AgentConfig):
            content = reviewer_agent.content.model_dump(mode="json")
            content.update(
                prompt_id=reviewer_prompt.id,
                skill_ids=[reviewer_skill.id],
                default_skill_id=reviewer_skill.id,
                model_profile_id=reviewer_model.id,
            )
            content["budget"] = {
                **reviewer_agent.content.budget.model_dump(),
                "max_output_tokens": 8000,
                "max_input_tokens": 32000,
                "max_events": 10000,
            }
            await upgrade_seed(reviewer_agent, content)
        print(
            "su8 Provider、三个真实模型及 Eval 专用评审 Agent 已准备，保留所有已有配置。",
            flush=True,
        )
    finally:
        if clients is not None:
            await clients.close()
        await database.close()
