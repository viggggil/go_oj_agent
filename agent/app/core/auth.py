"""验证 Gateway 的短期 HTTP 委托，不信任身份 Header 或请求体。"""

from typing import Any

import jwt
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.rsa import RSAPublicKey

from app.core.operations import operation
from app.core.settings import ConfigurationError, Settings
from app.models.runtime import Principal

CHAT_OPERATION = "HTTP POST /api/v1/agent/chat"


class InvalidDelegation(ValueError):
    """可公开的身份验证失败，不携带 token 或解析细节。"""


class DelegationVerifier:
    def __init__(self, settings: Settings) -> None:
        try:
            if settings.gateway_public_key_file is None:
                raise ValueError
            key = serialization.load_pem_public_key(settings.gateway_public_key_file.read_bytes())
            if not isinstance(key, RSAPublicKey) or key.key_size < 2048:
                raise ValueError
        except Exception:
            raise ConfigurationError("Invalid Gateway delegation public key") from None
        self._key = key
        self._kid = settings.gateway_key_id

    def verify(
        self, authorization: str | None, expected_operation: str = CHAT_OPERATION
    ) -> Principal:
        try:
            _, method, path = expected_operation.split(" ", 2)
            if operation(method, path) != expected_operation:
                raise ValueError
            if authorization is None or len(authorization) > 16_384:
                raise ValueError
            scheme, token = authorization.split(" ", 1)
            if scheme.lower() != "bearer" or not token or token != token.strip():
                raise ValueError
            header = jwt.get_unverified_header(token)
            if (
                header.get("alg") != "RS256"
                or header.get("typ") != "JWT"
                or header.get("kid") != self._kid
            ):
                raise ValueError
            claims: dict[str, Any] = jwt.decode(
                token,
                self._key,
                algorithms=["RS256"],
                issuer="go-oj-gateway",
                audience="agent-service",
                options={
                    "require": [
                        "iss",
                        "aud",
                        "sub",
                        "iat",
                        "exp",
                        "jti",
                        "rpc",
                        "actor_id",
                        "request_id",
                    ]
                },
            )
            if (
                claims["sub"] != "gateway-service"
                or claims["aud"] != "agent-service"
                or claims["rpc"] != expected_operation
            ):
                raise ValueError
            actor, issued, expires = claims["actor_id"], claims["iat"], claims["exp"]
            if any(type(value) is not int for value in (actor, issued, expires)):
                raise ValueError
            if not 0 < actor <= 2**63 - 1 or not 0 < expires - issued <= 60:
                raise ValueError
            if not isinstance(claims["jti"], str) or not 1 <= len(claims["jti"]) <= 128:
                raise ValueError
            roles = claims.get("actor_roles", [])
            if (
                not isinstance(roles, list)
                or len(roles) > 32
                or any(not isinstance(role, str) or not 1 <= len(role) <= 64 for role in roles)
            ):
                raise ValueError
            if not isinstance(claims["request_id"], str):
                raise ValueError
            # JSON 转义也可能携带孤立 surrogate，拒绝不能编码为 UTF-8 的身份文本。
            claims["request_id"].encode("utf-8")
            for role in roles:
                role.encode("utf-8")
            return Principal(user_id=actor, roles=frozenset(roles), request_id=claims["request_id"])
        except Exception:
            raise InvalidDelegation("Invalid Gateway delegation") from None
