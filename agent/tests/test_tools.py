"""Tool Registry/业务 Tool 的 Schema、边界和独立内部身份测试。"""

import asyncio
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any
from uuid import uuid4

import jwt
import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from pydantic import BaseModel, ConfigDict, SecretStr

from app.clients.auth import AgentTokenSigner
from app.clients.business import BusinessClients
from app.core.settings import Settings
from app.graphs.runtime import FakeRuntime
from app.grpcgen.api.problem.v1 import problem_pb2 as _problem_pb2
from app.grpcgen.api.submission.v1 import submission_pb2 as _submission_pb2
from app.models.runtime import AgentState, Principal
from app.tools.business import build_business_tool_registry
from app.tools.registry import (
    ToolContext,
    ToolDefinition,
    ToolExecutor,
    ToolRegistry,
    ToolRegistryError,
    ToolSpec,
)

problem_pb2: Any = _problem_pb2
submission_pb2: Any = _submission_pb2


class Input(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)
    value: int


class Output(BaseModel):
    value: int


def definition(spec: ToolSpec | None = None) -> ToolDefinition:
    async def handler(arguments: BaseModel, principal: Principal) -> BaseModel:
        assert isinstance(arguments, Input)
        return Output(value=arguments.value + principal.user_id)

    return ToolDefinition(
        spec=spec or ToolSpec(name="test_tool", description="测试工具", read_scope="current_user"),
        input_model=Input,
        output_model=Output,
        handler=handler,
    )


def context(*, roles: frozenset[str] = frozenset(), allowed: bool = True) -> ToolContext:
    return ToolContext(
        principal=Principal(user_id=1, request_id="request", roles=roles),
        remaining_seconds=1,
        allowed_tools=frozenset({"test_tool"}) if allowed else frozenset(),
    )


async def test_registry_catalog_and_typed_execution() -> None:
    registry = ToolRegistry()
    registry.register(definition())
    with pytest.raises(ToolRegistryError):
        registry.register(definition())
    catalog = registry.catalog()
    assert catalog[0]["name"] == "test_tool"
    assert catalog[0]["input_schema"]["additionalProperties"] is False
    assert catalog[0]["output_schema"]["properties"]["value"]["type"] == "integer"
    result = await ToolExecutor(registry).execute("test_tool", {"value": 2}, context())
    assert result.ok and result.data == {"value": 3}


@pytest.mark.parametrize("arguments", [{}, {"value": "2"}, {"value": 2, "user_id": 999}])
async def test_invalid_arguments_do_not_execute(arguments: dict[str, object]) -> None:
    registry = ToolRegistry()
    registry.register(definition())
    result = await ToolExecutor(registry).execute("test_tool", arguments, context())
    assert not result.ok and result.error_code == "TOOL_INVALID_ARGUMENT"


async def test_allowlist_admin_and_disabled_policy() -> None:
    for spec, invocation in (
        (
            ToolSpec(name="test_tool", description="tool", read_scope="public"),
            context(allowed=False),
        ),
        (
            ToolSpec(
                name="test_tool",
                description="tool",
                read_scope="admin",
                allowed_roles=frozenset({"system_admin"}),
            ),
            context(),
        ),
        (
            ToolSpec(name="test_tool", description="tool", read_scope="public", enabled=False),
            context(),
        ),
    ):
        registry = ToolRegistry()
        registry.register(definition(spec))
        result = await ToolExecutor(registry).execute("test_tool", {"value": 2}, invocation)
        assert not result.ok and result.error_code == "TOOL_PERMISSION_DENIED"
    assert not ToolRegistry().names()


async def test_timeout_and_cancel_propagate_to_handler() -> None:
    started = asyncio.Event()
    closed = asyncio.Event()

    async def handler(arguments: BaseModel, principal: Principal) -> BaseModel:
        try:
            started.set()
            await asyncio.Event().wait()
            return Output(value=1)
        finally:
            closed.set()

    registry = ToolRegistry()
    registry.register(
        ToolDefinition(
            spec=ToolSpec(
                name="test_tool", description="tool", read_scope="public", timeout_seconds=0.05
            ),
            input_model=Input,
            output_model=Output,
            handler=handler,
        )
    )
    executor = ToolExecutor(registry)
    task = asyncio.create_task(executor.execute("test_tool", {"value": 1}, context()))
    await asyncio.wait_for(started.wait(), 1)
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task
    assert closed.is_set()
    result = await executor.execute("test_tool", {"value": 1}, context())
    assert result.error_code == "TOOL_TIMEOUT" and not result.ok


