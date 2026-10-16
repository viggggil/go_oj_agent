"""运行配置快照接口；PR2 的 demo 配置不代表已发布控制面。"""

from typing import Literal, Protocol, cast

from pydantic import BaseModel, ConfigDict

from app.core.operations import ADMIN_ROLES
from app.core.settings import Settings
from app.models.configuration import (
    AgentConfig,
    ConfigError,
    ConfigKind,
    ModelProfileConfig,
    PromptConfig,
    ProviderConfig,
    SkillConfig,
    config_hash,
    parse_config,
)
from app.models.configuration import RuntimeBudget as RuntimeBudget
from app.models.runtime import ChatRequest, Principal
from app.rag.store import KnowledgeStore
from app.storage.configuration import ConfigurationStore, ConfigVersion
from app.tools.registry import ToolRegistry


class ConfigSnapshot(BaseModel):
    model_config = ConfigDict(frozen=True)

    source: Literal["demo_environment", "database"] = "demo_environment"
    version: str = "pr2-demo-v1"
    runtime: Literal["fake", "langgraph_fake", "model"]
    budget: RuntimeBudget
    allowed_tools: tuple[str, ...] = ()
    agent_key: str = "demo"
    skill_key: str = "demo"
    prompt_id: str | None = None
    skill_id: str | None = None
    model_profile_id: str | None = None
    prompt_text: str | None = None
    skill_prompt_id: str | None = None
    skill_prompt_text: str | None = None
    config_hash: str | None = None
    configurations: dict[str, dict[str, object]] = {}
    is_test: bool = False
    execution_mode: str = "direct"
    skill_selection: Literal["explicit", "automatic"] = "explicit"
    skill_references: tuple[dict[str, object], ...] = ()
    provider_id: str | None = None
    model_profile: dict[str, object] | None = None
    provider_config: dict[str, object] | None = None
    knowledge_scope: tuple[str, ...] = ()
    knowledge_versions: tuple[str, ...] = ()


class ConfigSnapshotReader(Protocol):
    async def read(self, request: ChatRequest, principal: Principal) -> ConfigSnapshot: ...


class DemoConfigReader:
    def __init__(self, settings: Settings) -> None:
        if (
            settings.runtime_mode not in {"fake", "langgraph_fake"}
            or settings.environment == "production"
        ):
            raise ValueError("Demo runtime must be explicitly enabled outside production")
        self._snapshot = ConfigSnapshot(
            runtime=cast(Literal["fake", "langgraph_fake"], settings.runtime_mode),
            budget=RuntimeBudget(
                max_run_seconds=settings.max_run_seconds,
                max_output_chars=settings.max_output_chars,
                max_events=settings.max_run_events,
            ),
            allowed_tools=(
                "get_problem",
                "list_problems",
                "get_submission",
                "get_submission_source",
                "get_judge_result",
                "list_submissions",
            )
            if settings.business_tools_enabled
            else (),
        )

    async def read(self, request: ChatRequest, principal: Principal) -> ConfigSnapshot:
        if request.agent_key not in (None, "demo") or request.skill_key not in (None, "demo"):
            raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
        return self._snapshot.model_copy(deep=True)


