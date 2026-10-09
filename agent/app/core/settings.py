"""环境配置只承载部署信息，不内置业务 Prompt 或模型。"""

from pathlib import Path
from typing import Literal

from pydantic import Field, SecretStr, ValidationError, field_validator, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict
from sqlalchemy.engine import make_url
from sqlalchemy.exc import ArgumentError


class ConfigurationError(RuntimeError):
    """可以安全输出的启动配置错误。"""


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_prefix="AGENT_",
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
        hide_input_in_errors=True,
        allow_inf_nan=False,
    )

    environment: Literal["development", "test", "production"] = "development"
    host: str = Field(default="127.0.0.1", min_length=1)
    port: int = Field(default=8000, ge=1, le=65535)
    log_level: Literal["DEBUG", "INFO", "WARNING", "ERROR", "CRITICAL"] = "INFO"
    database_url: SecretStr | None = None
    readiness_timeout_seconds: float = Field(default=2, gt=0, le=30)
    shutdown_timeout_seconds: int = Field(default=10, ge=1, le=60)
    runtime_mode: Literal["disabled", "fake", "langgraph_fake", "model"] = "disabled"
    config_mode: Literal["demo", "database"] = "demo"
    default_agent_key: str = Field(default="learning_assistant", pattern=r"^[a-z][a-z0-9_]{1,63}$")
    max_run_seconds: float = Field(default=30, gt=0, le=300)
    max_output_chars: int = Field(default=8_000, ge=1, le=32_000)
    max_run_events: int = Field(default=1_000, ge=3, le=10_000)
    chat_enabled: bool = False
    gateway_public_key_file: Path | None = None
    gateway_key_id: str = Field(default="gateway-internal-2026-09", min_length=1, max_length=128)
    max_request_bytes: int = Field(default=262_144, ge=1024, le=1_048_576)
    max_concurrent_runs: int = Field(default=8, ge=1, le=128)
    preflight_timeout_seconds: float = Field(default=5, gt=0, le=30)
    sse_heartbeat_seconds: float = Field(default=5, gt=0, le=30)
    sse_write_timeout_seconds: float = Field(default=5, gt=0, le=30)
    agent_private_key_file: Path | None = None
    agent_key_id: str = Field(default="agent-internal-2026-10", min_length=1, max_length=128)
    agent_issuer: str = Field(default="go-oj-agent", min_length=1, max_length=128)
    agent_subject: str = Field(default="agent-service", min_length=1, max_length=128)
    agent_token_ttl_seconds: int = Field(default=30, ge=1, le=60)
    problem_service_endpoint: str = Field(
        default="problem-service:9002", min_length=1, max_length=255
    )
    judge_service_endpoint: str = Field(default="judge-service:9003", min_length=1, max_length=255)
    tool_timeout_seconds: float = Field(default=5, gt=0, le=30)
    tool_max_page_size: int = Field(default=50, ge=1, le=100)
    tool_max_result_bytes: int = Field(default=256_000, ge=1024, le=1_048_576)
    business_tools_enabled: bool = False
    credential_keyring_file: Path | None = None
    provider_allowed_origins: tuple[str, ...] = ()
    provider_allow_private_network: bool = False
    max_model_calls: int = Field(default=8, ge=1, le=16)
    max_input_tokens: int = Field(default=32_000, ge=1, le=128_000)
    max_output_tokens: int = Field(default=8_000, ge=1, le=32_000)

    @field_validator("host", mode="before")
    @classmethod
    def normalize_host(cls, value: object) -> object:
        if isinstance(value, str):
            return value.strip()
        return value

    @field_validator("database_url", mode="before")
    @classmethod
    def normalize_database_url(cls, value: object) -> object:
        if isinstance(value, str) and not value.strip():
            return None
        return value

    @field_validator("database_url")
    @classmethod
    def validate_database_url(cls, value: SecretStr | None) -> SecretStr | None:
        if value is None:
            return None
        try:
            url = make_url(value.get_secret_value())
            # 访问 port 可发现 URL 中非法的端口文本。
            _ = url.port
        except (ArgumentError, ValueError):
            raise ValueError("Invalid database URL") from None
        if url.drivername != "mysql+asyncmy" or url.database != "oj_agent":
            raise ValueError("Database must use mysql+asyncmy and the oj_agent schema")
        if not url.host or not url.host.strip():
            raise ValueError("Database host is required")
        if url.port is not None and not 1 <= url.port <= 65535:
            raise ValueError("Database port is out of range")
        return value

    @model_validator(mode="after")
    def validate_production(self) -> "Settings":
        if self.runtime_mode == "model" and (
            self.config_mode != "database"
            or self.credential_keyring_file is None
            or not self.provider_allowed_origins
        ):
            raise ValueError("Model runtime requires database config, keyring and allowed origins")
        if self.environment == "production" and self.provider_allow_private_network:
            raise ValueError("Production cannot allow private model endpoints")
        if self.business_tools_enabled and self.agent_private_key_file is None:
            raise ValueError("Business tools require Agent service identity")
        if self.chat_enabled and (
            self.database_url is None
            or self.gateway_public_key_file is None
            or self.runtime_mode == "disabled"
        ):
            raise ValueError("Chat requires database, Gateway public key and an enabled runtime")
        if self.environment == "production" and self.database_url is None:
            raise ValueError("Production requires a database URL")
        if self.environment == "production" and self.runtime_mode in {"fake", "langgraph_fake"}:
            raise ValueError("Production cannot use a demo runtime")
        return self


def load_settings() -> Settings:
    """配置错误只暴露字段名称，不暴露输入、DSN 或异常上下文。"""
    try:
        return Settings()
    except ValidationError as exc:
        fields = sorted(
            {
                ".".join(str(part) for part in error["loc"]) or "environment/database_url"
                for error in exc.errors(
                    include_input=False, include_context=False, include_url=False
                )
            }
        )
        raise ConfigurationError("Invalid Agent configuration: " + ", ".join(fields)) from None
