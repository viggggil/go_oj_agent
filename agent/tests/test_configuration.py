from datetime import UTC, datetime, timedelta
from typing import Any
from uuid import uuid4

import pytest
from pydantic import ValidationError

from app.models.configuration import AgentConfig, PromptConfig, RuntimeBudget


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
