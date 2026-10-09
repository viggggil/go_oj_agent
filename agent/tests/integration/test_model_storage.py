"""真实 MySQL 凭据审计/撤销、Provider 版本和配置解析。"""

import asyncio
from pathlib import Path
from uuid import UUID, uuid4

import pytest
from pydantic import SecretStr
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncEngine

from app.core.credentials import CredentialCipher
from app.core.provider_policy import ProviderPolicy
from app.core.runtime_config import DatabaseConfigReader
from app.core.settings import Settings
from app.models.configuration import ConfigError
from app.models.runtime import ChatRequest, Principal
from app.storage.configuration import ConfigurationStore
from app.storage.credentials import CredentialStore, CredentialWrite
from app.storage.schema import agent_credentials, agent_provider_credentials
from app.tools.registry import ToolRegistry
from tests.integration.test_configuration import config_engine as config_engine
from tests.integration.test_configuration import write

pytestmark = pytest.mark.integration


def credential_store(engine: AsyncEngine) -> CredentialStore:
    return CredentialStore(engine, CredentialCipher("v1", {"v1": b"k" * 32}))


async def test_encrypted_credential_replay_conflict_revoke_and_concurrent_creation(
    config_engine: AsyncEngine,
) -> None:
    store = credential_store(config_engine)
    command = CredentialWrite(actor_id=7, request_id=str(uuid4()))
    results = await asyncio.gather(
        *(store.create(command, "relay", SecretStr("private-api-key")) for _ in range(6))
    )
    assert len({result["id"] for result in results}) == 1
    identifier = results[0]["id"]
    assert "private-api-key" not in repr(results)
    async with config_engine.connect() as connection:
        row = (
            await connection.execute(
                select(agent_credentials.c.encrypted_secret).where(
                    agent_credentials.c.id == identifier
                )
            )
        ).one()
        assert b"private-api-key" not in row.encrypted_secret
    assert (await store.read(identifier)).get_secret_value() == "private-api-key"
    with pytest.raises(ConfigError, match="REQUEST_CONFLICT"):
        await store.create(command, "relay", SecretStr("different-api-key"))
    revoke = CredentialWrite(actor_id=7, request_id=str(uuid4()))
    assert (await store.revoke(revoke, UUID(identifier)))["revoked"]
    assert (await store.revoke(revoke, UUID(identifier)))["revoked"]
    with pytest.raises(ConfigError, match="UNAVAILABLE"):
        await store.read(identifier)
    with pytest.raises(ConfigError, match="ALREADY_REVOKED"):
        await store.revoke(CredentialWrite(actor_id=7, request_id=str(uuid4())), UUID(identifier))


async def test_provider_graph_pins_model_and_rejects_revoked_credential(
    config_engine: AsyncEngine,
) -> None:
    settings = Settings(
        config_mode="database",
        runtime_mode="model",
        credential_keyring_file=Path("/not-read-in-this-test"),
        provider_allowed_origins=("https://relay.example",),
    )
    registry = ToolRegistry()
    store = ConfigurationStore(config_engine, registry, provider_policy=ProviderPolicy(settings))
    credentials = credential_store(config_engine)
    credential = await credentials.create(
        CredentialWrite(actor_id=7, request_id=str(uuid4())), "relay", SecretStr("private-api-key")
    )
    prefix = "model_" + uuid4().hex
    provider = await store.create_or_replace(
        write("provider", prefix + "_p"),
        {
            "name": "relay",
            "base_url": "https://relay.example/v1",
            "credential_id": credential["id"],
        },
    )
    model = await store.create_or_replace(
        write("model", prefix + "_m"),
        {"provider": "responses", "model": "deepseek-v4-flash", "provider_id": provider.id},
    )
    prompt = await store.create_or_replace(write("prompt", prefix + "_t"), {"text": "解释算法"})
    skill = await store.create_or_replace(
        write("skill", prefix + "_s"), {"name": "直接回答", "prompt_id": prompt.id}
    )
    agent = await store.create_or_replace(
        write("agent", prefix),
        {
            "name": "学习助手",
            "prompt_id": prompt.id,
            "skill_ids": [skill.id],
            "default_skill_id": skill.id,
            "model_profile_id": model.id,
        },
    )
    reader = DatabaseConfigReader(store, settings, registry)
    snapshot = await reader.read(
        ChatRequest(message="算法", agent_key=agent.key), Principal(user_id=7, request_id="request")
    )
    assert snapshot.provider_id == provider.id and snapshot.model_profile is not None
    assert snapshot.model_profile["model"] == "deepseek-v4-flash"
    assert "private-api-key" not in snapshot.model_dump_json()
    new = await store.create_or_replace(
        write("provider", provider.key, provider.id),
        {
            "name": "relay",
            "base_url": "https://relay.example/v2",
            "credential_id": credential["id"],
        },
    )
    assert new.id != provider.id and (await store.get_version(provider.id)).archived
    assert (
        await reader.read(
            ChatRequest(message="算法", agent_key=agent.key),
            Principal(user_id=7, request_id="request"),
        )
    ).provider_id == provider.id
    async with config_engine.connect() as connection:
        links = (
            (
                await connection.execute(
                    select(agent_provider_credentials.c.credential_id).where(
                        agent_provider_credentials.c.version_id == provider.id
                    )
                )
            )
            .scalars()
            .all()
        )
        assert links == [credential["id"]]
    await credentials.revoke(
        CredentialWrite(actor_id=7, request_id=str(uuid4())), UUID(credential["id"])
    )
    with pytest.raises(ConfigError, match="UNAVAILABLE"):
        await reader.read(
            ChatRequest(message="算法", agent_key=agent.key),
            Principal(user_id=7, request_id="request"),
        )
    with pytest.raises(ConfigError, match="UNAVAILABLE"):
        await store.create_or_replace(
            write("provider", prefix + "_bad"),
            {
                "name": "relay",
                "base_url": "https://relay.example/v1",
                "credential_id": credential["id"],
            },
        )
