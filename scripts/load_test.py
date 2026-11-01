#!/usr/bin/env python3
"""Black-box load tests for Gateway, Judge and Agent SSE.

The runner intentionally uses only the Python standard library so it can be
run from a checkout without installing a second load-testing stack.  It
creates isolated users for each run, records raw samples as JSON, and prints
the summary needed to compare the three workloads.

Examples:
  python3 scripts/load_test.py api --concurrencies 10,30,50 --duration 120
  python3 scripts/load_test.py judge --problem-id 1 --rates 0.5,1,2 --duration 120
  python3 scripts/load_test.py agent --concurrencies 1,3,5 --duration 120
"""

from __future__ import annotations

import argparse
import base64
import concurrent.futures
import dataclasses
import datetime as dt
import json
import os
import statistics
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from collections import Counter
from pathlib import Path
from typing import Any, Iterable


TERMINAL_SUBMISSION_STATUSES = {
    "SUBMISSION_STATUS_DONE",
    "SUBMISSION_STATUS_CANCELLED",
    "SUBMISSION_STATUS_INVALIDATED",
}
TERMINAL_SUBMISSION_STATUS_CODES = {4, 6, 7}


def now_iso() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat()


def percentile(values: list[float], ratio: float) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    index = min(len(ordered) - 1, max(0, int(round((len(ordered) - 1) * ratio))))
    return ordered[index]


@dataclasses.dataclass
class Sample:
    latency: float
    status: int | None
    error: str | None = None
    ttft: float | None = None
    tokens: int = 0
    completed: bool = False
    submission_id: int | None = None
    completion_latency: float | None = None


class Samples:
    def __init__(self) -> None:
        self._items: list[Sample] = []
        self._lock = threading.Lock()

    def add(self, item: Sample) -> None:
        with self._lock:
            self._items.append(item)

    def values(self) -> list[Sample]:
        with self._lock:
            return list(self._items)

    def summary(self, duration: float | None = None) -> dict[str, Any]:
        items = self.values()
        latencies = [item.latency for item in items]
        successes = [
            item for item in items if item.status is not None and 200 <= item.status < 400 and not item.error
        ]
        errors = [
            item for item in items if item.status is None or not 200 <= item.status < 400 or item.error
        ]
        ttfts = [item.ttft for item in items if item.ttft is not None]
        completions = [item.completion_latency for item in items if item.completion_latency is not None]
        return {
            "requests": len(items),
            "successes": len(successes),
            "errors": len(errors),
            "error_rate": (len(errors) / len(items)) if items else 0.0,
            "throughput_rps": (len(items) / duration) if duration and duration > 0 else None,
            "latency_ms": {
                "p50": _ms(percentile(latencies, 0.50)),
                "p95": _ms(percentile(latencies, 0.95)),
                "p99": _ms(percentile(latencies, 0.99)),
                "max": _ms(max(latencies) if latencies else None),
            },
            "ttft_ms": {
                "p50": _ms(percentile(ttfts, 0.50)),
                "p95": _ms(percentile(ttfts, 0.95)),
                "p99": _ms(percentile(ttfts, 0.99)),
            },
            "completion_ms": {
                "p50": _ms(percentile(completions, 0.50)),
                "p95": _ms(percentile(completions, 0.95)),
                "p99": _ms(percentile(completions, 0.99)),
            },
            "token_events": sum(item.tokens for item in items),
            "completed_streams": sum(1 for item in items if item.completed),
            "statuses": dict(Counter(str(item.status) for item in items)),
            "error_reasons": dict(Counter(item.error.split(":", 1)[0] for item in errors if item.error)),
            "error_samples": [item.error for item in errors[:10] if item.error],
        }


def _ms(value: float | None) -> float | None:
    return round(value * 1000, 2) if value is not None else None


