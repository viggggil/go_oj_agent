"""评测数据集的严格输入模型；样例内容视为不可信数据。"""

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator


class EvalTurn(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)

    role: Literal["user", "assistant"]
    content: str = Field(min_length=1, max_length=32_000)

    @field_validator("content")
    @classmethod
    def nonempty(cls, value: str) -> str:
        if not value.strip():
            raise ValueError("Turn content cannot be blank")
        return value


class EvalExpectations(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)

    required_facts: list[str] = Field(default_factory=list, max_length=64)
    required_tools: list[str] = Field(default_factory=list, max_length=32)
    forbidden_tools: list[str] = Field(default_factory=list, max_length=32)
    required_evidence: list[str] = Field(default_factory=list, max_length=64)
    forbidden_claims: list[str] = Field(default_factory=list, max_length=64)
    must_not_leak: list[str] = Field(default_factory=list, max_length=32)
    rubric: list[str] = Field(default_factory=list, max_length=16)


class EvalCase(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)

    id: str = Field(min_length=1, max_length=128, pattern=r"^[a-zA-Z0-9][a-zA-Z0-9_.:-]*$")
    category: str = Field(min_length=1, max_length=64)
    title: str = Field(min_length=1, max_length=256)
    agent_key: str | None = Field(default=None, max_length=64)
    turns: list[EvalTurn] = Field(min_length=1, max_length=32)
    fixture: dict[str, Any] = Field(default_factory=dict)
    expectations: EvalExpectations = Field(default_factory=EvalExpectations)


class EvalDatasetUpload(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)

    name: str = Field(min_length=1, max_length=128)
    description: str = Field(default="", max_length=512)
    content: str = Field(min_length=1, max_length=900_000)

    @field_validator("content")
    @classmethod
    def utf8_jsonl(cls, value: str) -> str:
        if "\x00" in value or not value.strip():
            raise ValueError("Invalid dataset content")
        return value


class EvalPage(BaseModel):
    model_config = ConfigDict(extra="forbid")

    page: int = Field(default=1, ge=1, le=501)
    page_size: int = Field(default=20, ge=1, le=100)
    search: str = Field(default="", max_length=64)


def parse_jsonl(content: str) -> list[EvalCase]:
    import json

    cases: list[EvalCase] = []
    seen: set[str] = set()
    for line_number, line in enumerate(content.splitlines(), 1):
        if not line.strip():
            continue
        if len(line.encode("utf-8")) > 64_000:
            raise ValueError(f"line {line_number} is too large")
        try:
            value = json.loads(line)
            case = EvalCase.model_validate(value)
        except (ValueError, TypeError) as exc:
            raise ValueError(f"line {line_number} is invalid") from exc
        if case.id in seen:
            raise ValueError(f"duplicate case id: {case.id}")
        seen.add(case.id)
        cases.append(case)
        if len(cases) > 1_000:
            raise ValueError("dataset contains too many cases")
    if not cases:
        raise ValueError("dataset must contain at least one case")
    return cases