def test_write_tools_cannot_be_enabled_or_skip_confirmation() -> None:
    with pytest.raises(ValueError):
        ToolSpec(
            name="test_tool",
            description="tool",
            read_scope="admin",
            allowed_roles=frozenset({"system_admin"}),
            side_effect="write",
        )


class FakeProblemClient:
    async def get_problem(self, _principal: Principal, _problem_id: int) -> object:
        return problem_pb2.Problem(
            id=3,
            title="二分查找",
            slug="binary-search",
            description="有界题面",
            difficulty=2,
            time_limit_ms=1000,
            memory_limit_kb=65536,
        )

    async def list_problems(self, _principal: Principal, _page: int, _page_size: int) -> object:
        return problem_pb2.ListProblemsResponse(
            items=[problem_pb2.ProblemSummary(id=3, title="二分查找", slug="binary-search")],
            page={"page": 1, "page_size": 1, "total": 1},
        )


class FakeJudgeClient:
    async def get_submission(self, _principal: Principal, _submission_id: int) -> object:
        return submission_pb2.Submission(id=8, user_id=7, problem_id=3, language="go")

    async def get_submission_source(self, _principal: Principal, _submission_id: int) -> object:
        return submission_pb2.GetSubmissionSourceResponse(
            submission_id=8,
            language="go",
            source_code="package main\n" * 50,
            size_bytes=650,
            sha256="abc",
        )

    async def get_judge_result(self, _principal: Principal, _submission_id: int) -> object:
        return submission_pb2.JudgeResult(
            submission_id=8,
            status=submission_pb2.SUBMISSION_STATUS_DONE,
            verdict=submission_pb2.JUDGE_VERDICT_WA,
        )

    async def list_submissions(
        self, _principal: Principal, _page: int, _page_size: int, **_filters: object
    ) -> object:
        return submission_pb2.ListSubmissionsResponse(
            items=[submission_pb2.Submission(id=8, user_id=7, problem_id=3)],
            page={"page": 1, "page_size": 1, "total": 1},
        )


def test_registry_contains_only_read_tools() -> None:
    clients = BusinessClients(FakeProblemClient(), FakeJudgeClient())  # type: ignore[arg-type]
    registry = build_business_tool_registry(clients)
    assert registry.names() == (
        "get_judge_result",
        "get_problem",
        "get_submission",
        "get_submission_source",
        "list_problems",
        "list_submissions",
    )
    assert all(item["side_effect"] == "read" for item in registry.catalog())


@pytest.mark.asyncio
async def test_non_empty_problem_list_uses_summary_schema() -> None:
    clients = BusinessClients(FakeProblemClient(), FakeJudgeClient())  # type: ignore[arg-type]
    result = await ToolExecutor(build_business_tool_registry(clients)).execute(
        "list_problems",
        {"page": 1, "page_size": 20},
        ToolContext(
            principal=Principal(user_id=7, request_id="request-1"),
            remaining_seconds=5,
            allowed_tools=frozenset({"list_problems"}),
        ),
    )
    assert result.ok
    assert result.data["data"]["items"] == [
        {"id": 3, "title": "二分查找", "slug": "binary-search", "difficulty": 0, "status": 0}
    ]


@pytest.mark.asyncio
async def test_all_business_tools_execute_and_large_source_is_bounded() -> None:
    clients = BusinessClients(FakeProblemClient(), FakeJudgeClient())  # type: ignore[arg-type]
    maximum = 220
    executor = ToolExecutor(build_business_tool_registry(clients, maximum=maximum))
    principal = Principal(user_id=7, request_id="request-1")
    calls = (
        ("get_problem", {"problem_id": 3}),
        ("list_problems", {"page": 1, "page_size": 1}),
        ("get_submission", {"submission_id": 8}),
        ("get_submission_source", {"submission_id": 8}),
        ("get_judge_result", {"submission_id": 8}),
        ("list_submissions", {"page": 1, "page_size": 1}),
    )
    results = [
        await executor.execute(
            name,
            arguments,
            ToolContext(
                principal=principal,
                remaining_seconds=5,
                allowed_tools=frozenset({name}),
            ),
        )
        for name, arguments in calls
    ]
    assert all(result.ok for result in results)
    source = results[3]
    assert source.data["truncated"] is True
    assert source.data["data"] == {"submission_id": 8, "sha256": "abc"}
    assert len(source.model_dump_json().encode("utf-8")) <= maximum


