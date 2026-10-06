"""配置与 Secret 泄露负向测试。"""

from pathlib import Path

import pytest
from pydantic import SecretStr, ValidationError

from app.core.settings import ConfigurationError, Settings, load_settings


def test_standalone_defaults_need_no_model_key() -> None:
    settings = load_settings()
    assert settings.host == "127.0.0.1"
    assert settings.port == 8000
    assert settings.database_url is None
    assert settings.environment == "development"


def test_environment_overrides_dotenv(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> None:
    (tmp_path / ".env").write_text(
        "AGENT_PORT=8010\nAGENT_LOG_LEVEL=WARNING\nUNRELATED_SETTING=ignored\n",
        encoding="utf-8",
    )
    monkeypatch.setenv("AGENT_PORT", "8020")
    settings = load_settings()
    assert settings.port == 8020
    assert settings.log_level == "WARNING"


@pytest.mark.parametrize(
    ("name", "value"),
    [
        ("AGENT_PORT", "0"),
        ("AGENT_PORT", "65536"),
        ("AGENT_ENVIRONMENT", "unknown"),
        ("AGENT_LOG_LEVEL", "unknown"),
        ("AGENT_HOST", ""),
        ("AGENT_HOST", "   "),
        ("AGENT_READINESS_TIMEOUT_SECONDS", "0"),
        ("AGENT_READINESS_TIMEOUT_SECONDS", "31"),
        ("AGENT_READINESS_TIMEOUT_SECONDS", "nan"),
        ("AGENT_SHUTDOWN_TIMEOUT_SECONDS", "0"),
    ],
)
def test_invalid_settings_fail_before_startup(
    monkeypatch: pytest.MonkeyPatch, name: str, value: str
) -> None:
    monkeypatch.setenv(name, value)
    with pytest.raises(ConfigurationError, match="Invalid Agent configuration"):
        load_settings()


@pytest.mark.parametrize(
    "url",
    [
        "not-a-url-secret",
        "sqlite+aiosqlite:///oj_agent",
        "mysql+asyncmy://agent:secret@127.0.0.1/oj_submission",
        "mysql+asyncmy://agent:secret@127.0.0.1:bad/oj_agent",
        "mysql+asyncmy://agent:secret@127.0.0.1:0/oj_agent",
        "mysql+asyncmy://agent:secret@127.0.0.1:65536/oj_agent",
        "mysql+asyncmy://agent:secret@/oj_agent",
    ],
)
def test_only_agent_database_is_allowed(url: str) -> None:
    with pytest.raises(ValidationError):
        Settings(database_url=SecretStr(url))


def test_database_password_is_redacted() -> None:
    settings = Settings(
        database_url=SecretStr("mysql+asyncmy://agent:private-password@127.0.0.1/oj_agent")
    )
    assert settings.database_url is not None
    assert "private-password" not in str(settings)
    assert "private-password" not in settings.model_dump_json()


def test_config_error_does_not_include_input(monkeypatch: pytest.MonkeyPatch) -> None:
    secret = "mysql+asyncmy://agent:sensitive-password@db:bad/oj_agent"
    monkeypatch.setenv("AGENT_DATABASE_URL", secret)
    with pytest.raises(ConfigurationError) as error:
        load_settings()
    assert "database_url" in str(error.value)
    assert "sensitive-password" not in str(error.value)
    assert secret not in str(error.value)
    assert error.value.__suppress_context__


def test_empty_database_setting_is_unconfigured(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AGENT_DATABASE_URL", "")
    assert load_settings().database_url is None


def test_production_cannot_start_without_database(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AGENT_ENVIRONMENT", "production")
    with pytest.raises(ConfigurationError):
        load_settings()
