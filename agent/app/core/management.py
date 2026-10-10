"""管理用例复用配置仓储；目录只返回聊天必需字段。"""

import builtins
from datetime import UTC, datetime
from typing import Any
from uuid import UUID

from sqlalchemy.ext.asyncio import AsyncConnection

from app.core.operations import ADMIN_ROLES
from app.core.provider_policy import ProviderPolicy
from app.core.settings import Settings
from app.models.admin import PageQuery
from app.models.configuration import (
    AgentConfig,
    ConfigBody,
    ConfigError,
    ConfigKind,
    ConfigWrite,
    ModelProfileConfig,
    ProviderConfig,
    SkillConfig,
)
from app.models.runtime import Principal
from app.storage.configuration import ConfigurationStore, ConfigVersion
from app.storage.configuration_query import (
    ConfigurationQuery,
    agent_available,
    json_text,
    model_supported,
)
from app.tools.registry import ToolRegistry


def summary(value: ConfigVersion, *, full: bool = False) -> dict[str, Any]:
    def date(value: datetime | None) -> str | None:
        return value.replace(tzinfo=UTC).isoformat() if value else None

    result: dict[str, Any] = {
        "id": value.id,
        "kind": value.kind,
        "key": value.key,
        "archived": value.archived,
        "disabled": value.disabled,
        "created_at": date(value.created_at),
        "created_by": value.created_by,
        "archived_at": date(value.archived_at),
        "archived_by": value.archived_by,
    }
    if full:
        result["content"] = value.content.model_dump(mode="json")
    elif isinstance(value.content, (AgentConfig, SkillConfig)):
        result["name"] = value.content.name
        if isinstance(value.content, AgentConfig):
            result.update(
                visibility=value.content.visibility,
                is_test=value.content.is_test,
                test_expires_at=value.content.test_expires_at,
                model_profile_id=value.content.model_profile_id,
            )
    return result


def page_result(items: list[dict[str, Any]], total: int, query: PageQuery) -> dict[str, Any]:
    return {
        "items": items,
        "page": {"page": query.page, "page_size": query.page_size, "total": total},
    }


