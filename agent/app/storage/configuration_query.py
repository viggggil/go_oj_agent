"""有界管理查询与可运行目录；过滤在 SQL 分页前完成。"""

from datetime import UTC, datetime
from typing import Any

from sqlalchemy import and_, exists, func, not_, or_, select
from sqlalchemy.ext.asyncio import AsyncConnection

from app.models.configuration import ConfigKind
from app.storage.configuration import ConfigurationStore, ConfigVersion, _record, _version_query
from app.storage.schema import agent_config_links as links
from app.storage.schema import agent_config_resources as resources
from app.storage.schema import agent_config_versions as versions
from app.storage.schema import agent_credentials, agent_provider_credentials


def json_text(version: Any, field: str) -> Any:
    return func.json_unquote(func.json_extract(version.c.content, "$." + field))


def dependencies_available(version: Any, depth: int = 3) -> Any:
    # 配置图最长为 Agent -> Skill/Model -> Prompt/Provider，不接受任意深度图。
    credential = exists(
        select(1)
        .select_from(agent_provider_credentials.join(agent_credentials))
        .where(
            agent_provider_credentials.c.version_id == version.c.id,
            agent_credentials.c.revoked_at.is_(None),
        )
    )
    owner = resources.alias()
    provider_valid = or_(
        not_(
            exists(select(1).where(owner.c.id == version.c.resource_id, owner.c.kind == "provider"))
        ),
        credential,
    )
    if depth == 0:
        return and_(
            provider_valid, not_(exists(select(1).where(links.c.version_id == version.c.id)))
        )
    child, child_owner, edge = versions.alias(), resources.alias(), links.alias()
    invalid = exists(
        select(1)
        .select_from(
            edge.join(child, edge.c.target_id == child.c.id).join(
                child_owner, child.c.resource_id == child_owner.c.id
            )
        )
        .where(
            edge.c.version_id == version.c.id,
            or_(child_owner.c.disabled.is_(True), not_(dependencies_available(child, depth - 1))),
        )
    )
    return and_(provider_valid, not_(invalid))


def model_supported(
    version: Any,
    runtime: str,
    provider_key: str | None = None,
    provider_urls: list[str] | None = None,
) -> Any:
    if runtime != "model":
        return json_text(version, "provider") == "fake"
    provider, owner = versions.alias(), resources.alias()
    return and_(
        json_text(version, "provider") == "responses",
        exists(
            select(1)
            .select_from(provider.join(owner, provider.c.resource_id == owner.c.id))
            .where(
                provider.c.id == json_text(version, "provider_id"),
                owner.c.kind == "provider",
                owner.c.disabled.is_(False),
                *([owner.c.config_key == provider_key] if provider_key else []),
                *(
                    [json_text(provider, "base_url").in_(provider_urls)]
                    if provider_urls is not None
                    else []
                ),
                dependencies_available(provider),
            )
        ),
    )


def agent_available(admin: bool, runtime: str, provider_urls: list[str] | None = None) -> Any:
    model, skill = versions.alias(), versions.alias()
    expiry = json_text(versions, "test_expires_at")
    expiry_utc = func.convert_tz(
        func.str_to_date(
            func.if_(
                func.substr(expiry, 20, 1) == ".",
                func.substr(expiry, 1, 26),
                func.substr(expiry, 1, 19),
            ),
            func.if_(
                func.substr(expiry, 20, 1) == ".", "%Y-%m-%dT%H:%i:%s.%f", "%Y-%m-%dT%H:%i:%s"
            ),
        ),
        func.if_(func.right(expiry, 1) == "Z", "+00:00", func.right(expiry, 6)),
        "+00:00",
    )
    return and_(
        versions.c.archived_at.is_(None),
        resources.c.disabled.is_(False),
        *([] if admin else [json_text(versions, "visibility") == "user"]),
        or_(expiry == "null", expiry_utc > datetime.now(UTC).replace(tzinfo=None)),
        dependencies_available(versions),
        exists(
            select(1).where(
                model.c.id == json_text(versions, "model_profile_id"),
                model_supported(model, runtime, provider_urls=provider_urls),
            )
        ),
        exists(
            select(1).where(
                skill.c.id == json_text(versions, "default_skill_id"),
                *([json_text(skill, "execution_mode") == "direct"] if runtime == "model" else []),
            )
        ),
    )


class ConfigurationQuery:
    def __init__(self, store: ConfigurationStore) -> None:
        self.store = store

    async def page(
        self,
        kind: ConfigKind,
        *,
        page: int,
        page_size: int,
        search: str = "",
        state: str = "active",
        key: str | None = None,
        history: bool = False,
        predicate: Any = None,
        connection: AsyncConnection | None = None,
    ) -> tuple[list[ConfigVersion], int]:
        if connection is None:
            async with self.store._engine.connect() as connection, connection.begin():
                return await self.page(
                    kind,
                    page=page,
                    page_size=page_size,
                    search=search,
                    state=state,
                    key=key,
                    history=history,
                    predicate=predicate,
                    connection=connection,
                )
        filters = [resources.c.kind == kind]
        if key is not None:
            filters.append(resources.c.config_key == key)
        if search:
            filters.append(resources.c.config_key.contains(search, autoescape=True))
        if not history:
            filters.append(resources.c.current_id == versions.c.id)
            if state == "active":
                filters.append(versions.c.archived_at.is_(None))
            elif state == "archived":
                filters.append(versions.c.archived_at.is_not(None))
            elif state == "disabled":
                filters.extend([resources.c.disabled.is_(True), versions.c.archived_at.is_(None)])
        if predicate is not None:
            filters.append(predicate)
        query = _version_query().where(*filters)
        ordering = versions if history else resources
        total = int(
            (
                await connection.execute(select(func.count()).select_from(query.subquery()))
            ).scalar_one()
        )
        rows = (
            (
                await connection.execute(
                    query.order_by(ordering.c.created_at.desc(), ordering.c.id.desc())
                    .limit(page_size)
                    .offset((page - 1) * page_size)
                )
            )
            .mappings()
            .all()
        )
        return [_record(row) for row in rows], total

    async def detail(
        self, kind: ConfigKind, key: str, identifier: str | None = None
    ) -> ConfigVersion:
        from app.models.configuration import ConfigError

        query = _version_query().where(resources.c.kind == kind, resources.c.config_key == key)
        query = query.where(
            versions.c.id == identifier if identifier else resources.c.current_id == versions.c.id
        )
        async with self.store._engine.connect() as connection:
            row = (await connection.execute(query)).mappings().first()
        if row is None:
            raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
        return _record(row)

    async def by_ids(self, identifiers: list[str]) -> list[ConfigVersion]:
        async with self.store._engine.connect() as connection:
            return await self._by_ids(connection, identifiers)

    async def _by_ids(
        self, connection: AsyncConnection, identifiers: list[str]
    ) -> list[ConfigVersion]:
        rows = (
            (await connection.execute(_version_query().where(versions.c.id.in_(identifiers))))
            .mappings()
            .all()
        )
        return [_record(row) for row in rows]
