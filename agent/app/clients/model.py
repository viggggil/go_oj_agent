"""有界 Responses SSE：只消费回答文本，禁止透传推理、工具和错误正文。"""

import asyncio
import json
import re
from collections.abc import AsyncGenerator, AsyncIterator
from time import monotonic
from typing import Any, cast

import httpx
from pydantic import SecretStr

from app.core.provider_policy import ProviderPolicy
from app.models.configuration import ConfigError, ModelProfileConfig, ProviderConfig
from app.models.provider import ModelAttempt, ModelFailure

MAX_FRAME_BYTES = 262_144


async def sse_data(response: httpx.Response) -> AsyncGenerator[tuple[str | None, dict[str, Any]]]:
    buffer = bytearray()
    data: list[bytes] = []
    event: str | None = None
    frame_size = 0
    async for chunk in response.aiter_bytes():
        buffer.extend(chunk)
        while b"\n" in buffer:
            raw, _, rest = buffer.partition(b"\n")
            buffer = bytearray(rest)
            frame_size += len(raw) + 1
            if frame_size > MAX_FRAME_BYTES:
                raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE")
            line = raw.rstrip(b"\r")
            if not line:
                if data:
                    try:
                        payload = json.loads(b"\n".join(data).decode("utf-8"))
                        if not isinstance(payload, dict):
                            raise ValueError
                    except (ValueError, UnicodeError):
                        raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE") from None
                    yield event, payload
                data = []
                event = None
                frame_size = 0
            elif line.startswith(b"data:"):
                data.append(bytes(line[5:].lstrip(b" ")))
            elif line.startswith(b"event:"):
                try:
                    event = line[6:].strip().decode("ascii")
                except UnicodeError:
                    raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE") from None
        if len(buffer) + frame_size > MAX_FRAME_BYTES:
            raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE")
    # 未终止帧不允许当成完整事件。
    if buffer or data:
        raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE")


def _http_failure(status: int) -> ModelFailure:
    if status in {401, 403}:
        return ModelFailure("AGENT_MODEL_AUTH_FAILED")
    if status == 402:
        return ModelFailure("AGENT_MODEL_QUOTA")
    if status == 429:
        return ModelFailure("AGENT_MODEL_RATE_LIMIT", retryable=True)
    if status in {500, 502, 503, 504}:
        return ModelFailure("AGENT_MODEL_UNAVAILABLE", retryable=True)
    return ModelFailure("AGENT_MODEL_REJECTED")


