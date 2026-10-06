"""环境配置只承载部署信息，不内置业务 Prompt 或模型。"""

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
        if self.environment == "production" and self.database_url is None:
            raise ValueError("Production requires a database URL")
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
