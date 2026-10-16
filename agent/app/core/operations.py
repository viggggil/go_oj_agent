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
_MESSAGES = re.compile(r"/api/v1/agent/conversations/([0-9a-f-]{36})/messages")
_MODEL = re.compile(r"/api/v1/admin/agent/model-options/([0-9a-f-]{36})")
_VERSION = re.compile(r"/api/v1/admin/agent/(agents|prompts|skills)/versions/([0-9a-f-]{36})")


def operation(method: str, path: str) -> str:
    allowed = False
    if path == "/api/v1/agent/chat":
        allowed = method == "POST"
    elif path in {
        "/api/v1/agent/agents",
        "/api/v1/agent/conversations",
        "/api/v1/admin/agent/model-options",
        "/api/v1/admin/agent/tools",
    } or _SKILLS.fullmatch(path):
        allowed = method == "GET"
    elif conversation := _MESSAGES.fullmatch(path):
        try:
            allowed = method == "GET" and str(UUID(conversation[1])) == conversation[1]
        except ValueError:
            pass
    elif model := _MODEL.fullmatch(path):
        try:
            allowed = method == "GET" and str(UUID(model[1])) == model[1]
        except ValueError:
            pass
    elif version := _VERSION.fullmatch(path):
        try:
            allowed = method == "GET" and str(UUID(version[2])) == version[2]
        except ValueError:
            pass
    elif path.startswith("/api/v1/admin/agent/rag/"):
        tail = path.removeprefix("/api/v1/admin/agent/rag/")
        if tail in {"knowledge-bases", "documents"}:
            allowed = method in {"GET", "POST"}
        elif tail in {"search", "preview"}:
            allowed = method == "POST"
        elif run := re.fullmatch(r"runs/([0-9a-f-]{36})/evidence", tail):
            allowed = method == "GET" and str(UUID(run[1])) == run[1]
        elif group := re.fullmatch(r"knowledge-bases/([0-9a-f-]{36})(/rename)?", tail):
            allowed = str(UUID(group[1])) == group[1] and method == ("POST" if group[2] else "GET")
        elif match_rag := re.fullmatch(
            r"documents/([0-9a-f-]{36})(?:/(enable|archive|retry|versions))?", tail
        ):
            allowed = str(UUID(match_rag[1])) == match_rag[1] and (
                method == ("POST" if match_rag[2] in {"enable", "archive", "retry"} else "GET")
            )
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
