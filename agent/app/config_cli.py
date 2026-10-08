"""部署人员使用的本机配置命令；信任边界为 oj_agent 数据库凭据。"""

import argparse
import asyncio
import json
import logging
from pathlib import Path
from typing import Any, cast
from uuid import UUID

from pydantic import ValidationError

from app.clients.business import build_business_clients
from app.core.database import DatabaseProbe
from app.core.settings import ConfigurationError, Settings, load_settings
from app.models.configuration import AgentConfig, ConfigError, ConfigKind, ConfigWrite
from app.storage.configuration import ConfigAction, ConfigurationStore, ConfigVersion
from app.tools.business import build_business_tool_registry
from app.tools.registry import ToolRegistry


def _read_json(path: Path) -> dict[str, Any]:
    with path.open("rb") as stream:
        value = stream.read(262_145)
    if len(value) > 262_144:
        raise ConfigError("AGENT_CONFIGURATION_FILE_TOO_LARGE")
    content = json.loads(value)
    if not isinstance(content, dict):
        raise ConfigError("AGENT_CONFIGURATION_INVALID")
    return content


def _summary(version: ConfigVersion, *, content: bool = False) -> dict[str, Any]:
    result: dict[str, Any] = {
        "id": version.id,
        "kind": version.kind,
        "key": version.key,
        "archived": version.archived,
        "disabled": version.disabled,
    }
    if content:
        result["content"] = version.content.model_dump(mode="json")
    return result


async def run_command(settings: Settings, args: argparse.Namespace) -> Any:
    probe = DatabaseProbe(settings)
    clients = None
    try:
        if probe.engine is None:
            raise ConfigurationError("Configuration command requires an Agent database")
        registry = ToolRegistry()
        if settings.business_tools_enabled:
            clients = build_business_clients(settings)
            registry = build_business_tool_registry(
                clients, settings.tool_max_result_bytes, settings.tool_max_page_size
            )
        store = ConfigurationStore(probe.engine, registry)
        if args.command == "get":
            version = (
                await store.get_version(str(args.id))
                if args.id
                else await store.resolve(args.kind, args.key)
            )
            if version.kind != args.kind or version.key != args.key:
                raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
            return _summary(version, content=True)
        if args.command == "list":
            return [
                _summary(item)
                for item in await store.list_versions(
                    args.kind, args.key, limit=args.limit, offset=args.offset
                )
            ]
        write = ConfigWrite(
            kind=args.kind,
            key=args.key,
            actor_id=args.actor_id,
            request_id=args.request_id,
            expected_id=args.expected_id,
        )
        if args.command == "put":
            version = await store.create_or_replace(write, _read_json(args.file))
        elif args.command == "restore":
            source = await store.get_version(str(args.source_id))
            version = await store.create_or_replace(
                write.model_copy(update={"source_id": args.source_id}),
                source.content.model_dump(mode="json"),
            )
        elif args.command == "bootstrap":
            document = _read_json(args.file)
            identifiers: dict[str, UUID] = {}
            for name, kind in (
                ("prompt", "prompt"),
                ("skill_prompt", "prompt"),
                ("model", "model"),
                ("skill", "skill"),
                ("agent", "agent"),
            ):
                item = document[name]
                content = dict(item["content"])
                if kind == "skill":
                    content["prompt_id"] = str(identifiers["skill_prompt"])
                if kind == "agent":
                    content.update(
                        prompt_id=str(identifiers["prompt"]),
                        skill_ids=[str(identifiers["skill"])],
                        default_skill_id=str(identifiers["skill"]),
                        model_profile_id=str(identifiers["model"]),
                    )
                part = ConfigWrite(
                    kind=cast(ConfigKind, kind),
                    key=item["key"],
                    actor_id=args.actor_id,
                    request_id=f"{args.request_id}.{name}",
                )
                version = await store.create_or_replace(part, content)
                identifiers[name] = UUID(version.id)
            return {key: str(value) for key, value in identifiers.items()}
        elif args.command == "clone-test":
            source = await store.resolve("agent", args.source_key)
            if source.archived or source.disabled or not isinstance(source.content, AgentConfig):
                raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
            body = source.content.model_dump(mode="json")
            body.update(visibility="admin", is_test=True, test_expires_at=args.expires_at)
            if args.prompt_id is not None:
                body["prompt_id"] = str(args.prompt_id)
            version = await store.create_or_replace(write, body)
        else:
            version = await store.change_state(write, cast(ConfigAction, args.command))
        return _summary(version)
    finally:
        try:
            if clients is not None:
                await clients.close()
        finally:
            await probe.close()


def main() -> int:
    parser = argparse.ArgumentParser(description="Agent 本机配置管理（数据库凭据授权）")
    parser.add_argument(
        "command",
        choices=[
            "get",
            "list",
            "put",
            "restore",
            "archive",
            "disable",
            "enable",
            "bootstrap",
            "clone-test",
        ],
    )
    parser.add_argument("--kind", choices=["prompt", "skill", "model", "agent"], default="agent")
    parser.add_argument("--key", default="learning_assistant")
    parser.add_argument("--actor-id", type=int)
    parser.add_argument("--request-id")
    parser.add_argument("--expected-id", type=UUID)
    parser.add_argument("--source-id", type=UUID)
    parser.add_argument("--source-key")
    parser.add_argument("--prompt-id", type=UUID)
    parser.add_argument("--expires-at")
    parser.add_argument("--id", type=UUID)
    parser.add_argument("--file", type=Path)
    parser.add_argument("--limit", type=int, default=50)
    parser.add_argument("--offset", type=int, default=0)
    args = parser.parse_args()
    if args.command not in {"get", "list"} and (args.actor_id is None or args.request_id is None):
        parser.error("Mutations require --actor-id and --request-id")
    if args.command in {"put", "bootstrap"} and args.file is None:
        parser.error("This command requires --file")
    if args.command == "restore" and args.source_id is None:
        parser.error("Restore requires --source-id")
    if args.command == "clone-test" and (
        args.source_key is None or args.expires_at is None or args.kind != "agent"
    ):
        parser.error("Clone-test requires --kind agent, --source-key and --expires-at")
    try:
        result = asyncio.run(run_command(load_settings(), args))
        print(json.dumps(result, ensure_ascii=False), flush=True)
        return 0
    except Exception as exc:
        if isinstance(exc, ConfigError):
            code = exc.code
        elif isinstance(exc, (ValueError, ValidationError, KeyError, TypeError)):
            code = "AGENT_CONFIGURATION_INVALID"
        else:
            code = "AGENT_CONFIGURATION_FAILED"
        # SQL、模板和环境配置异常不得原样进入 stderr。
        logging.getLogger(__name__).error(code)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
