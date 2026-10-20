"""首批只读业务 Tool；结果只保留 Agent 需要的有界证据。"""

from datetime import UTC, datetime
from typing import Any, cast

from pydantic import BaseModel, ConfigDict, Field

from app.clients.business import BusinessClients
from app.models.runtime import Principal
from app.tools.registry import ReadScope, Sensitivity, ToolDefinition, ToolRegistry, ToolSpec

_MAX_INT64 = 2**63 - 1


class StrictToolInput(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)


class ProblemInput(StrictToolInput):
    problem_id: int = Field(gt=0, le=_MAX_INT64)


class SubmissionInput(StrictToolInput):
    submission_id: int = Field(gt=0, le=_MAX_INT64)


class ToolEnvelope(BaseModel):
    model_config = ConfigDict(extra="forbid")

    source: str
    fetched_at: str
    data: dict[str, Any]
    truncated: bool = False


def _fetched() -> str:
    return datetime.now(UTC).isoformat()


def _bounded(data: dict[str, Any], maximum: int) -> ToolEnvelope:
    source = "go-service"
    fetched_at = _fetched()

    def envelope_size(envelope: ToolEnvelope) -> int:
        return len(envelope.model_dump_json().encode("utf-8"))

    complete = ToolEnvelope(source=source, fetched_at=fetched_at, data=data)
    if envelope_size(complete) <= maximum:
        return complete
    # Keep identifiers and status evidence when a large statement/source/result is cut.
    reduced: dict[str, Any] = {}
    for key in ("id", "submission_id", "problem_id", "status", "verdict", "sha256"):
        if key in data:
            reduced[key] = data[key]
    for key, value in data.items():
        if not isinstance(value, dict):
            continue
        nested = {
            nested_key: value[nested_key]
            for nested_key in (
                "id",
                "submission_id",
                "problem_id",
                "status",
                "verdict",
                "judge_revision",
                "sha256",
            )
            if nested_key in value
        }
        if nested:
            reduced[key] = nested
    bounded = ToolEnvelope(source=source, fetched_at=fetched_at, data=reduced, truncated=True)
    if envelope_size(bounded) > maximum:
        # Settings reject budgets below 1024 bytes. Keep this guard for direct
        # registry callers so a too-small custom budget cannot silently exceed
        # its declared limit.
        raise ValueError("Tool result budget is too small for its evidence envelope")
    return bounded


def _problem(value: Any) -> dict[str, Any]:
    return {
        "id": value.id,
        "title": value.title,
        "slug": value.slug,
        "description": value.description,
        "difficulty": value.difficulty,
        "time_limit_ms": value.time_limit_ms,
        "memory_limit_kb": value.memory_limit_kb,
        "status": value.status,
        "tags": [{"id": item.id, "name": item.name} for item in value.tags],
    }


def _problem_summary(value: Any) -> dict[str, Any]:
    return {
        "id": value.id,
        "title": value.title,
        "slug": value.slug,
        "difficulty": value.difficulty,
        "status": value.status,
    }


def _submission(value: Any) -> dict[str, Any]:
    return {
        "id": value.id,
        "user_id": value.user_id,
        "problem_id": value.problem_id,
        "language": value.language,
        "status": value.status,
        "verdict": value.verdict,
        "time_ms": value.time_ms,
        "memory_kb": value.memory_kb,
        "judge_revision": value.judge_revision,
    }


def _judge(value: Any) -> dict[str, Any]:
    return {
        "submission_id": value.submission_id,
        "status": value.status,
        "verdict": value.verdict,
        "time_ms": value.time_ms,
        "memory_kb": value.memory_kb,
        "judge_revision": value.judge_revision,
        "system_error_reason": value.system_error_reason,
        "case_results": [
            {
                "case_no": item.case_no,
                "verdict": item.verdict,
                "time_ms": item.time_ms,
                "memory_kb": item.memory_kb,
                "message": item.message,
            }
            for item in value.case_results
        ],
    }