class HTTPClient:
    def __init__(self, base_url: str, token: str | None = None, timeout: float = 30.0) -> None:
        self.base_url = base_url.rstrip("/")
        self.token = token
        self.timeout = timeout

    def request(
        self,
        method: str,
        path: str,
        body: dict[str, Any] | None = None,
        *,
        timeout: float | None = None,
        accept: str = "application/json",
    ) -> tuple[int, dict[str, str], bytes]:
        data = json.dumps(body, separators=(",", ":")).encode() if body is not None else None
        request = urllib.request.Request(self.base_url + path, data=data, method=method)
        request.add_header("Accept", accept)
        if body is not None:
            request.add_header("Content-Type", "application/json")
        if self.token:
            request.add_header("Authorization", f"Bearer {self.token}")
        try:
            with urllib.request.urlopen(request, timeout=timeout or self.timeout) as response:
                return response.status, dict(response.headers.items()), response.read()
        except urllib.error.HTTPError as error:
            return error.code, dict(error.headers.items()), error.read()

    def stream(
        self,
        method: str,
        path: str,
        body: dict[str, Any] | None = None,
        *,
        timeout: float | None = None,
        accept: str = "text/event-stream",
    ) -> tuple[Any, int, dict[str, str]]:
        """Open a response without buffering it; required for real SSE TTFT."""
        data = json.dumps(body, separators=(",", ":")).encode() if body is not None else None
        request = urllib.request.Request(self.base_url + path, data=data, method=method)
        request.add_header("Accept", accept)
        if body is not None:
            request.add_header("Content-Type", "application/json")
        if self.token:
            request.add_header("Authorization", f"Bearer {self.token}")
        try:
            response = urllib.request.urlopen(request, timeout=timeout or self.timeout)
            return response, response.status, dict(response.headers.items())
        except urllib.error.HTTPError as error:
            body_bytes = error.read()

            class ErrorResponse:
                def __init__(self, value: bytes) -> None:
                    self.value = value

                def read(self, _size: int = -1) -> bytes:
                    return self.value

                def close(self) -> None:
                    return None

            return ErrorResponse(body_bytes), error.code, dict(error.headers.items())

    def json(self, method: str, path: str, body: dict[str, Any] | None = None) -> tuple[int, dict[str, str], dict[str, Any]]:
        status, headers, raw = self.request(method, path, body)
        try:
            payload = json.loads(raw) if raw else {}
        except json.JSONDecodeError:
            payload = {"raw": raw.decode(errors="replace")}
        return status, headers, payload


def provision_users(base_url: str, count: int, password: str, timeout: float) -> list[str]:
    """Register one disposable account per virtual user and return access tokens."""
    client = HTTPClient(base_url, timeout=timeout)
    stamp = str(int(time.time()))
    tokens: list[str] = []
    for index in range(count):
        username = f"loadtest_{stamp}_{index}_{uuid.uuid4().hex[:6]}"
        email = f"{username}@loadtest.invalid"
        status, _, payload = client.json(
            "POST", "/api/v1/auth/register", {"username": username, "email": email, "password": password}
        )
        if status not in {200, 201} and status != 409:
            raise RuntimeError(f"register failed ({status}): {payload}")
        status, _, payload = client.json("POST", "/api/v1/auth/login", {"account": username, "password": password})
        token = payload.get("access_token")
        if status != 200 or not isinstance(token, str) or not token:
            raise RuntimeError(f"login failed ({status}): {payload}")
        auth_status, _, _ = HTTPClient(base_url, token, timeout).json("GET", "/api/v1/users/me")
        if auth_status != 200:
            raise RuntimeError(f"newly issued access token failed validation ({auth_status})")
        tokens.append(token)
    return tokens


def run_api(base_url: str, concurrency: int, duration: float, path: str, password: str, timeout: float) -> dict[str, Any]:
    tokens = provision_users(base_url, concurrency, password, timeout)
    samples = Samples()
    deadline = time.monotonic() + duration

    def worker(token: str) -> None:
        client = HTTPClient(base_url, token, timeout)
        while time.monotonic() < deadline:
            started = time.monotonic()
            try:
                status, _, payload = client.json("GET", path)
                error = None if 200 <= status < 400 else f"HTTP {status}: {str(payload)[:200]}"
                samples.add(Sample(time.monotonic() - started, status, error))
            except Exception as exc:  # noqa: BLE001 - preserve transport failures in the report
                samples.add(Sample(time.monotonic() - started, None, f"{type(exc).__name__}: {exc}"))

    with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
        list(pool.map(worker, tokens))
    return {"test": "api", "concurrency": concurrency, "duration_seconds": duration, "path": path, "summary": samples.summary(duration)}


