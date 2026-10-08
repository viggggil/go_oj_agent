"""Problem/Judge 只读 gRPC Client；业务授权由目标 Go 服务执行。"""

from dataclasses import dataclass
from typing import Any

from app.clients.auth import JUDGE_AUDIENCE, PROBLEM_AUDIENCE, AgentTokenSigner
from app.clients.grpc import AuthenticatedChannel
from app.core.settings import Settings
from app.grpcgen.api.common.v1 import common_pb2 as _common_pb2
from app.grpcgen.api.problem.v1 import problem_pb2 as _problem_pb2
from app.grpcgen.api.submission.v1 import submission_pb2 as _submission_pb2
from app.models.runtime import Principal

common_pb2: Any = _common_pb2
problem_pb2: Any = _problem_pb2
submission_pb2: Any = _submission_pb2


@dataclass(frozen=True)
class BusinessClients:
    problem: "ProblemClient"
    judge: "JudgeClient"

    async def close(self) -> None:
        await self.problem.close()
        await self.judge.close()


class ProblemClient:
    def __init__(self, settings: Settings, signer: AgentTokenSigner) -> None:
        self._channel = AuthenticatedChannel(
            settings.problem_service_endpoint,
            PROBLEM_AUDIENCE,
            signer,
            settings.tool_timeout_seconds,
        )

    async def close(self) -> None:
        await self._channel.close()

    async def get_problem(self, principal: Principal, problem_id: int) -> problem_pb2.Problem:
        response = await self._channel.unary(
            "/problem.v1.ProblemService/GetProblem",
            problem_pb2.GetProblemRequest(problem_id=problem_id),
            problem_pb2.GetProblemResponse,
            principal,
        )
        return response.problem

    async def list_problems(
        self, principal: Principal, page: int, page_size: int
    ) -> problem_pb2.ListProblemsResponse:
        return await self._channel.unary(
            "/problem.v1.ProblemService/ListProblems",
            problem_pb2.ListProblemsRequest(
                page=common_pb2.PageRequest(page=page, page_size=page_size)
            ),
            problem_pb2.ListProblemsResponse,
            principal,
        )


class JudgeClient:
    def __init__(self, settings: Settings, signer: AgentTokenSigner) -> None:
        self._channel = AuthenticatedChannel(
            settings.judge_service_endpoint,
            JUDGE_AUDIENCE,
            signer,
            settings.tool_timeout_seconds,
        )

    async def close(self) -> None:
        await self._channel.close()

    async def get_submission(
        self, principal: Principal, submission_id: int
    ) -> submission_pb2.Submission:
        response = await self._channel.unary(
            "/submission.v1.SubmissionService/GetSubmission",
            submission_pb2.GetSubmissionRequest(submission_id=submission_id),
            submission_pb2.GetSubmissionResponse,
            principal,
        )
        return response.submission

    async def get_submission_source(
        self, principal: Principal, submission_id: int
    ) -> submission_pb2.GetSubmissionSourceResponse:
        return await self._channel.unary(
            "/submission.v1.SubmissionService/GetSubmissionSource",
            submission_pb2.GetSubmissionSourceRequest(submission_id=submission_id),
            submission_pb2.GetSubmissionSourceResponse,
            principal,
        )

    async def get_judge_result(
        self, principal: Principal, submission_id: int
    ) -> submission_pb2.JudgeResult:
        response = await self._channel.unary(
            "/submission.v1.SubmissionService/GetJudgeResult",
            submission_pb2.GetJudgeResultRequest(submission_id=submission_id),
            submission_pb2.GetJudgeResultResponse,
            principal,
        )
        return response.result

    async def list_submissions(
        self,
        principal: Principal,
        page: int,
        page_size: int,
        *,
        problem_id: int = 0,
        language: str = "",
    ) -> submission_pb2.ListSubmissionsResponse:
        return await self._channel.unary(
            "/submission.v1.SubmissionService/ListSubmissions",
            submission_pb2.ListSubmissionsRequest(
                page=common_pb2.PageRequest(page=page, page_size=page_size),
                problem_id=problem_id,
                language=language,
            ),
            submission_pb2.ListSubmissionsResponse,
            principal,
        )


def build_business_clients(settings: Settings) -> BusinessClients:
    signer = AgentTokenSigner(settings)
    return BusinessClients(ProblemClient(settings, signer), JudgeClient(settings, signer))
