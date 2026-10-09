"""凭据不可原地替换；加密、撤销与审计在 oj_agent 本地事务内完成。"""

import hmac
from typing import Any
from uuid import UUID, uuid4

from pydantic import BaseModel, ConfigDict, Field, SecretStr
from sqlalchemy import RowMapping, insert, select, update
from sqlalchemy.exc import IntegrityError
from sqlalchemy.ext.asyncio import AsyncConnection, AsyncEngine

from app.core.credentials import CredentialCipher
from app.models.configuration import ConfigError, config_hash
from app.storage.repository import utc_now
from app.storage.schema import agent_credential_audits as audits
from app.storage.schema import agent_credentials as credentials


class CredentialWrite(BaseModel):
    model_config = ConfigDict(frozen=True, extra="forbid", hide_input_in_errors=True)
    actor_id: int = Field(gt=0, le=2**63 - 1, strict=True)
    request_id: str = Field(min_length=1, max_length=128, pattern=r"^[a-zA-Z0-9._:-]+$")


def summary(row: RowMapping) -> dict[str, Any]:
    return {
        "id": row["id"],
        "name": row["name"],
        "revoked": row["revoked_at"] is not None,
        "created_at": row["created_at"].isoformat(),
    }


class CredentialStore:
    def __init__(self, engine: AsyncEngine, cipher: CredentialCipher) -> None:
        if engine.url.database != "oj_agent" or engine.url.drivername != "mysql+asyncmy":
            raise ValueError("Credential store requires oj_agent")
        self._engine = engine
        self._cipher = cipher

    async def _row(
        self, connection: AsyncConnection, identifier: str, *, lock: bool = False
    ) -> RowMapping:
        query = select(*credentials.c).where(credentials.c.id == identifier)
        if lock:
            query = query.with_for_update()
        row = (await connection.execute(query)).mappings().first()
        if row is None:
            raise ConfigError("AGENT_CREDENTIAL_NOT_FOUND", 404)
        return row

    def _decrypt(self, row: RowMapping) -> SecretStr:
        return self._cipher.decrypt(
            row["id"], row["key_version"], row["nonce"], row["encrypted_secret"]
        )

    async def read(self, identifier: str) -> SecretStr:
        async with self._engine.connect() as connection:
            row = await self._row(connection, identifier)
            if row["revoked_at"] is not None:
                raise ConfigError("AGENT_CREDENTIAL_UNAVAILABLE", 503)
            return self._decrypt(row)

    async def get(self, identifier: str) -> dict[str, Any]:
        async with self._engine.connect() as connection:
            return summary(await self._row(connection, identifier))

    async def list(self, *, limit: int = 50, offset: int = 0) -> list[dict[str, Any]]:
        if not 1 <= limit <= 100 or not 0 <= offset <= 10_000:
            raise ConfigError("AGENT_CREDENTIAL_INVALID_PAGE")
        async with self._engine.connect() as connection:
            rows = (
                (
                    await connection.execute(
                        select(*credentials.c)
                        .order_by(credentials.c.created_at.desc(), credentials.c.id.desc())
                        .limit(limit)
                        .offset(offset)
                    )
                )
                .mappings()
                .all()
            )
            return [summary(row) for row in rows]

    async def _replay(
        self,
        connection: AsyncConnection,
        write: CredentialWrite,
        fingerprint: str,
        secret: SecretStr | None = None,
    ) -> dict[str, Any] | None:
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
        if audit["fingerprint"] != fingerprint:
            raise ConfigError("AGENT_CREDENTIAL_REQUEST_CONFLICT", 409)
        row = await self._row(connection, audit["credential_id"])
        # 幂等比较解密后的原凭据，不将 Key 或其裸哈希保存到审计。
        if secret is not None and not hmac.compare_digest(
            self._decrypt(row).get_secret_value().encode(), secret.get_secret_value().encode()
        ):
            raise ConfigError("AGENT_CREDENTIAL_REQUEST_CONFLICT", 409)
        return summary(row)

    async def create(self, write: CredentialWrite, name: str, secret: SecretStr) -> dict[str, Any]:
        value = secret.get_secret_value()
        if (
            not 1 <= len(name) <= 128
            or not name.strip()
            or not 1 <= len(value) <= 4096
            or any(ord(char) <= 32 or ord(char) >= 127 for char in value)
        ):
            raise ConfigError("AGENT_CREDENTIAL_INVALID")
        fingerprint = config_hash({**write.model_dump(), "action": "create", "name": name})
        try:
            async with self._engine.begin() as connection:
                replay = await self._replay(connection, write, fingerprint, secret)
                if replay is not None:
                    return replay
                identifier = str(uuid4())
                version, nonce, ciphertext = self._cipher.encrypt(identifier, secret)
                await connection.execute(
                    insert(credentials).values(
                        id=identifier,
                        name=name,
                        encrypted_secret=ciphertext,
                        nonce=nonce,
                        key_version=version,
                        created_by=write.actor_id,
                        created_at=utc_now(),
                    )
                )
                await connection.execute(
                    insert(audits).values(
                        request_id=write.request_id,
                        credential_id=identifier,
                        actor_id=write.actor_id,
                        action="create",
                        fingerprint=fingerprint,
                        created_at=utc_now(),
                    )
                )
                return summary(await self._row(connection, identifier))
        except IntegrityError:
            async with self._engine.connect() as connection:
                replay = await self._replay(connection, write, fingerprint, secret)
                if replay is not None:
                    return replay
            raise ConfigError("AGENT_CREDENTIAL_REQUEST_CONFLICT", 409) from None

    async def revoke(self, write: CredentialWrite, identifier: UUID) -> dict[str, Any]:
        fingerprint = config_hash({**write.model_dump(), "action": "revoke", "id": str(identifier)})
        try:
            async with self._engine.begin() as connection:
                row = await self._row(connection, str(identifier), lock=True)
                replay = await self._replay(connection, write, fingerprint)
                if replay is not None:
                    return replay
                if row["revoked_at"] is not None:
                    raise ConfigError("AGENT_CREDENTIAL_ALREADY_REVOKED", 409)
                await connection.execute(
                    update(credentials)
                    .where(credentials.c.id == str(identifier))
                    .values(revoked_by=write.actor_id, revoked_at=utc_now())
                )
                await connection.execute(
                    insert(audits).values(
                        request_id=write.request_id,
                        credential_id=str(identifier),
                        actor_id=write.actor_id,
                        action="revoke",
                        fingerprint=fingerprint,
                        created_at=utc_now(),
                    )
                )
                return summary(await self._row(connection, str(identifier)))
        except IntegrityError:
            async with self._engine.connect() as connection:
                replay = await self._replay(connection, write, fingerprint)
                if replay is not None:
                    return replay
            raise ConfigError("AGENT_CREDENTIAL_REQUEST_CONFLICT", 409) from None