def _source_code(language: str) -> str:
    if language == "go":
        return 'package main\nimport "fmt"\nfunc main(){fmt.Println(1)}\n'
    if language == "python":
        return "print(1)\n"
    if language == "cpp":
        return '#include <iostream>\nint main(){std::cout << 1 << "\\n";}\n'
    raise ValueError(f"unsupported load-test language: {language}")


def rabbit_snapshot(base_url: str, user: str, password: str, queues: Iterable[str]) -> dict[str, Any]:
    """Read queue depth from RabbitMQ management API; failures are reported, not hidden."""
    result: dict[str, Any] = {}
    auth = base64.b64encode(f"{user}:{password}".encode()).decode()
    for queue in queues:
        path = "/api/queues/%2F/" + urllib.parse.quote(queue, safe="")
        request = urllib.request.Request(base_url.rstrip("/") + path)
        request.add_header("Authorization", f"Basic {auth}")
        try:
            with urllib.request.urlopen(request, timeout=5) as response:
                payload = json.loads(response.read())
            result[queue] = {
                "messages": payload.get("messages"),
                "messages_ready": payload.get("messages_ready"),
                "messages_unacknowledged": payload.get("messages_unacknowledged"),
                "consumers": payload.get("consumers"),
            }
        except Exception as exc:  # noqa: BLE001
            result[queue] = {"error": f"{type(exc).__name__}: {exc}"}
    return result


def run_judge(
    base_url: str,
    rate: float,
    duration: float,
    problem_id: int,
    language: str,
    password: str,
    timeout: float,
    drain_timeout: float,
    rabbit_url: str | None,
    rabbit_user: str,
    rabbit_password: str,
) -> dict[str, Any]:
    tokens = provision_users(base_url, max(1, min(32, int(rate * 4) + 1)), password, timeout)
    submit_samples = Samples()
    submission_ids: list[tuple[int, float, str]] = []
    ids_lock = threading.Lock()
    started_at = time.monotonic()
    end_at = started_at + duration
    source = _source_code(language)

    def submit(sequence: int) -> None:
        token = tokens[sequence % len(tokens)]
        client = HTTPClient(base_url, token, timeout)
        started = time.monotonic()
        key = str(uuid.uuid4())
        try:
            status, _, payload = client.json(
                "POST",
                "/api/v1/submissions",
                {"problem_id": problem_id, "language": language, "source_code": source, "idempotency_key": key},
            )
            error = None if status in {200, 201} and payload.get("submission_id") else f"HTTP {status}: {str(payload)[:200]}"
            submission_id = payload.get("submission_id") if error is None else None
            if isinstance(submission_id, int):
                with ids_lock:
                    submission_ids.append((submission_id, time.monotonic(), token))
            submit_samples.add(Sample(time.monotonic() - started, status, error, submission_id=submission_id))
        except Exception as exc:  # noqa: BLE001
            submit_samples.add(Sample(time.monotonic() - started, None, f"{type(exc).__name__}: {exc}"))

    # A monotonic scheduler avoids the burstiness caused by sleeping after each request.
    sequence = 0
    queue_snapshots: list[dict[str, Any]] = []
    queue_names = [f"judge.task.{language}", "judge.results", "judge.dlq"]
    next_sample = started_at
    with concurrent.futures.ThreadPoolExecutor(max_workers=max(4, min(64, int(rate * 8) + 4))) as pool:
        while time.monotonic() < end_at:
            now = time.monotonic()
            if now >= next_sample:
                if rabbit_url:
                    queue_snapshots.append({"at": now_iso(), "queues": rabbit_snapshot(rabbit_url, rabbit_user, rabbit_password, queue_names)})
                next_sample += 1.0
            target = started_at + sequence / rate
            if now < target:
                time.sleep(min(target - now, 0.05))
                continue
            pool.submit(submit, sequence)
            sequence += 1
        pool.shutdown(wait=True)

    # Drain all accepted submissions and measure queue recovery/completion time.
    pending = {item[0]: item for item in submission_ids}
    terminal: dict[int, tuple[str, float]] = {}
    drain_deadline = time.monotonic() + drain_timeout
    while pending and time.monotonic() < drain_deadline:
        for submission_id, (_, accepted_at, token) in list(pending.items()):
            try:
                status, _, payload = HTTPClient(base_url, token, timeout).json("GET", f"/api/v1/submissions/{submission_id}")
                item = payload.get("submission", payload)
                raw_state = item.get("status", "")
                state = str(raw_state)
                if (
                    raw_state in TERMINAL_SUBMISSION_STATUS_CODES
                    or state in TERMINAL_SUBMISSION_STATUSES
                    or state.endswith("DONE")
                    or state.endswith("CANCELLED")
                    or state.endswith("INVALIDATED")
                ):
                    terminal[submission_id] = (state, time.monotonic() - accepted_at)
                    pending.pop(submission_id, None)
            except Exception:
                pass
        if pending:
            time.sleep(0.5)
    if rabbit_url:
        queue_snapshots.append({"at": now_iso(), "queues": rabbit_snapshot(rabbit_url, rabbit_user, rabbit_password, queue_names)})
    completion_values = [value[1] for value in terminal.values()]
    summary = submit_samples.summary(duration)
    summary.update({
        "accepted_submissions": len(submission_ids),
        "completed_submissions": len(terminal),
        "pending_after_drain": len(pending),
        "drain_timeout_seconds": drain_timeout,
        "completion_ms": {
            "p50": _ms(percentile(completion_values, 0.50)),
            "p95": _ms(percentile(completion_values, 0.95)),
            "p99": _ms(percentile(completion_values, 0.99)),
        },
    })
    return {"test": "judge", "rate_per_second": rate, "duration_seconds": duration, "problem_id": problem_id, "language": language, "summary": summary, "rabbitmq": queue_snapshots}


