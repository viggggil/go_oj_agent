"""启动错误和进程信号测试，无需模型或数据库。"""

import json
import os
import re
import selectors
import signal
import subprocess
import sys
import time
from pathlib import Path

import httpx


def test_invalid_environment_exits_without_secret(tmp_path: Path) -> None:
    environment = dict(os.environ)
    environment["AGENT_DATABASE_URL"] = "mysql+asyncmy://agent:private-cli-password@db:bad/oj_agent"
    result = subprocess.run(
        [sys.executable, "-m", "app"],
        cwd=tmp_path,
        env=environment,
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert result.returncode == 2
    assert "private-cli-password" not in result.stderr
    assert "Traceback" not in result.stderr
    entry = json.loads(result.stderr.strip())
    assert entry["level"] == "ERROR"
    assert "database_url" in entry["message"]


def test_server_starts_and_stops_on_sigterm(tmp_path: Path) -> None:
    # 由操作系统分配端口；用 Uvicorn 的启动日志作为同步点，不依赖固定 sleep。
    environment = dict(os.environ)
    process = subprocess.Popen(
        [sys.executable, "-m", "uvicorn", "app.main:create_app", "--factory", "--port", "0"],
        cwd=tmp_path,
        env=environment,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
    )
    selector = selectors.DefaultSelector()
    try:
        assert process.stderr is not None
        selector.register(process.stderr, selectors.EVENT_READ)
        output = b""
        address: str | None = None
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            if not selector.select(timeout=max(0, deadline - time.monotonic())):
                raise AssertionError("server did not report startup")
            chunk = os.read(process.stderr.fileno(), 4096)
            if not chunk:
                raise AssertionError("server exited before startup: " + output.decode())
            output += chunk
            match = re.search(rb"Uvicorn running on (http://127\.0\.0\.1:\d+)", output)
            if match:
                address = match[1].decode()
                break
        assert address is not None
        response = httpx.get(address + "/healthz", timeout=3)
        assert response.status_code == 200
        assert httpx.get(address + "/readyz", timeout=3).status_code == 503
        process.send_signal(signal.SIGTERM)
        _, remaining = process.communicate(timeout=10)
        assert b"Application shutdown complete" in remaining
        # Uvicorn 会重新触发 SIGTERM，也可能正常返回。
        assert process.returncode in (0, -signal.SIGTERM)
    finally:
        selector.close()
        if process.poll() is None:
            process.kill()
            process.communicate(timeout=5)
