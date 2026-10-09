"""部署侧精确 origin allowlist；请求配置不能扩大网络范围。"""

import ipaddress
from urllib.parse import urlsplit

from app.core.settings import Settings
from app.models.configuration import ConfigError, ProviderConfig


def origin(value: str, *, base_url: bool = False) -> str:
    try:
        parts = urlsplit(value)
        if (
            parts.scheme not in {"https", "http"}
            or not parts.hostname
            or parts.username is not None
            or parts.password is not None
            or parts.query
            or parts.fragment
            or "\\" in value
            or any(ord(char) <= 32 for char in value)
            or (not base_url and parts.path not in {"", "/"})
            or (base_url and (".." in parts.path or "%" in parts.path))
        ):
            raise ValueError
        host = parts.hostname.lower()
        port = parts.port or (443 if parts.scheme == "https" else 80)
        if not 1 <= port <= 65535:
            raise ValueError
        host = f"[{host}]" if ":" in host else host
        return f"{parts.scheme}://{host}:{port}"
    except ValueError:
        raise ConfigError("AGENT_MODEL_ENDPOINT_DENIED") from None


class ProviderPolicy:
    def __init__(self, settings: Settings) -> None:
        self._origins = {origin(value) for value in settings.provider_allowed_origins}
        self._private = settings.provider_allow_private_network

    def validate(self, provider: ProviderConfig) -> None:
        value = origin(provider.base_url, base_url=True)
        parts = urlsplit(provider.base_url)
        if value not in self._origins or (parts.scheme != "https" and not self._private):
            raise ConfigError("AGENT_MODEL_ENDPOINT_DENIED")
        host = parts.hostname or ""
        if not self._private:
            if host == "localhost" or host.endswith((".localhost", ".local")):
                raise ConfigError("AGENT_MODEL_ENDPOINT_DENIED")
            try:
                address = ipaddress.ip_address(host)
            except ValueError:
                return
            if not address.is_global:
                raise ConfigError("AGENT_MODEL_ENDPOINT_DENIED")