def consume_sse(response: Any, started: float) -> tuple[float | None, int, bool, str | None]:
    first_token: float | None = None
    tokens = 0
    completed = False
    event_name: str | None = None
    data_lines: list[str] = []
    while True:
        line = response.readline()
        if not line:
            break
        decoded = line.decode("utf-8", errors="replace").rstrip("\r\n") if isinstance(line, bytes) else line.rstrip("\r\n")
        if decoded.startswith(":"):
            continue
        if decoded.startswith("event:"):
            event_name = decoded[6:].strip()
        elif decoded.startswith("data:"):
            data_lines.append(decoded[5:].strip())
        elif decoded == "":
            if data_lines:
                try:
                    event = json.loads("\n".join(data_lines))
                    kind = str(event.get("type") or event_name or "")
                    if kind == "token":
                        tokens += 1
                        if first_token is None:
                            first_token = time.monotonic() - started
                    if kind == "done":
                        completed = True
                except json.JSONDecodeError:
                    return first_token, tokens, completed, "invalid SSE JSON"
            event_name, data_lines = None, []
    if not completed:
        return first_token, tokens, completed, "stream ended before done"
    return first_token, tokens, completed, None


def run_agent(base_url: str, concurrency: int, duration: float, message: str, password: str, timeout: float) -> dict[str, Any]:
    tokens = provision_users(base_url, concurrency, password, timeout)
    samples = Samples()
    deadline = time.monotonic() + duration

    def worker(token: str) -> None:
        client = HTTPClient(base_url, token, timeout=max(timeout, 180.0))
        while time.monotonic() < deadline:
            started = time.monotonic()
            try:
                # Keep the response open long enough to observe the terminal event.
                response, status, headers = client.stream("POST", "/api/v1/agent/chat", {"message": message}, timeout=max(timeout, 180.0), accept="text/event-stream")
                if status != 200:
                    error = f"HTTP {status}: {response.read(200)[:200].decode(errors='replace')}"
                    response.close()
                    samples.add(Sample(time.monotonic() - started, status, error))
                    continue
                try:
                    ttft, count, completed, error = consume_sse(response, started)
                finally:
                    response.close()
                elapsed = time.monotonic() - started
                samples.add(
                    Sample(
                        elapsed,
                        status,
                        error,
                        ttft=ttft,
                        tokens=count,
                        completed=completed,
                        completion_latency=elapsed if completed else None,
                    )
                )
            except Exception as exc:  # noqa: BLE001
                samples.add(Sample(time.monotonic() - started, None, f"{type(exc).__name__}: {exc}"))

    with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
        list(pool.map(worker, tokens))
    return {"test": "agent", "concurrency": concurrency, "duration_seconds": duration, "summary": samples.summary(duration)}