class DatabaseConfigReader:
    def __init__(
        self,
        store: ConfigurationStore,
        settings: Settings,
        registry: ToolRegistry,
        knowledge_store: KnowledgeStore | None = None,
    ) -> None:
        if settings.runtime_mode == "disabled":
            raise ValueError("Database runtime requires explicit activation")
        self._store = store
        self._settings = settings
        self._registry = registry
        self._knowledge_store = knowledge_store

    async def read(
        self,
        request: ChatRequest,
        principal: Principal,
        *,
        pinned: ConfigSnapshot | None = None,
        render_prompts: bool = True,
    ) -> ConfigSnapshot:
        from datetime import UTC, datetime

        agent_key = request.agent_key or self._settings.default_agent_key
        if pinned is None:
            root, graph = await self._store.load_graph(agent_key)
        else:
            if agent_key != pinned.agent_key:
                raise ConfigError("AGENT_CONFIGURATION_INVALID", 409)
            graph = {
                identifier: ConfigVersion(
                    id=identifier,
                    kind=cast(ConfigKind, value["kind"]),
                    key=str(value["key"]),
                    content=parse_config(cast(ConfigKind, value["kind"]), value["content"]),
                )
                for identifier, value in pinned.configurations.items()
            }
            if pinned.version not in graph:
                raise ConfigError("AGENT_CONFIGURATION_INVALID", 409)
            root = graph[pinned.version]
        agent = root.content
        if not isinstance(agent, AgentConfig):
            raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
        if agent.visibility == "admin" and not principal.roles.intersection(ADMIN_ROLES):
            raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
        if agent.test_expires_at is not None and agent.test_expires_at <= datetime.now(UTC):
            raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
        skill_id = str(agent.default_skill_id)
        if request.skill_key is not None:
            matches = [
                str(value)
                for value in agent.skill_ids
                if graph[str(value)].key == request.skill_key
            ]
            if len(matches) != 1:
                raise ConfigError("AGENT_CONFIGURATION_NOT_FOUND", 404)
            skill_id = matches[0]
        skill_version = graph[skill_id]
        skill = skill_version.content
        if not isinstance(skill, SkillConfig):
            raise ConfigError("AGENT_CONFIGURATION_INVALID", 409)
        model_version = graph[str(agent.model_profile_id)]
        profile = model_version.content
        if not isinstance(profile, ModelProfileConfig):
            raise ConfigError("AGENT_CONFIGURATION_INVALID", 409)
        real = self._settings.runtime_mode == "model"
        if real != (profile.provider == "responses") or (
            real and skill.execution_mode not in {"direct", "react"}
        ):
            raise ConfigError("AGENT_CONFIGURATION_UNSUPPORTED", 409)
        provider_version = graph[str(profile.provider_id)] if real else None
        if provider_version is not None and not isinstance(
            provider_version.content, ProviderConfig
        ):
            raise ConfigError("AGENT_CONFIGURATION_INVALID", 409)
        prompt_version = graph[str(agent.prompt_id)]
        skill_prompt_version = graph[str(skill.prompt_id)] if skill.prompt_id else None
        if not isinstance(prompt_version.content, PromptConfig) or (
            skill_prompt_version is not None
            and not isinstance(skill_prompt_version.content, PromptConfig)
        ):
            raise ConfigError("AGENT_CONFIGURATION_INVALID", 409)

        def render(prompt: PromptConfig) -> str:
            if not render_prompts:
                return prompt.text
            values = request.context.model_dump()
            if any(values[name] is None for name in prompt.variables):
                raise ConfigError("AGENT_PROMPT_CONTEXT_REQUIRED")
            return prompt.text.format(**values)

        def render_skill(value: SkillConfig) -> str:
            if value.prompt_id is not None:
                return render(cast(PromptConfig, graph[str(value.prompt_id)].content))
            if value.variables is not None:
                return render(PromptConfig(text=value.text or "", variables=value.variables))
            return value.text or ""

        # 未指定 Skill 时让模型在同一次回答中参考绑定技能；不额外调用分类模型。
        references: list[dict[str, object]] = []
        reference_skills: list[SkillConfig] = []
        automatic = real and request.skill_key is None
        if automatic:
            for identifier in dict.fromkeys([agent.default_skill_id, *agent.skill_ids]):
                version = graph[str(identifier)]
                candidate = cast(SkillConfig, version.content)
                try:
                    text = render_skill(candidate)
                except ConfigError as error:
                    if error.code != "AGENT_PROMPT_CONTEXT_REQUIRED":
                        raise
                    # 例如提交诊断需要 submission_id，不能让它阻断普通算法对话。
                    continue
                references.append(
                    {
                        "id": version.id,
                        "key": version.key,
                        "name": candidate.name,
                        "description": candidate.description,
                        "text": text,
                    }
                )
                reference_skills.append(candidate)
            if not references:
                raise ConfigError("AGENT_PROMPT_CONTEXT_REQUIRED")

        system_budget = RuntimeBudget(
            max_run_seconds=self._settings.max_run_seconds,
            max_output_chars=self._settings.max_output_chars,
            max_events=self._settings.max_run_events,
            max_model_calls=self._settings.max_model_calls,
            max_input_tokens=self._settings.max_input_tokens,
            max_output_tokens=self._settings.max_output_tokens,
        )
        budgets = [agent.budget.model_dump(), system_budget.model_dump()]
        active_skills = reference_skills if automatic else [skill]
        budgets.extend(
            value.budget.model_dump() for value in active_skills if value.budget is not None
        )
        budget = RuntimeBudget.model_validate(
            {name: min(item[name] for item in budgets) for name in budgets[0]}
        )
        system_tools = {
            name
            for name in self._registry.names()
            if self._registry.get(name).spec.enabled
            and self._registry.get(name).spec.side_effect == "read"
        }
        skill_tools = {
            name
            for value in active_skills
            if not automatic or value.execution_mode == "react"
            for name in value.allowed_tools
        }
        # 分组绑定本身授予知识工具；不要求用户同时修改每个 Skill 的工具列表。
        system_tools.discard("retrieve_knowledge")
        permitted_tools = system_tools & set(agent.allowed_tools) & skill_tools
        scope = tuple(str(value) for value in agent.knowledge_scope)
        knowledge_versions: tuple[str, ...] = ()
        if real and scope and self._knowledge_store is not None:
            knowledge_versions = (
                pinned.knowledge_versions
                if pinned is not None
                else tuple(
                    row["id"] for row in await self._knowledge_store.active(list(scope), None)
                )
            )
            if "retrieve_knowledge" in self._registry.names():
                permitted_tools.add("retrieve_knowledge")
        tools = tuple(sorted(permitted_tools))
        configurations: dict[str, dict[str, object]] = {
            version.id: {
                "kind": version.kind,
                "key": version.key,
                "content": version.content.model_dump(mode="json"),
            }
            for version in graph.values()
        }
        if self._settings.runtime_mode == "disabled":
            raise ConfigError("AGENT_CONFIGURATION_INVALID", 409)
        snapshot = ConfigSnapshot(
            source="database",
            version=root.id,
            runtime=self._settings.runtime_mode,
            agent_key=agent_key,
            skill_key=skill_version.key,
            prompt_id=prompt_version.id,
            prompt_text=render(prompt_version.content),
            skill_prompt_id=skill_prompt_version.id if skill_prompt_version else None,
            skill_prompt_text=None if automatic else render_skill(skill),
            skill_id=skill_id,
            model_profile_id=str(agent.model_profile_id),
            allowed_tools=tools,
            budget=budget,
            configurations=configurations,
            is_test=agent.is_test,
            execution_mode=("react" if tools else "direct")
            if automatic or "retrieve_knowledge" in tools
            else skill.execution_mode,
            skill_selection="automatic" if automatic else "explicit",
            skill_references=tuple(references),
            provider_id=provider_version.id if provider_version is not None else None,
            model_profile=profile.model_dump(mode="json") if real else None,
            provider_config=provider_version.content.model_dump(mode="json")
            if provider_version is not None
            else None,
            knowledge_scope=scope,
            knowledge_versions=knowledge_versions,
        )
        return snapshot.model_copy(
            update={"config_hash": config_hash(snapshot.model_dump(mode="json"))}
        )
