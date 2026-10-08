"""验证 Python gRPC Client 的 FullMethod、metadata 和取消边界。"""

from pathlib import Path
from typing import Any

import grpc
import jwt
import pytest
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from pydantic import SecretStr

from app.clients.auth import AgentTokenSigner
from app.clients.business import ProblemClient
from app.core.settings import Settings
from app.grpcgen.api.problem.v1 import problem_pb2 as _problem_pb2
from app.grpcgen.api.problem.v1 import problem_pb2_grpc as _problem_pb2_grpc
from app.models.runtime import Principal

problem_pb2: Any = _problem_pb2
problem_pb2_grpc: Any = _problem_pb2_grpc


class ProblemStub(problem_pb2_grpc.ProblemServiceServicer):  # type: ignore[misc]
    def __init__(self, public_key: Any) -> None:
        self.public_key = public_key
        self.calls: list[int] = []

    async def GetProblem(self, request: Any, context: Any) -> Any:
        self.calls.append(request.problem_id)
        metadata = dict(context.invocation_metadata())
        token = metadata["authorization"].removeprefix("Bearer ")
        claims = jwt.decode(
            token,
            self.public_key,
            algorithms=["RS256"],
            audience="problem-service",
        )
        assert claims["rpc"] == "/problem.v1.ProblemService/GetProblem"
        assert claims["actor_id"] == 7
        return problem_pb2.GetProblemResponse(
            problem=problem_pb2.Problem(id=request.problem_id, title="题目")
        )


@pytest.mark.asyncio
async def test_problem_client_sends_bound_internal_identity(tmp_path: Path) -> None:
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
        tool_timeout_seconds=2,
    )
    server = grpc.aio.server()
    servicer = ProblemStub(key.public_key())
    problem_pb2_grpc.add_ProblemServiceServicer_to_server(servicer, server)
    port = server.add_insecure_port("127.0.0.1:0")
    settings.problem_service_endpoint = f"127.0.0.1:{port}"
    await server.start()
    client = ProblemClient(settings, AgentTokenSigner(settings))
    try:
        response = await client.get_problem(Principal(user_id=7, request_id="req-1"), 42)
        assert response.id == 42
        assert servicer.calls == [42]
    finally:
        await client.close()
        await server.stop(grace=0)
