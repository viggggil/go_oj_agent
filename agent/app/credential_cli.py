"""本机凭据管理：Key 文件/stdin 输入，查询仅输出元数据。"""

import argparse
import asyncio
import base64
import json
import os
import sys
from pathlib import Path
from uuid import UUID

from pydantic import SecretStr, ValidationError

from app.core.credentials import CredentialCipher
from app.core.database import DatabaseProbe
from app.core.settings import ConfigurationError, Settings, load_settings
from app.models.configuration import ConfigError
from app.storage.credentials import CredentialStore, CredentialWrite


def init_keyring(path: Path) -> None:
    content = {"active": "v1", "keys": {"v1": base64.b64encode(os.urandom(32)).decode()}}
    # exclusive create 防止覆盖现有 keyring 导致历史凭据无法解密。
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w") as stream:
        json.dump(content, stream)


async def run(settings: Settings, args: argparse.Namespace) -> object:
    cipher = CredentialCipher.from_file(
        settings.credential_keyring_file,
        allow_insecure_test_file=settings.environment == "test",
    )
    database = DatabaseProbe(settings)
    try:
        if database.engine is None:
            raise ConfigurationError("Credential command requires Agent database")
        store = CredentialStore(database.engine, cipher)
        if args.command == "list":
            return await store.list(limit=args.limit, offset=args.offset)
        if args.command == "get":
            return await store.get(str(args.id))
        write = CredentialWrite(actor_id=args.actor_id, request_id=args.request_id)
        if args.command == "revoke":
            return await store.revoke(write, args.id)
        if args.key_file is not None:
            if args.key_file.stat().st_mode & 0o077:
                raise ConfigError("AGENT_CREDENTIAL_FILE_PERMISSIONS")
            with args.key_file.open("rb") as stream:
                raw = stream.read(4097)
        else:
            raw = sys.stdin.buffer.read(4097)
        if len(raw) > 4096:
            raise ConfigError("AGENT_CREDENTIAL_INVALID")
        return await store.create(write, args.name, SecretStr(raw.decode().strip()))
    finally:
        await database.close()


def main() -> int:
    parser = argparse.ArgumentParser(description="Agent 本机加密凭据管理")
    parser.add_argument("command", choices=["init-keyring", "create", "get", "list", "revoke"])
    parser.add_argument("--keyring-file", type=Path)
    parser.add_argument("--key-file", type=Path)
    parser.add_argument("--name")
    parser.add_argument("--id", type=UUID)
    parser.add_argument("--actor-id", type=int)
    parser.add_argument("--request-id")
    parser.add_argument("--limit", type=int, default=50)
    parser.add_argument("--offset", type=int, default=0)
    args = parser.parse_args()
    try:
        if args.command == "init-keyring":
            if args.keyring_file is None:
                parser.error("init-keyring requires --keyring-file")
            init_keyring(args.keyring_file)
            print(json.dumps({"status": "created"}))
            return 0
        if args.command in {"get", "revoke"} and args.id is None:
            parser.error("command requires --id")
        if args.command == "create" and args.name is None:
            parser.error("create requires --name")
        settings = load_settings()
        if args.keyring_file is not None:
            settings = settings.model_copy(update={"credential_keyring_file": args.keyring_file})
        print(json.dumps(asyncio.run(run(settings, args)), ensure_ascii=False))
        return 0
    except (ConfigError, ConfigurationError) as error:
        print(
            json.dumps({"code": getattr(error, "code", "AGENT_CREDENTIAL_CONFIGURATION_INVALID")})
        )
    except ValidationError:
        print(json.dumps({"code": "AGENT_CREDENTIAL_INVALID"}))
    except Exception:
        print(json.dumps({"code": "AGENT_CREDENTIAL_COMMAND_FAILED"}))
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
