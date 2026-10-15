"""管理员知识 API 输入；正文 UTF-8 JSON 传输，不接受任意对象路径。"""

from typing import Literal
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator

MODEL = "BAAI/bge-small-zh-v1.5"
REVISION = "7999e1d3359715c523056ef9478215996d62a620"
SPLITTER = "markdown-token-v1"
DIMENSIONS = 512


class Input(BaseModel):
    model_config = ConfigDict(extra="forbid", allow_inf_nan=False)


class BaseInput(Input):
    name: str = Field(min_length=1, max_length=128)


class Upload(Input):
    base_id: UUID
    document_id: UUID | None = None
    expected_version: UUID | None = None
    title: str = Field(min_length=1, max_length=128)
    kind: Literal["editorial", "lecture", "platform"] = "editorial"
    problem_id: int | None = Field(default=None, gt=0)
    source_url: str = Field(default="", max_length=512)
    tags: list[str] = Field(default_factory=list, max_length=20)
    filename: str = Field(min_length=1, max_length=128)
    content: str = Field(min_length=1, max_length=120_000)

    @field_validator("filename")
    @classmethod
    def filename_valid(cls, value: str) -> str:
        if "/" in value or "\\" in value or not value.lower().endswith((".md", ".txt")):
            raise ValueError("UTF-8 Markdown/TXT only")
        return value

    @field_validator("content")
    @classmethod
    def content_valid(cls, value: str) -> str:
        if not value.strip() or "\x00" in value or len(value.encode("utf-8")) > 120_000:
            raise ValueError("Invalid or oversized document")
        return value

    @field_validator("tags")
    @classmethod
    def tags_valid(cls, value: list[str]) -> list[str]:
        if any(not tag.strip() or len(tag) > 64 for tag in value) or len(set(value)) != len(value):
            raise ValueError("Invalid tags")
        return value

    @model_validator(mode="after")
    def replacement_valid(self) -> "Upload":
        if (self.document_id is None) != (self.expected_version is None):
            raise ValueError("Replacement requires document and expected version")
        return self


class StateInput(Input):
    expected_version: UUID


class SearchInput(Input):
    base_ids: list[UUID] = Field(min_length=1, max_length=20)
    query: str = Field(min_length=1, max_length=400)
    problem_id: int | None = Field(default=None, gt=0)
    top_k: int = Field(default=5, ge=1, le=10)


class Page(Input):
    page: int = Field(default=1, ge=1, le=10000)
    page_size: int = Field(default=20, ge=1, le=50)
    base_id: UUID | None = None
