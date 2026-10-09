"""认证加密的凭据 keyring；主密钥仅从部署文件读取。"""

import base64
import json
import os
import re
import stat
from pathlib import Path

from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from pydantic import SecretStr

from app.models.configuration import ConfigError


class CredentialCipher:
    def __init__(self, active: str, keys: dict[str, bytes]) -> None:
        if (
            active not in keys
            or not keys
            or any(
                not re.fullmatch(r"[a-zA-Z0-9_.-]{1,64}", version) or len(key) != 32
                for version, key in keys.items()
            )
        ):
            raise ConfigError("AGENT_CREDENTIAL_KEYRING_INVALID")
        self._active = active
        self._keys = keys

    @classmethod
    def from_file(
        cls, path: Path | None, *, allow_insecure_test_file: bool = False
    ) -> "CredentialCipher":
        try:
            if path is None or path.stat().st_size > 16_384:
                raise ValueError
            if not allow_insecure_test_file and stat.S_IMODE(path.stat().st_mode) & 0o077:
                raise ValueError
            content = json.loads(path.read_text())
            keys = {
                name: base64.b64decode(value, validate=True)
                for name, value in content["keys"].items()
            }
            return cls(content["active"], keys)
        except Exception:
            raise ConfigError("AGENT_CREDENTIAL_KEYRING_INVALID") from None

    def encrypt(self, identifier: str, secret: SecretStr) -> tuple[str, bytes, bytes]:
        nonce = os.urandom(12)
        ciphertext = AESGCM(self._keys[self._active]).encrypt(
            nonce, secret.get_secret_value().encode(), identifier.encode()
        )
        return self._active, nonce, ciphertext

    def decrypt(self, identifier: str, version: str, nonce: bytes, ciphertext: bytes) -> SecretStr:
        try:
            return SecretStr(
                AESGCM(self._keys[version]).decrypt(nonce, ciphertext, identifier.encode()).decode()
            )
        except Exception:
            raise ConfigError("AGENT_CREDENTIAL_UNAVAILABLE", 503) from None
