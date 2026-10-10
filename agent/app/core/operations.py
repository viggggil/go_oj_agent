"""公开操作白名单；委托绑定规范化的具体资源路径。"""

import re
from uuid import UUID

ADMIN_ROLES = frozenset({"admin", "system_admin", "agent_admin"})
KEY_PATTERN = r"[a-z][a-z0-9_]{1,63}"
_RESOURCE = re.compile(
    rf"/api/v1/admin/agent/(agents|prompts|skills)(?:/({KEY_PATTERN}))?"
    r"(?:/(versions|archive|restore|disable|enable)(?:/([0-9a-f-]{36}))?)?"
)
_SKILLS = re.compile(rf"/api/v1/agent/agents/({KEY_PATTERN})/skills")


def operation(method: str, path: str) -> str:
    allowed = False
    if path == "/api/v1/agent/chat":
        allowed = method == "POST"
    elif path in {
        "/api/v1/agent/agents",
        "/api/v1/admin/agent/model-options",
        "/api/v1/admin/agent/tools",
    } or _SKILLS.fullmatch(path):
        allowed = method == "GET"
    elif match := _RESOURCE.fullmatch(path):
        _, key, action, identifier = match.groups()
        if identifier is not None:
            try:
                allowed = (
                    key is not None
                    and action == "versions"
                    and method == "GET"
                    and str(UUID(identifier)) == identifier
                )
            except ValueError:
                pass
        elif action is not None:
            allowed = key is not None and (
                method == "GET" if action == "versions" else method == "POST"
            )
        else:
            allowed = method in ({"GET", "PUT"} if key else {"GET", "POST"})
    if not allowed:
        raise ValueError("Unsupported Agent operation")
    return f"HTTP {method} {path}"
