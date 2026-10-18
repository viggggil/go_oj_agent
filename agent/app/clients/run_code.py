"""与 judge-worker 的内部 Run Code API 通信；原始上游错误不会返回给模型。"""

from typing import Any

import httpx

from app.core.settings import Settings


class RunCodeUnavailable(RuntimeError):
    code = "TOOL_DEPENDENCY_UNAVAILABLE"


class RunCodeClient:
    def __init__(self, settings: Settings) -> None:
        self._client = httpx.AsyncClient(
            base_url=settings.run_code_url,
            headers={"Authorization": "Bearer " + settings.run_code_token.get_secret_value()},
            timeout=httpx.Timeout(65, connect=2),
            follow_redirects=False,
            trust_env=False,
        )

    async def close(self) -> None:
        await self._client.aclose()

    async def run(self, language: str, source: str, stdin: str) -> dict[str, Any]:
        try:
            response = await self._client.post(
                "", json={"language": language, "source": source, "stdin": stdin}
            )
            response.raise_for_status()
            value = response.json()
            if not isinstance(value, dict) or value.get("status") not in {
                "compile_error",
                "executed",
            }:
                raise RunCodeUnavailable
            return value
        except (httpx.HTTPError, ValueError, RunCodeUnavailable):
            raise RunCodeUnavailable from None
