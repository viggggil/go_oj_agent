"""Agent 控制面配置存储；所有替换在一个 MySQL 事务中完成。"""

from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any
from uuid import UUID, uuid4

from sqlalchemy import and_, insert, select, update
from sqlalchemy.ext.asyncio import AsyncEngine

from app.models.configuration import (
    ConfigBody,
    ConfigError,
    ConfigKind,
    ConfigWrite,
    config_hash,
    config_references,
    parse_config,
)
from app.storage.schema import (
    agent_config_audits,
    agent_config_links,
    agent_config_resources,
    agent_config_versions,
)


@dataclass(frozen=True)
class ConfigVersion:
    id: UUID
    kind: ConfigKind
    key: str
    content: dict[str, Any]
    disabled: bool
    archived: bool


class ConfigurationStore:
    def __init__(self, engine: AsyncEngine, *, clock: Any) -> None:
        self._engine = engine
        self._clock = clock

    @staticmethod
    def _expired(content: dict[str, Any]) -> bool:
        value = content.get("test_expires_at")
        if not value:
            return False
        try:
            return datetime.fromisoformat(str(value)).astimezone(timezone.utc) <= datetime.now(timezone.utc)
        except ValueError:
            return True

    async def resolve(self, kind: ConfigKind, key: str, *, version_id: UUID | None = None) -> ConfigVersion:
        async with self._engine.connect() as connection:
            row = (
                await connection.execute(
                    select(
                        agent_config_resources.c.kind,
                        agent_config_resources.c.config_key,
                        agent_config_resources.c.disabled,
                        agent_config_versions.c.id,
                        agent_config_versions.c.content,
                        agent_config_versions.c.archived_at,
                    )
                    .join(agent_config_versions, agent_config_versions.c.id == (str(version_id) if version_id else agent_config_resources.c.current_id))
                    .where(and_(agent_config_resources.c.kind == kind, agent_config_resources.c.config_key == key))
                )
            ).mappings().first()
        if row is None or row["disabled"] or row["archived_at"] is not None or self._expired(dict(row["content"])):
            raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
        return ConfigVersion(UUID(str(row["id"])), kind, key, dict(row["content"]), False, False)

    async def create_or_replace(self, write: ConfigWrite, content: Any) -> ConfigVersion:
        body = parse_config(write.kind, content)
        body_json = body.model_dump(mode="json")
        fingerprint = config_hash({"kind": write.kind, "key": write.key, "expected_id": str(write.expected_id) if write.expected_id else None, "content": body_json})
        now = self._clock()
        async with self._engine.begin() as connection:
            audit = (
                await connection.execute(select(agent_config_audits).where(agent_config_audits.c.request_id == write.request_id))
            ).mappings().first()
            if audit is not None:
                if audit["fingerprint"] != fingerprint or audit["actor_id"] != write.actor_id:
                    raise ConfigError("AGENT_CONFIGURATION_REQUEST_CONFLICT", 409)
                if audit["new_id"] is None:
                    raise ConfigError("AGENT_CONFIGURATION_RETRY_UNAVAILABLE", 409)
                row = (await connection.execute(select(agent_config_resources, agent_config_versions).join(
                    agent_config_versions, agent_config_versions.c.id == agent_config_resources.c.current_id
                ).where(agent_config_resources.c.id == audit["resource_id"]))).mappings().first()
                if row is None:
                    raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
                return ConfigVersion(UUID(str(audit["new_id"])), write.kind, write.key, dict(row["content"]), False, False)

            resource = (await connection.execute(
                select(agent_config_resources).where(and_(agent_config_resources.c.kind == write.kind, agent_config_resources.c.config_key == write.key)).with_for_update()
            )).mappings().first()
            old_id: str | None = None
            if resource is None:
                resource_id = str(uuid4())
                await connection.execute(insert(agent_config_resources).values(
                    id=resource_id, kind=write.kind, config_key=write.key,
                    current_id=None, disabled=False, created_at=now,
                ))
            else:
                resource_id = str(resource["id"])
                if resource["disabled"]:
                    raise ConfigError("AGENT_CONFIGURATION_DISABLED", 409)
                old_id = str(resource["current_id"]) if resource["current_id"] else None
                if write.expected_id is not None and old_id != str(write.expected_id):
                    raise ConfigError("AGENT_CONFIGURATION_STALE", 409)

            for reference_id, reference_kind in config_references(body).items():
                reference = (
                    await connection.execute(
                        select(agent_config_versions.c.id)
                        .join(
                            agent_config_resources,
                            agent_config_resources.c.id == agent_config_versions.c.resource_id,
                        )
                        .where(
                            and_(
                                agent_config_versions.c.id == reference_id,
                                agent_config_resources.c.kind == reference_kind,
                                agent_config_resources.c.disabled.is_(False),
                                agent_config_versions.c.archived_at.is_(None),
                            )
                        )
                    )
                ).first()
                if reference is None:
                    raise ConfigError("AGENT_CONFIGURATION_REFERENCE_INVALID", 400)

            version_id = str(uuid4())
            await connection.execute(insert(agent_config_versions).values(
                id=version_id, resource_id=resource_id, previous_id=old_id,
                content=body_json, created_by=write.actor_id, created_at=now,
            ))
            for reference_id in config_references(body):
                await connection.execute(insert(agent_config_links).values(version_id=version_id, target_id=reference_id))
            if old_id:
                await connection.execute(update(agent_config_versions).where(agent_config_versions.c.id == old_id).values(archived_by=write.actor_id, archived_at=now))
            await connection.execute(update(agent_config_resources).where(agent_config_resources.c.id == resource_id).values(current_id=version_id))
            await connection.execute(insert(agent_config_audits).values(
                request_id=write.request_id, actor_id=write.actor_id, action="replace" if old_id else "create",
                resource_id=resource_id, old_id=old_id, new_id=version_id, fingerprint=fingerprint, created_at=now,
            ))
        return ConfigVersion(UUID(version_id), write.kind, write.key, body_json, False, False)
