"""隔离协议 fixture：真实 gRPC/Protobuf 与 Agent JWT，业务授权另由 Go 集成测试覆盖。"""

import asyncio
from pathlib import Path

import grpc
import jwt

from app.grpcgen.api.problem.v1 import problem_pb2 as pb

METHOD = '/problem.v1.ProblemService/GetProblem'


async def get_problem(request, context):
    metadata = dict(context.invocation_metadata())
    try:
        bearer = metadata.get('authorization', '')
        assert bearer.startswith('Bearer ')
        token = bearer[7:]
        assert jwt.get_unverified_header(token)['kid'] == 'agent-internal-2026-10'
        claims = jwt.decode(token, Path('/run/keys/agent-public.pem').read_bytes(),
                            algorithms=['RS256'], audience='problem-service', issuer='go-oj-agent',
                            options={'require': ['exp', 'iat', 'jti', 'sub', 'rpc', 'actor_id', 'actor_roles', 'request_id']})
        assert claims['sub'] == 'agent-service' and claims['rpc'] == METHOD
        assert claims['actor_id'] in {7206, 7301}
        assert claims['actor_roles'] == ['admin'] and claims['request_id']
    except (AssertionError, jwt.PyJWTError, KeyError):
        await context.abort(grpc.StatusCode.UNAUTHENTICATED, 'invalid delegation')
    if request.problem_id != 41:
        await context.abort(grpc.StatusCode.PERMISSION_DENIED, 'private business detail')
    return pb.GetProblemResponse(problem=pb.Problem(
        id=41, title='证据数字 739216', description='只读协议证据', status=pb.PROBLEM_STATUS_NORMAL))


async def main():
    server = grpc.aio.server()
    server.add_generic_rpc_handlers((grpc.method_handlers_generic_handler('problem.v1.ProblemService', {
        'GetProblem': grpc.unary_unary_rpc_method_handler(get_problem,
            request_deserializer=pb.GetProblemRequest.FromString,
            response_serializer=lambda value: value.SerializeToString()),
    }),))
    server.add_insecure_port('[::]:9002')
    await server.start()
    await server.wait_for_termination()


asyncio.run(main())
