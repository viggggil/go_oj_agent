"""评测数据集的严格输入模型；样例内容视为不可信数据。"""

from typing import Any, Literal
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator


class EvalDatasetInvalid(ValueError):
    """带有界定位信息的数据集校验错误。"""

    def __init__(self, message: str, *, line: int | None = None) -> None:
        super().__init__(message)
        self.line = line


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


class EvalRunCreate(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)

    dataset_id: UUID
    agent_key: str = Field(min_length=2, max_length=64, pattern=r"^[a-z][a-z0-9_]{1,63}$")
    repeat_count: int = Field(default=1, ge=1, le=3, strict=True)
    concurrency: int = Field(default=1, ge=1, le=8, strict=True)


class EvalRunPage(EvalPage):
    status: Literal["", "QUEUED", "RUNNING", "COMPLETED", "FAILED", "CANCELLED"] = ""


class EvalResultPage(EvalPage):
    verdict: Literal["", "pass", "fail", "hard_fail", "unreviewed"] = ""
    category: str = Field(default="", max_length=64)
    error_code: str = Field(default="", max_length=64)
    status: Literal["", "COMPLETED", "FAILED", "CANCELLED"] = ""
    page_size: int = Field(default=20, ge=1, le=100)


class EvalScores(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)

    task_completion: int = Field(ge=0, le=4)
    technical_correctness: int = Field(ge=0, le=4)
    evidence_support: int = Field(ge=0, le=4)
    instruction_following: int = Field(ge=0, le=4)
    clarity: int = Field(ge=0, le=4)


class EvalReviewWrite(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)

    expected_version: int = Field(ge=0)
    scores: EvalScores
    passed: bool = Field(alias="pass")
    tags: list[str] = Field(default_factory=list, max_length=10)
    note: str = Field(default="", max_length=2000)
    reason: str = Field(min_length=1, max_length=512)

    @field_validator("reason")
    @classmethod
    def meaningful_reason(cls, value: str) -> str:
        if not value.strip():
            raise ValueError("Review reason cannot be blank")
        return value

    @field_validator("tags")
    @classmethod
    def valid_tags(cls, values: list[str]) -> list[str]:
        if any(not value.strip() or len(value) > 32 for value in values):
            raise ValueError("Invalid review tag")
        if len(set(values)) != len(values):
            raise ValueError("Duplicate review tags")
        return values


class EvalReviewPage(EvalPage):
    page_size: int = Field(default=10, ge=1, le=20)


class EvalModelReview(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)

    case_id: str = Field(min_length=1, max_length=128)
    passed: bool = Field(alias="pass")
    scores: EvalScores
    hard_fail: bool
    violations: list[str] = Field(max_length=32)
    missing_facts: list[str] = Field(max_length=64)
    reason: str = Field(min_length=1, max_length=1024)

    @field_validator("violations", "missing_facts")
    @classmethod
    def bounded_review_items(cls, values: list[str]) -> list[str]:
        if any(not item.strip() or len(item) > 256 for item in values):
            raise ValueError("Invalid review item")
        return values

    @field_validator("reason")
    @classmethod
    def meaningful_review_reason(cls, value: str) -> str:
        if not value.strip():
            raise ValueError("Review reason cannot be blank")
        return value

    @model_validator(mode="after")
    def hard_failure_cannot_pass(self) -> "EvalModelReview":
        if self.hard_fail and self.passed:
            raise ValueError("A hard failure cannot pass")
        return self


class EvalModelReviewRetry(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)

    expected_attempt: int = Field(ge=0, strict=True)


class EvalModelReviewPage(EvalPage):
    page_size: int = Field(default=10, ge=1, le=20)


class EvalRunCancel(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)

    expected_version: int = Field(ge=1)


def parse_jsonl(content: str) -> list[EvalCase]:
    import json

    cases: list[EvalCase] = []
    seen: set[str] = set()
    # Agent 数据库默认使用大小写不敏感排序；上传前按同一规则拒绝冲突 ID。
    seen_folded: set[str] = set()
    for line_number, line in enumerate(content.splitlines(), 1):
        if not line.strip():
            continue
        if len(line.encode("utf-8")) > 64_000:
            raise EvalDatasetInvalid(f"line {line_number} is too large", line=line_number)
        try:
            value = json.loads(
                line, parse_constant=lambda value: (_ for _ in ()).throw(ValueError(value))
            )
            case = EvalCase.model_validate(value)
        except (ValueError, TypeError) as exc:
            raise EvalDatasetInvalid(f"line {line_number} is invalid", line=line_number) from exc
        if case.id in seen:
            raise EvalDatasetInvalid(f"duplicate case id: {case.id}", line=line_number)
        folded = case.id.casefold()
        if folded in seen_folded:
            raise EvalDatasetInvalid(
                f"case id conflicts ignoring case: {case.id}", line=line_number
            )
        seen.add(case.id)
        seen_folded.add(folded)
        cases.append(case)
        if len(cases) > 1_000:
            raise EvalDatasetInvalid("dataset contains too many cases")
    if not cases:
        raise EvalDatasetInvalid("dataset must contain at least one case")
    return cases
