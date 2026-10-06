"""测试隔离本机配置，真实数据库测试使用专门的 AGENT_TEST_* 变量。"""

import os
from pathlib import Path

import pytest


@pytest.fixture(autouse=True)
def isolated_settings(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> None:
    for name in os.environ:
        if name.startswith("AGENT_") and not name.startswith("AGENT_TEST_"):
            monkeypatch.delenv(name)
    monkeypatch.chdir(tmp_path)