def build_business_tool_registry(
    clients: BusinessClients, maximum: int = 256_000, max_page_size: int = 100
) -> ToolRegistry:
    if not 1 <= max_page_size <= 100:
        raise ValueError("max_page_size must be between 1 and 100")

    class ProblemListInput(BaseModel):
        model_config = ConfigDict(extra="forbid", strict=True)
        page: int = Field(default=1, ge=1, le=10_000)
        page_size: int = Field(default=min(20, max_page_size), ge=1, le=max_page_size)

    class SubmissionListInput(BaseModel):
        model_config = ConfigDict(extra="forbid", strict=True)
        page: int = Field(default=1, ge=1, le=10_000)
        page_size: int = Field(default=min(20, max_page_size), ge=1, le=max_page_size)
        problem_id: int = Field(default=0, ge=0, le=_MAX_INT64)
        language: str = Field(default="", max_length=32)

    registry = ToolRegistry()

    async def get_problem(arguments: ProblemInput, principal: Principal) -> ToolEnvelope:
        return _bounded(
            {
                "problem": _problem(
                    await clients.problem.get_problem(principal, arguments.problem_id)
                )
            },
            maximum,
        )

    async def list_problems(arguments: ProblemListInput, principal: Principal) -> ToolEnvelope:
        response = await clients.problem.list_problems(
            principal, arguments.page, arguments.page_size
        )
        return _bounded(
            {
                "items": [_problem_summary(item) for item in response.items],
                "page": {
                    "page": response.page.page,
                    "page_size": response.page.page_size,
                    "total": response.page.total,
                },
            },
            maximum,
        )

    async def get_submission(arguments: SubmissionInput, principal: Principal) -> ToolEnvelope:
        value = await clients.judge.get_submission(principal, arguments.submission_id)
        return _bounded({"submission": _submission(value)}, maximum)

    async def get_source(arguments: SubmissionInput, principal: Principal) -> ToolEnvelope:
        value = await clients.judge.get_submission_source(principal, arguments.submission_id)
        return _bounded(
            {
                "submission_id": value.submission_id,
                "language": value.language,
                "source_code": value.source_code,
                "size_bytes": value.size_bytes,
                "sha256": value.sha256,
            },
            maximum,
        )

    async def get_judge_result(arguments: SubmissionInput, principal: Principal) -> ToolEnvelope:
        return _bounded(
            {
                "result": _judge(
                    await clients.judge.get_judge_result(principal, arguments.submission_id)
                )
            },
            maximum,
        )

    async def list_submissions(
        arguments: SubmissionListInput, principal: Principal
    ) -> ToolEnvelope:
        response = await clients.judge.list_submissions(
            principal,
            arguments.page,
            arguments.page_size,
            problem_id=arguments.problem_id,
            language=arguments.language,
        )
        return _bounded(
            {
                "items": [_submission(item) for item in response.items],
                "page": {
                    "page": response.page.page,
                    "page_size": response.page.page_size,
                    "total": response.page.total,
                },
            },
            maximum,
        )

    definitions = [
        ("get_problem", "读取题目详情", ProblemInput, get_problem, "public", "public"),
        ("list_problems", "分页读取题目", ProblemListInput, list_problems, "public", "public"),
        (
            "get_submission",
            "读取当前用户可见的提交",
            SubmissionInput,
            get_submission,
            "current_user",
            "private",
        ),
        (
            "get_submission_source",
            "读取当前用户可见的提交源码",
            SubmissionInput,
            get_source,
            "current_user",
            "source",
        ),
        (
            "get_judge_result",
            "读取当前用户可见的判题结果",
            SubmissionInput,
            get_judge_result,
            "current_user",
            "private",
        ),
        (
            "list_submissions",
            "分页读取当前用户可见的提交",
            SubmissionListInput,
            list_submissions,
            "current_user",
            "private",
        ),
    ]
    if clients.run_code is not None:

        class RunCodeInput(BaseModel):
            model_config = ConfigDict(extra="forbid", strict=True)
            language: str = Field(pattern=r"^(c|cpp|go|java|python)$")
            source: str = Field(min_length=1, max_length=65_536)
            stdin: str = Field(default="", max_length=16_384)

        class RunCodeOutput(BaseModel):
            model_config = ConfigDict(extra="forbid")
            status: str
            verdict: str = ""
            stdout: str = Field(default="", max_length=32_768)
            stderr: str = Field(default="", max_length=32_768)
            time_ms: int = Field(default=0, ge=0)
            memory_bytes: int = Field(default=0, ge=0)

        async def run_code(arguments: RunCodeInput, principal: Principal) -> RunCodeOutput:
            del principal
            assert clients.run_code is not None
            value = await clients.run_code.run(
                arguments.language, arguments.source, arguments.stdin
            )
            return RunCodeOutput.model_validate(value)

        registry.register(
            ToolDefinition(
                spec=ToolSpec(
                    name="run_code",
                    description="在无网络、限时限内存的独立沙箱中编译并运行一段代码。输入仅用于验证示例，不会提交到题目。",
                    read_scope="public",
                    sensitivity="internal",
                    timeout_seconds=60,
                ),
                input_model=RunCodeInput,
                output_model=RunCodeOutput,
                handler=cast(Any, run_code),
            )
        )
    for name, description, input_model, handler, scope, sensitivity in definitions:
        registry.register(
            ToolDefinition(
                spec=ToolSpec(
                    name=name,
                    description=description,
                    read_scope=cast(ReadScope, scope),
                    sensitivity=cast(Sensitivity, sensitivity),
                ),
                input_model=cast(Any, input_model),
                output_model=ToolEnvelope,
                handler=cast(Any, handler),
            )
        )
    return registry
