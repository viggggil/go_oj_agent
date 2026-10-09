"""不可变配置、原子替换与审计重放；只访问 oj_agent。"""

from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, Literal, cast
from uuid import uuid4

from sqlalchemy import RowMapping, and_, insert, select, update
from sqlalchemy.dialects.mysql import insert as mysql_insert
from sqlalchemy.exc import IntegrityError
from sqlalchemy.ext.asyncio import AsyncConnection, AsyncEngine

from app.core.provider_policy import ProviderPolicy
from app.models.configuration import (
    AgentConfig,
    ConfigBody,
    ConfigError,
    ConfigKind,
    ConfigWrite,
    ProviderConfig,
    SkillConfig,
    config_hash,
    config_references,
    parse_config,
)
from app.storage.repository import utc_now
from app.storage.schema import (
    agent_config_audits as audits,
)
from app.storage.schema import (
    agent_config_links as links,
)
from app.storage.schema import (
    agent_config_resources as resources,
)
from app.storage.schema import (
    agent_config_versions as versions,
)
from app.storage.schema import agent_credentials, agent_provider_credentials
from app.tools.registry import ToolNotFound, ToolRegistry

type ConfigAction = Literal["archive", "disable", "enable"]


@dataclass(frozen=True)
class ConfigVersion:
    id: str
    kind: ConfigKind
    key: str
    content: ConfigBody
    disabled: bool = False
    archived: bool = False


def _version_query() -> Any:
    return select(
        versions.c.id,
        versions.c.content,
        versions.c.archived_at,
        resources.c.kind,
        resources.c.config_key,
        resources.c.disabled,
    ).join(resources, resources.c.id == versions.c.resource_id)


def _record(row: RowMapping) -> ConfigVersion:
    kind = cast(ConfigKind, row["kind"])
    return ConfigVersion(
        id=str(row["id"]),
        kind=kind,
        key=str(row["config_key"]),
        content=parse_config(kind, row["content"]),
        disabled=bool(row["disabled"]),
        archived=row["archived_at"] is not None,
    )


