from datetime import UTC, datetime, timedelta
from typing import Any
from uuid import UUID, uuid4

import pytest
from pydantic import ValidationError

from app.models.configuration import AgentConfig, PromptConfig, RuntimeBudget


@pytest.mark.parametrize("system_limit, expected", [(8, 8), (16, 16), (32, 16)])
async def test_effective_tool_budget_uses_configured_system_limit(
    system_limit: int,
    expected: int,
) -> None:
    from app.core.runtime_config import DatabaseConfigReader
    from app.core.settings import Settings
    from app.models.configuration import ModelProfileConfig, SkillConfig
    from app.models.runtime import ChatRequest, Principal
    from app.storage.configuration import ConfigVersion
    from app.tools.registry import ToolRegistry

    prompt = ConfigVersion(str(uuid4()), "prompt", "prompt", PromptConfig(text="规则"))
    skill = ConfigVersion(str(uuid4()), "skill", "skill", SkillConfig(name="技能", text="说明"))
    model = ConfigVersion(str(uuid4()), "model", "model", ModelProfileConfig())
    agent = ConfigVersion(
        str(uuid4()),
        "agent",
        "assistant",
        AgentConfig.model_validate(
            {
                "name": "助手",
                "prompt_id": prompt.id,
                "skill_ids": [skill.id],
                "default_skill_id": skill.id,
                "model_profile_id": model.id,
                "budget": {"max_tool_calls": 16},
            }
        ),
    )
    graph = {v.id: v for v in [prompt, skill, model, agent]}

    class Store:
        async def load_graph(self, key: str) -> tuple[ConfigVersion, dict[str, ConfigVersion]]:
            return agent, graph

    reader = DatabaseConfigReader(
        Store(),  # type: ignore[arg-type]
        Settings(runtime_mode="fake", default_agent_key="assistant", max_tool_calls=system_limit),
        ToolRegistry(),
    )
    snapshot = await reader.read(
        ChatRequest(message="问题"), Principal(user_id=1, request_id="test")
    )
    assert snapshot.budget.max_tool_calls == expected


def test_prompt_variables_are_declared_exactly_and_expressions_are_rejected() -> None:
    assert PromptConfig(text="Explain {language}", variables=("language",))
    with pytest.raises(ValidationError):
        PromptConfig(text="Explain {language}", variables=())
    with pytest.raises(ValidationError):
        PromptConfig(text="Explain {language.upper}", variables=("language",))


def test_test_agent_requires_admin_visibility_and_expiration() -> None:
    values: dict[str, Any] = dict(
        name="Prompt test",
        prompt_id=uuid4(),
        skill_ids=(uuid4(),),
        default_skill_id=None,
        model_profile_id=uuid4(),
        is_test=True,
    )
    values["default_skill_id"] = values["skill_ids"][0]
    with pytest.raises(ValidationError):
        AgentConfig(**values)
    values.update(
        visibility="admin",
        test_expires_at=datetime.now(UTC) + timedelta(hours=1),
    )
    assert AgentConfig(**values).is_test


def test_budget_has_bounded_model_and_tool_limits() -> None:
    assert RuntimeBudget(max_tool_calls=0).max_tool_calls == 0
    with pytest.raises(ValidationError):
        RuntimeBudget(max_model_calls=0)


def test_seed_upgrade_audits_distinguish_dependency_changes_and_source_versions() -> None:
    from app.development_bootstrap import is_seed_request, seed_upgrade_request
    from app.models.configuration import ConfigWrite
    from app.storage.configuration import ConfigVersion

    version = ConfigVersion(str(uuid4()), "prompt", "a" * 64, PromptConfig(text="initial"))
    first = seed_upgrade_request(version, {"text": "first"})
    second = seed_upgrade_request(version, {"text": "second"})
    successor = ConfigVersion(str(uuid4()), version.kind, version.key, version.content)
    assert first == seed_upgrade_request(version, {"text": "first"})
    assert len({first, second, seed_upgrade_request(successor, {"text": "first"})}) == 3
    assert is_seed_request(version, first)
    assert all(
        is_seed_request(version, f"compose.su8.v{n}.prompt.{version.key}") for n in range(1, 7)
    )
    assert not is_seed_request(version, "admin.saved.prompt")
    assert not is_seed_request(version, first + "invalid")
    assert ConfigWrite(
        kind="prompt", key=version.key, actor_id=1, request_id=first, expected_id=UUID(version.id)
    ).expected_id == UUID(version.id)