class ManagementService:
    def __init__(
        self, store: ConfigurationStore, settings: Settings, registry: ToolRegistry
    ) -> None:
        self.store = store
        self.query = ConfigurationQuery(store)
        self.settings = settings
        self.registry = registry

    async def list(
        self, kind: ConfigKind, query: PageQuery, key: str | None = None
    ) -> dict[str, Any]:
        if key is not None:
            await self.query.detail(kind, key)
        items, total = await self.query.page(
            kind, **query.model_dump(), key=key, history=key is not None
        )
        return page_result([summary(item) for item in items], total, query)

    async def detail(
        self, kind: ConfigKind, key: str, identifier: str | None = None
    ) -> dict[str, Any]:
        return {"configuration": summary(await self.query.detail(kind, key, identifier), full=True)}

    async def save(
        self,
        kind: ConfigKind,
        key: str,
        content: dict[str, Any],
        principal: Principal,
        expected_id: UUID | None = None,
        source_id: UUID | None = None,
    ) -> dict[str, Any]:
        write = ConfigWrite(
            kind=kind,
            key=key,
            actor_id=principal.user_id,
            request_id=principal.request_id,
            expected_id=expected_id,
            source_id=source_id,
        )
        value = await self.store.create_or_replace(write, content, validator=self._validate)
        return {"configuration": summary(value, full=True)}

    async def _validate(self, connection: AsyncConnection, body: ConfigBody) -> None:
        if isinstance(body, SkillConfig) and body.execution_mode != "direct":
            raise ConfigError("AGENT_CONFIGURATION_UNSUPPORTED", 409)
        if isinstance(body, AgentConfig):
            model = await self.store._by_id(connection, str(body.model_profile_id))
            if (
                not isinstance(model.content, ModelProfileConfig)
                or model.content.provider != "responses"
            ):
                raise ConfigError("AGENT_CONFIGURATION_UNSUPPORTED", 409)
            provider = await self.store._by_id(connection, str(model.content.provider_id))
            if provider.key != self.settings.admin_provider_key or not isinstance(
                provider.content, ProviderConfig
            ):
                raise ConfigError("AGENT_CONFIGURATION_UNSUPPORTED", 409)
            ProviderPolicy(self.settings).validate(provider.content)
            for skill in await self.query._by_ids(
                connection, [str(identifier) for identifier in body.skill_ids]
            ):
                if (
                    not isinstance(skill.content, SkillConfig)
                    or skill.content.execution_mode != "direct"
                ):
                    raise ConfigError("AGENT_CONFIGURATION_UNSUPPORTED", 409)

    async def models(self, query: PageQuery) -> dict[str, Any]:
        from sqlalchemy import and_

        from app.storage.schema import agent_config_resources as resources
        from app.storage.schema import agent_config_versions as versions

        async with self.store._engine.connect() as connection, connection.begin():
            provider_urls = await self._provider_urls(connection)
            items, total = await self.query.page(
                "model",
                page=query.page,
                page_size=query.page_size,
                search=query.search,
                predicate=and_(
                    resources.c.disabled.is_(False),
                    model_supported(
                        versions, "model", self.settings.admin_provider_key, provider_urls
                    ),
                ),
                connection=connection,
            )
        values = []
        for item in items:
            assert isinstance(item.content, ModelProfileConfig)
            profile = item.content
            values.append(
                {
                    "id": item.id,
                    "key": item.key,
                    "model": profile.model,
                    "temperature": profile.temperature,
                    "verbosity": profile.verbosity,
                    "max_output_tokens": profile.max_output_tokens,
                    "context_window_tokens": profile.context_window_tokens,
                }
            )
        return page_result(values, total, query)

    async def _provider_urls(self, connection: AsyncConnection) -> builtins.list[str]:
        from sqlalchemy import select

        from app.storage.schema import agent_config_resources as resources
        from app.storage.schema import agent_config_versions as versions

        # 同源历史版本只验证一次；目录分页前应用部署网络策略，不逐 Agent 读取配置图。
        urls = (
            await connection.execute(
                select(json_text(versions, "base_url"))
                .distinct()
                .select_from(versions.join(resources, versions.c.resource_id == resources.c.id))
                .where(resources.c.kind == "provider")
            )
        ).scalars()
        policy = ProviderPolicy(self.settings)
        allowed = []
        for url in urls:
            try:
                policy.validate(
                    ProviderConfig(name="policy", base_url=url, credential_id=UUID(int=0))
                )
            except ConfigError:
                continue
            allowed.append(url)
        return allowed

    async def catalog(
        self, query: PageQuery, principal: Principal, key: str | None = None
    ) -> dict[str, Any]:
        if (
            not self.settings.chat_enabled
            or self.settings.runtime_mode == "disabled"
            or self.settings.config_mode != "database"
        ):
            if key:
                raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
            return page_result([], 0, query)
        async with self.store._engine.connect() as connection, connection.begin():
            provider_urls = (
                await self._provider_urls(connection)
                if self.settings.runtime_mode == "model"
                else None
            )
            items, total = await self.query.page(
                "agent",
                page=query.page,
                page_size=query.page_size,
                search=query.search,
                key=key,
                predicate=agent_available(
                    bool(principal.roles & ADMIN_ROLES), self.settings.runtime_mode, provider_urls
                ),
                connection=connection,
            )
            if key is not None:
                if not items:
                    raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
                config = items[0].content
                assert isinstance(config, AgentConfig)
                skills = await self.query._by_ids(
                    connection, [str(identifier) for identifier in config.skill_ids]
                )
                values = [
                    self._skill_summary(item, config)
                    for item in skills
                    if isinstance(item.content, SkillConfig)
                    and not item.disabled
                    and (
                        self.settings.runtime_mode != "model"
                        or item.content.execution_mode == "direct"
                    )
                ]
                default = next((value["key"] for value in values if value["default"]), None)
                if default is None:
                    raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
                return {"items": values, "default_skill_key": default}
        return page_result(
            [
                {
                    "key": item.key,
                    "name": item.content.name,
                    "is_default": item.key == self.settings.default_agent_key,
                }
                for item in items
                if isinstance(item.content, AgentConfig)
            ],
            total,
            query,
        )

    @staticmethod
    def _skill_summary(item: ConfigVersion, config: AgentConfig) -> dict[str, Any]:
        assert isinstance(item.content, SkillConfig)
        return {
            "key": item.key,
            "name": item.content.name,
            "execution_mode": item.content.execution_mode,
            "default": item.id == str(config.default_skill_id),
        }