def print_result(result: dict[str, Any]) -> None:
    summary = result["summary"]
    label = result["test"]
    if label == "api":
        label += f" concurrency={result['concurrency']}"
    elif label == "judge":
        label += f" rate={result['rate_per_second']}/s"
    else:
        label += f" concurrency={result['concurrency']}"
    print(
        f"{label}: requests={summary['requests']} success={summary['successes']} "
        f"errors={summary['errors']} error_rate={summary['error_rate']:.3%} "
        f"rps={summary['throughput_rps']:.2f} p50={summary['latency_ms']['p50']}ms "
        f"p95={summary['latency_ms']['p95']}ms p99={summary['latency_ms']['p99']}ms"
    )
    if label.startswith("agent"):
        print(f"  TTFT p50/p95={summary['ttft_ms']['p50']}/{summary['ttft_ms']['p95']}ms token_events={summary['token_events']} completed={summary['completed_streams']}")
    if label.startswith("judge"):
        print(f"  accepted={summary['accepted_submissions']} completed={summary['completed_submissions']} pending_after_drain={summary['pending_after_drain']}")


def save_results(path: Path, base_url: str, results: list[dict[str, Any]]) -> None:
    path.write_text(
        json.dumps({"generated_at": now_iso(), "base_url": base_url, "results": results}, ensure_ascii=False, indent=2),
        encoding="utf-8",
    )


def parser() -> argparse.ArgumentParser:
    root = argparse.ArgumentParser(description=__doc__)
    root.add_argument("test", choices=["api", "judge", "agent", "all"])
    root.add_argument("--base-url", default=os.getenv("LOADTEST_BASE_URL", "http://127.0.0.1:8080"))
    root.add_argument("--duration", type=float, default=120.0)
    root.add_argument("--timeout", type=float, default=30.0)
    root.add_argument("--password", default=os.getenv("LOADTEST_PASSWORD", "Loadtest-password-1"))
    root.add_argument("--output", type=Path, default=Path("load-test-results.json"))
    root.add_argument("--concurrencies", default="10,30,50")
    root.add_argument("--rates", default="0.5,1,2")
    root.add_argument("--agent-message", default="请用一句话介绍二分查找。")
    root.add_argument("--api-path", default="/api/v1/problems?page.page=1&page.page_size=20")
    root.add_argument("--problem-id", type=int, default=int(os.getenv("LOADTEST_PROBLEM_ID", "0")))
    root.add_argument("--language", default="go")
    root.add_argument("--drain-timeout", type=float, default=180.0)
    root.add_argument("--rabbit-url", default=os.getenv("LOADTEST_RABBIT_URL", "http://127.0.0.1:15672"))
    root.add_argument("--rabbit-user", default=os.getenv("RABBITMQ_USER", "judge"))
    root.add_argument("--rabbit-password", default=os.getenv("RABBITMQ_PASSWORD", "judge-password"))
    root.add_argument("--no-rabbit", action="store_true")
    return root


def main() -> int:
    args = parser().parse_args()
    if args.test in {"judge", "all"} and args.problem_id <= 0:
        raise SystemExit("judge/all requires --problem-id or LOADTEST_PROBLEM_ID")
    results: list[dict[str, Any]] = []
    if args.test in {"api", "all"}:
        for concurrency in _numbers(args.concurrencies, int):
            result = run_api(args.base_url, concurrency, args.duration, args.api_path, args.password, args.timeout)
            print_result(result)
            results.append(result)
            save_results(args.output, args.base_url, results)
    if args.test in {"judge", "all"}:
        for rate in _numbers(args.rates, float):
            result = run_judge(args.base_url, rate, args.duration, args.problem_id, args.language, args.password, args.timeout, args.drain_timeout, None if args.no_rabbit else args.rabbit_url, args.rabbit_user, args.rabbit_password)
            print_result(result)
            results.append(result)
            save_results(args.output, args.base_url, results)
    if args.test in {"agent", "all"}:
        for concurrency in _numbers(args.concurrencies, int):
            result = run_agent(args.base_url, concurrency, args.duration, args.agent_message, args.password, args.timeout)
            print_result(result)
            results.append(result)
            save_results(args.output, args.base_url, results)
    save_results(args.output, args.base_url, results)
    print(f"raw report: {args.output}")
    return 0


def _numbers(value: str, converter: Any) -> list[Any]:
    return [converter(item.strip()) for item in value.split(",") if item.strip()]


if __name__ == "__main__":
    raise SystemExit(main())
