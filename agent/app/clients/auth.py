"""Agent 到 Go 服务的短期、按 RPC 绑定的内部身份。"""

import time
from pathlib import Path
from typing import Final
from uuid import uuid4

import jwt
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.rsa import RSAPrivateKey

from app.core.settings import ConfigurationError, Settings
from app.models.runtime import Principal


class InternalAuthError(RuntimeError):
    """Agent 内部身份不可用。"""


class AgentTokenSigner:
    """每次 gRPC 调用签发只绑定一个 FullMethod 的 token。"""

    def __init__(self, settings: Settings) -> None:
        path = settings.agent_private_key_file
        if path is None:
            raise ConfigurationError("Agent service identity is not configured")
        try:
            key = serialization.load_pem_private_key(Path(path).read_bytes(), password=None)
        except Exception:
            raise ConfigurationError("Invalid Agent service identity key") from None
        if not isinstance(key, RSAPrivateKey) or key.key_size < 2048:
            raise ConfigurationError("Invalid Agent service identity key")
        self._key = key
        self._kid = settings.agent_key_id
        self._issuer = settings.agent_issuer
        self._subject = settings.agent_subject
        self._ttl = settings.agent_token_ttl_seconds

    def sign(self, principal: Principal, audience: str, rpc: str) -> str:
        if not rpc.startswith("/") or "Service/" not in rpc:
            raise InternalAuthError("RPC method is invalid")
        now = int(time.time())
        claims = {
            "iss": self._issuer,
            "aud": audience,
            "sub": self._subject,
            "iat": now,
            "exp": now + self._ttl,
            "jti": uuid4().hex,
            "rpc": rpc,
            "actor_id": principal.user_id,
            "actor_roles": sorted(principal.roles),
            "request_id": principal.request_id,
        }
        return jwt.encode(claims, self._key, algorithm="RS256", headers={"kid": self._kid})


PROBLEM_AUDIENCE: Final[str] = "problem-service"
JUDGE_AUDIENCE: Final[str] = "judge-service"