class ResponsesClient:
    def __init__(self, policy: ProviderPolicy, *, client: httpx.AsyncClient | None = None) -> None:
        self._policy = policy
        self._client = client or httpx.AsyncClient(
            trust_env=False,
            follow_redirects=False,
            limits=httpx.Limits(max_connections=128, max_keepalive_connections=16),
        )

    async def close(self) -> None:
        await self._client.aclose()

    async def stream(
        self,
        provider: ProviderConfig,
        profile: ModelProfileConfig,
        secret: SecretStr,
        instructions: str,
        message: str,
        max_output: int,
        attempt: ModelAttempt,
        *,
        max_output_chars: int,
        max_events: int,
    ) -> AsyncIterator[str]:
        try:
            self._policy.validate(provider)
        except ConfigError:
            raise ModelFailure("AGENT_MODEL_ENDPOINT_DENIED") from None
        body: dict[str, Any] = {
            "model": profile.model,
            "instructions": instructions,
            "input": [{"role": "user", "content": message}],
            "stream": True,
            "store": False,
            "max_output_tokens": max_output,
        }
        if profile.temperature is not None:
            body["temperature"] = profile.temperature
        if profile.verbosity is not None:
            body["text"] = {"verbosity": profile.verbosity}
        started = monotonic()
        answer = ""
        completed = False
        event_count = 0
        output_index: int | None = None
        try:
            async with asyncio.timeout(provider.request_timeout_seconds):
                timeout = httpx.Timeout(
                    provider.idle_timeout_seconds, connect=provider.connect_timeout_seconds
                )
                async with self._client.stream(
                    "POST",
                    provider.base_url.rstrip("/") + "/responses",
                    json=body,
                    headers={
                        "Authorization": "Bearer " + secret.get_secret_value(),
                        "Accept": "text/event-stream",
                    },
                    timeout=timeout,
                ) as response:
                    if response.status_code != 200:
                        raise _http_failure(response.status_code)
                    if (
                        response.headers.get("content-type", "").split(";")[0].strip()
                        != "text/event-stream"
                    ):
                        raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE")
                    iterator = sse_data(response)
                    try:
                        while True:
                            first_remaining = provider.first_token_timeout_seconds - (
                                monotonic() - started
                            )
                            wait = (
                                provider.idle_timeout_seconds
                                if answer
                                else min(provider.idle_timeout_seconds, max(0, first_remaining))
                            )
                            try:
                                name, event = await asyncio.wait_for(anext(iterator), timeout=wait)
                            except StopAsyncIteration:
                                break
                            event_count += 1
                            if event_count > max_events:
                                raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE")
                            kind = event.get("type")
                            if not isinstance(kind, str) or (name is not None and name != kind):
                                raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE")
                            if kind == "response.output_text.delta":
                                text = event.get("delta")
                                if not isinstance(text, str) or completed:
                                    raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE")
                                index = event.get("output_index", 0)
                                if not isinstance(index, int) or isinstance(index, bool):
                                    raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE")
                                if output_index is not None and output_index != index:
                                    raise ModelFailure("AGENT_MODEL_UNSUPPORTED_OUTPUT")
                                output_index = index
                                answer += text
                                if len(answer) > max_output_chars:
                                    raise ModelFailure("AGENT_MODEL_OUTPUT_LIMIT")
                                # 明确标记为保守估算，不把字节数作为 Provider usage。
                                attempt.output_tokens = len(answer.encode())
                                if text:
                                    if attempt.first_token_ms is None:
                                        attempt.first_token_ms = int((monotonic() - started) * 1000)
                                    yield text
                            elif kind == "response.completed":
                                self._complete(event, attempt, answer)
                                completed = True
                                return
                            elif kind in {"response.failed", "response.incomplete", "error"}:
                                code = "AGENT_MODEL_INVALID_RESPONSE"
                                if kind == "response.incomplete":
                                    details = (
                                        event.get("response", {}).get("incomplete_details") or {}
                                    )
                                    code = (
                                        "AGENT_MODEL_OUTPUT_LIMIT"
                                        if details.get("reason") == "max_output_tokens"
                                        else "AGENT_MODEL_BLOCKED"
                                    )
                                raise ModelFailure(code)
                            elif kind.startswith("response.refusal"):
                                raise ModelFailure("AGENT_MODEL_BLOCKED")
                            elif kind == "response.output_item.added":
                                item = event.get("item")
                                if not isinstance(item, dict) or item.get("type") not in {
                                    "message",
                                    "reasoning",
                                }:
                                    raise ModelFailure("AGENT_MODEL_UNSUPPORTED_OUTPUT")
                            elif "function_call" in kind or "tool" in kind:
                                raise ModelFailure("AGENT_MODEL_UNSUPPORTED_OUTPUT")
                    finally:
                        await iterator.aclose()
            if not completed:
                raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE")
        except (TimeoutError, httpx.TimeoutException):
            raise ModelFailure("AGENT_MODEL_TIMEOUT") from None
        except httpx.HTTPError:
            # 网络错误可能已被计费；不自动重试不确定的连接结果。
            raise ModelFailure("AGENT_MODEL_UNAVAILABLE") from None

    @staticmethod
    def _complete(event: dict[str, Any], attempt: ModelAttempt, answer: str) -> None:
        response = event.get("response")
        if (
            not isinstance(response, dict)
            or response.get("status") != "completed"
            or not answer.strip()
        ):
            raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE")
        output = response.get("output")
        if not isinstance(output, list):
            raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE")
        texts: list[str] = []
        for item in output:
            if not isinstance(item, dict):
                raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE")
            if item.get("type") == "reasoning":
                continue
            if item.get("type") != "message" or item.get("role") != "assistant":
                raise ModelFailure("AGENT_MODEL_UNSUPPORTED_OUTPUT")
            content = item.get("content")
            if not isinstance(content, list):
                raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE")
            for part in content:
                if (
                    not isinstance(part, dict)
                    or part.get("type") != "output_text"
                    or not isinstance(part.get("text"), str)
                ):
                    raise ModelFailure("AGENT_MODEL_UNSUPPORTED_OUTPUT")
                texts.append(part["text"])
        if "".join(texts) != answer:
            raise ModelFailure("AGENT_MODEL_INVALID_RESPONSE")
        returned_model = response.get("model")
        if isinstance(returned_model, str) and re.fullmatch(
            r"[a-zA-Z0-9_./:@+-]{1,200}", returned_model
        ):
            attempt.returned_model = returned_model
        usage = response.get("usage")
        if isinstance(usage, dict):
            values = [usage.get("input_tokens"), usage.get("output_tokens")]
            if all(
                isinstance(value, int) and not isinstance(value, bool) and 0 <= value <= 10_000_000
                for value in values
            ):
                attempt.input_tokens = cast(int, values[0])
                attempt.output_tokens = cast(int, values[1])
                attempt.usage_source = "provider"
        attempt.finish_reason = "completed"
