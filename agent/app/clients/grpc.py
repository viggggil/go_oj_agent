"""带内部 JWT、deadline 和取消传播的异步 gRPC Client。"""

from typing import Any

import grpc

from app.clients.auth import AgentTokenSigner
from app.models.runtime import Principal


class GrpcDependencyError(RuntimeError):
    """对 Tool 暴露的稳定依赖错误。"""

    def __init__(self, code: str) -> None:
        super().__init__(code)
        self.code = code


def map_grpc_error(error: grpc.aio.AioRpcError) -> GrpcDependencyError:
    status = error.code()
    if status in {grpc.StatusCode.UNAUTHENTICATED, grpc.StatusCode.PERMISSION_DENIED}:
        return GrpcDependencyError("TOOL_PERMISSION_DENIED")
    if status == grpc.StatusCode.NOT_FOUND:
        return GrpcDependencyError("TOOL_NOT_FOUND")
    if status in {grpc.StatusCode.DEADLINE_EXCEEDED, grpc.StatusCode.CANCELLED}:
        return GrpcDependencyError("TOOL_TIMEOUT")
    if status == grpc.StatusCode.INVALID_ARGUMENT:
        return GrpcDependencyError("TOOL_INVALID_ARGUMENT")
    return GrpcDependencyError("TOOL_DEPENDENCY_UNAVAILABLE")


class AuthenticatedChannel:
    def __init__(
        self, endpoint: str, audience: str, signer: AgentTokenSigner, timeout: float
    ) -> None:
        self._channel = grpc.aio.insecure_channel(endpoint)
        self._audience = audience
        self._signer = signer
        self._timeout = timeout

    async def close(self) -> None:
        await self._channel.close()

    async def unary(
        self,
        method: str,
        request: Any,
        response: type[Any],
        principal: Principal,
        *,
        deadline_seconds: float | None = None,
    ) -> Any:
        token = self._signer.sign(principal, self._audience, method)
        call = self._channel.unary_unary(
            method,
            request_serializer=lambda value: value.SerializeToString(),
            response_deserializer=response.FromString,
        )
        try:
            return await call(
                request,
                timeout=min(self._timeout, deadline_seconds or self._timeout),
                metadata=(("authorization", f"Bearer {token}"),),
            )
        except grpc.aio.AioRpcError as error:
            raise map_grpc_error(error) from None