@pytest.mark.asyncio
async def test_fake_runtime_selects_explicit_tool_without_prefetch() -> None:
    clients = BusinessClients(FakeProblemClient(), FakeJudgeClient())  # type: ignore[arg-type]
    state = AgentState(
        user_id=7,
        principal=Principal(user_id=7, request_id="request-1"),
        conversation_id=uuid4(),
        run_id=uuid4(),
        user_message='/tool get_problem {"problem_id":3}',
        started_at=datetime.now(UTC).replace(tzinfo=None),
        # MySQL DATETIME returns the UTC deadline without tzinfo.
        deadline_at=datetime.now(UTC).replace(tzinfo=None) + timedelta(seconds=5),
        allowed_tools=("get_problem",),
    )
    events = [
        event
        async for event in FakeRuntime(
            tool_executor=ToolExecutor(build_business_tool_registry(clients))
        ).run(state)
    ]
    assert events[1].type == "token"
    assert "二分查找" in events[1].data["text"]


@pytest.mark.asyncio
async def test_private_tool_uses_trusted_principal_and_returns_evidence() -> None:
    clients = BusinessClients(FakeProblemClient(), FakeJudgeClient())  # type: ignore[arg-type]
    executor = ToolExecutor(build_business_tool_registry(clients))
    result = await executor.execute(
        "get_submission",
        {"submission_id": 8},
        ToolContext(
            principal=Principal(user_id=7, request_id="request-1"),
            remaining_seconds=5,
            allowed_tools=frozenset({"get_submission"}),
        ),
    )
    assert result.ok
    assert result.data["source"] == "go-service"
    assert result.data["data"]["submission"]["id"] == 8


@pytest.mark.asyncio
async def test_tool_allowlist_and_arguments_are_enforced() -> None:
    clients = BusinessClients(FakeProblemClient(), FakeJudgeClient())  # type: ignore[arg-type]
    executor = ToolExecutor(build_business_tool_registry(clients))
    context = ToolContext(
        principal=Principal(user_id=7, request_id="request-1"),
        remaining_seconds=5,
        allowed_tools=frozenset(),
    )
    denied = await executor.execute("get_submission", {"submission_id": 8}, context)
    invalid = await executor.execute(
        "get_submission",
        {"submission_id": 0},
        context.model_copy(update={"allowed_tools": frozenset({"get_submission"})}),
    )
    assert denied.error_code == "TOOL_PERMISSION_DENIED"
    assert invalid.error_code == "TOOL_INVALID_ARGUMENT"


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "arguments",
    [
        {"page_size": "20"},
        {"page_size": 20, "unexpected": True},
        {"page_size": 101},
        {"page_size": 20, "problem_id": 2**63},
    ],
)
async def test_tool_inputs_are_strict_and_bounded(arguments: dict[str, object]) -> None:
    clients = BusinessClients(FakeProblemClient(), FakeJudgeClient())  # type: ignore[arg-type]
    result = await ToolExecutor(build_business_tool_registry(clients, max_page_size=50)).execute(
        "list_submissions",
        arguments,
        ToolContext(
            principal=Principal(user_id=7, request_id="request-1"),
            remaining_seconds=5,
            allowed_tools=frozenset({"list_submissions"}),
        ),
    )
    assert not result.ok and result.error_code == "TOOL_INVALID_ARGUMENT"


def test_agent_signer_binds_audience_and_full_method(tmp_path: Path) -> None:
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    private = tmp_path / "agent-private.pem"
    private.write_bytes(
        key.private_bytes(
            serialization.Encoding.PEM,
            serialization.PrivateFormat.PKCS8,
            serialization.NoEncryption(),
        )
    )
    settings = Settings(
        agent_private_key_file=private,
        database_url=SecretStr("mysql+asyncmy://agent:secret@db/oj_agent"),
    )
    token = AgentTokenSigner(settings).sign(
        Principal(user_id=7, roles=frozenset({"user"}), request_id="r1"),
        "judge-service",
        "/submission.v1.SubmissionService/GetSubmission",
    )
    claims = jwt.decode(token, key.public_key(), algorithms=["RS256"], audience="judge-service")
    assert claims["sub"] == "agent-service"
    assert claims["rpc"] == "/submission.v1.SubmissionService/GetSubmission"
    assert claims["actor_id"] == 7
