"""控制面请求模型；身份和幂等编号不接受正文覆盖。"""

from typing import Any
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field

from app.models.configuration import ConfigKey


class AdminRequest(BaseModel):
    model_config = ConfigDict(extra="forbid", hide_input_in_errors=True)


class CreateConfiguration(AdminRequest):
    key: ConfigKey
    content: dict[str, Any]


class ExpectedConfiguration(AdminRequest):
    expected_id: UUID


class ReplaceConfiguration(ExpectedConfiguration):
    content: dict[str, Any]


class RestoreConfiguration(ExpectedConfiguration):
    source_id: UUID


class PageQuery(AdminRequest):
    page: int = Field(default=1, ge=1, le=501)
    page_size: int = Field(default=20, ge=1, le=100)
    search: str = Field(default="", max_length=64)
    state: str = Field(default="active", pattern=r"^(active|archived|disabled|all)$")