class ConfigurationStore:
    def __init__(
        self,
        engine: AsyncEngine,
        registry: ToolRegistry,
        *,
        clock: Callable[[], datetime] = utc_now,
        provider_policy: ProviderPolicy | None = None,
    ) -> None:
        if engine.url.database != "oj_agent" or engine.url.drivername != "mysql+asyncmy":
            raise ValueError("Configuration store requires its own oj_agent MySQL schema")
        self._engine = engine
        self._registry = registry
        self._clock = clock
        self._provider_policy = provider_policy

    def _validate_tools(self, body: ConfigBody) -> None:
        if isinstance(body, ProviderConfig):
            if self._provider_policy is None:
                raise ConfigError("AGENT_MODEL_ENDPOINT_DENIED")
            self._provider_policy.validate(body)
        if not isinstance(body, (AgentConfig, SkillConfig)):
            return
        for name in body.allowed_tools:
            try:
                spec = self._registry.get(name).spec
            except ToolNotFound:
                raise ConfigError("AGENT_CONFIGURATION_TOOL_INVALID") from None
            if not spec.enabled or spec.side_effect != "read":
                raise ConfigError("AGENT_CONFIGURATION_TOOL_INVALID")

    async def _by_id(self, connection: AsyncConnection, version_id: str) -> ConfigVersion:
        row = (
            (await connection.execute(_version_query().where(versions.c.id == version_id)))
            .mappings()
            .first()
        )
        if row is None:
            raise ConfigError("AGENT_CONFIGURATION_REFERENCE_INVALID")
        return _record(row)

    async def get_version(self, version_id: str) -> ConfigVersion:
        async with self._engine.connect() as connection:
            return await self._by_id(connection, version_id)

    async def resolve(self, kind: ConfigKind, key: str) -> ConfigVersion:
        async with self._engine.connect() as connection:
            row = (
                (
                    await connection.execute(
                        _version_query().where(
                            and_(
                                resources.c.kind == kind,
                                resources.c.config_key == key,
                                resources.c.current_id == versions.c.id,
                                versions.c.archived_at.is_(None),
                            )
                        )
                    )
                )
                .mappings()
                .first()
            )
        if row is None:
            raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
        return _record(row)

    async def list_versions(
        self,
        kind: ConfigKind,
        key: str,
        *,
        limit: int = 50,
        offset: int = 0,
    ) -> list[ConfigVersion]:
        if not 1 <= limit <= 100 or not 0 <= offset <= 10_000:
            raise ConfigError("AGENT_CONFIGURATION_INVALID_PAGE")
        async with self._engine.connect() as connection:
            rows = (
                (
                    await connection.execute(
                        _version_query()
                        .where(
                            and_(
                                resources.c.kind == kind,
                                resources.c.config_key == key,
                            )
                        )
                        .order_by(versions.c.created_at.desc(), versions.c.id.desc())
                        .limit(limit)
                        .offset(offset)
                    )
                )
                .mappings()
                .all()
            )
        return [_record(row) for row in rows]

    async def load_graph(self, agent_key: str) -> tuple[ConfigVersion, dict[str, ConfigVersion]]:
        # 同一连接的 REPEATABLE READ 快照；不会把不同时间的当前指针拼成一份配置。
        async with self._engine.connect() as connection, connection.begin():
            row = (
                (
                    await connection.execute(
                        _version_query().where(
                            and_(
                                resources.c.kind == "agent",
                                resources.c.config_key == agent_key,
                                resources.c.current_id == versions.c.id,
                            )
                        )
                    )
                )
                .mappings()
                .first()
            )
            if row is None:
                raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
            root = _record(row)
            if root.archived or root.disabled:
                raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
            graph = {root.id: root}
            queue = [root]
            while queue:
                item = queue.pop()
                await self._check_credential(connection, item.content)
                for version_id, kind in config_references(item.content).items():
                    reference = graph.get(version_id)
                    if reference is None:
                        reference = await self._by_id(connection, version_id)
                        graph[version_id] = reference
                        queue.append(reference)
                    if reference.kind != kind or reference.disabled:
                        raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
            return root, graph

    async def _check_credential(
        self, connection: AsyncConnection, body: ConfigBody, *, lock: bool = False
    ) -> None:
        if not isinstance(body, ProviderConfig):
            return
        query = select(agent_credentials.c.id, agent_credentials.c.revoked_at).where(
            agent_credentials.c.id == str(body.credential_id)
        )
        if lock:
            query = query.with_for_update()
        row = (await connection.execute(query)).first()
        if row is None or row.revoked_at is not None:
            raise ConfigError("AGENT_CREDENTIAL_UNAVAILABLE", 503)

    async def _replay(
        self,
        connection: AsyncConnection,
        write: ConfigWrite,
        fingerprint: str,
    ) -> ConfigVersion | None:
        audit = (
            (
                await connection.execute(
                    select(*audits.c).where(audits.c.request_id == write.request_id)
                )
            )
            .mappings()
            .first()
        )
        if audit is None:
            return None
        if audit["fingerprint"] != fingerprint or audit["actor_id"] != write.actor_id:
            raise ConfigError("AGENT_CONFIGURATION_REQUEST_CONFLICT", 409)
        return await self._by_id(connection, str(audit["new_id"] or audit["old_id"]))

    async def _lock_resource(self, connection: AsyncConnection, write: ConfigWrite) -> RowMapping:
        statement = mysql_insert(resources).values(
            id=str(uuid4()),
            kind=write.kind,
            config_key=write.key,
            disabled=False,
            created_at=self._clock(),
        )
        await connection.execute(statement.on_duplicate_key_update(id=resources.c.id))
        row = (
            (
                await connection.execute(
                    select(*resources.c)
                    .where(
                        and_(
                            resources.c.kind == write.kind,
                            resources.c.config_key == write.key,
                        )
                    )
                    .with_for_update()
                )
            )
            .mappings()
            .one()
        )
        return row

    async def _audit(
        self,
        connection: AsyncConnection,
        write: ConfigWrite,
        fingerprint: str,
        resource_id: str,
        action: str,
        old_id: str | None,
        new_id: str | None,
    ) -> None:
        await connection.execute(
            insert(audits).values(
                request_id=write.request_id,
                actor_id=write.actor_id,
                action=action,
                resource_id=resource_id,
                old_id=old_id,
                new_id=new_id,
                fingerprint=fingerprint,
                created_at=self._clock(),
            )
        )

    async def _check_references(
        self,
        connection: AsyncConnection,
        body: ConfigBody,
        preserved_ids: tuple[str, ...],
    ) -> None:
        old_links: set[str] = set()
        await self._check_credential(connection, body, lock=True)
        for old_id in preserved_ids:
            old_links.update(
                (
                    await connection.execute(
                        select(links.c.target_id).where(links.c.version_id == old_id)
                    )
                )
                .scalars()
                .all()
            )
        skill_keys: set[str] = set()
        queue: list[ConfigVersion] = []
        for version_id, kind in config_references(body).items():
            reference = await self._by_id(connection, version_id)
            if (
                reference.kind != kind
                or reference.disabled
                or (reference.archived and version_id not in old_links)
            ):
                raise ConfigError("AGENT_CONFIGURATION_REFERENCE_INVALID")
            if kind == "skill":
                if reference.key in skill_keys:
                    raise ConfigError("AGENT_CONFIGURATION_REFERENCE_INVALID")
                skill_keys.add(reference.key)
            queue.append(reference)
        visited: set[str] = set()
        while queue:
            reference = queue.pop()
            if reference.id in visited:
                continue
            visited.add(reference.id)
            self._validate_tools(reference.content)
            await self._check_credential(connection, reference.content, lock=True)
            for version_id, kind in config_references(reference.content).items():
                child = await self._by_id(connection, version_id)
                if child.kind != kind or child.disabled:
                    raise ConfigError("AGENT_CONFIGURATION_REFERENCE_INVALID")
                queue.append(child)

    async def create_or_replace(self, write: ConfigWrite, content: Any) -> ConfigVersion:
        body = parse_config(write.kind, content)
        serialized = body.model_dump(mode="json")
        fingerprint = config_hash(
            {**write.model_dump(mode="json"), "action": "save", "content": serialized}
        )
        try:
            async with self._engine.begin() as connection:
                resource = await self._lock_resource(connection, write)
                replay = await self._replay(connection, write, fingerprint)
                if replay is not None:
                    return replay
                old_id = cast(str | None, resource["current_id"])
                expected = str(write.expected_id) if write.expected_id else None
                if old_id != expected:
                    raise ConfigError("AGENT_CONFIGURATION_STALE", 409)
                self._validate_tools(body)
                preserved_ids = [old_id] if old_id else []
                if write.source_id is not None:
                    source = await self._by_id(connection, str(write.source_id))
                    if (
                        source.kind != write.kind
                        or source.key != write.key
                        or source.content != body
                    ):
                        raise ConfigError("AGENT_CONFIGURATION_RESTORE_INVALID")
                    preserved_ids.append(source.id)
                await self._check_references(connection, body, tuple(preserved_ids))
                now = self._clock()
                if isinstance(body, AgentConfig) and body.test_expires_at is not None:
                    if body.test_expires_at.timestamp() <= now.replace(tzinfo=UTC).timestamp():
                        raise ConfigError("AGENT_CONFIGURATION_TEST_EXPIRED")
                version_id = str(uuid4())
                await connection.execute(
                    insert(versions).values(
                        id=version_id,
                        resource_id=resource["id"],
                        previous_id=old_id,
                        content=serialized,
                        created_by=write.actor_id,
                        created_at=now,
                    )
                )
                for reference_id in config_references(body):
                    await connection.execute(
                        insert(links).values(version_id=version_id, target_id=reference_id)
                    )
                if isinstance(body, ProviderConfig):
                    await connection.execute(
                        insert(agent_provider_credentials).values(
                            version_id=version_id, credential_id=str(body.credential_id)
                        )
                    )
                if old_id:
                    await connection.execute(
                        update(versions)
                        .where(and_(versions.c.id == old_id, versions.c.archived_at.is_(None)))
                        .values(archived_by=write.actor_id, archived_at=now)
                    )
                await connection.execute(
                    update(resources)
                    .where(resources.c.id == resource["id"])
                    .values(current_id=version_id)
                )
                await self._audit(
                    connection,
                    write,
                    fingerprint,
                    str(resource["id"]),
                    "replace" if old_id else "create",
                    old_id,
                    version_id,
                )
                return ConfigVersion(
                    version_id, write.kind, write.key, body, disabled=bool(resource["disabled"])
                )
        except IntegrityError:
            # 同一个请求编号在另一个资源上并发提交：失败事务回滚后读取已提交审计。
            async with self._engine.connect() as connection:
                replay = await self._replay(connection, write, fingerprint)
                if replay is not None:
                    return replay
            raise

    async def change_state(self, write: ConfigWrite, action: ConfigAction) -> ConfigVersion:
        fingerprint = config_hash({**write.model_dump(mode="json"), "action": action})
        try:
            async with self._engine.begin() as connection:
                resource = await self._lock_resource(connection, write)
                replay = await self._replay(connection, write, fingerprint)
                if replay is not None:
                    return replay
                old_id = resource["current_id"]
                if not old_id:
                    raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
                if str(write.expected_id) != old_id:
                    raise ConfigError("AGENT_CONFIGURATION_STALE", 409)
                if action == "archive":
                    await connection.execute(
                        update(versions)
                        .where(and_(versions.c.id == old_id, versions.c.archived_at.is_(None)))
                        .values(archived_by=write.actor_id, archived_at=self._clock())
                    )
                else:
                    await connection.execute(
                        update(resources)
                        .where(resources.c.id == resource["id"])
                        .values(disabled=action == "disable")
                    )
                await self._audit(
                    connection, write, fingerprint, str(resource["id"]), action, old_id, None
                )
                return await self._by_id(connection, str(old_id))
        except IntegrityError:
            # 回滚后重新读取审计，把跨资源的并发请求编号冲突转换为稳定错误。
            async with self._engine.connect() as connection:
                replay = await self._replay(connection, write, fingerprint)
                if replay is not None:
                    return replay
            raise
