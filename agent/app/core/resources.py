"""每个应用实例拥有独立资源，由 FastAPI lifespan 管理。"""

from dataclasses import dataclass
from typing import TYPE_CHECKING, cast

from fastapi import Request

from app.clients.business import BusinessClients
from app.core.database import ReadinessProbe
from app.core.settings import Settings
from app.tools.registry import ToolRegistry

if TYPE_CHECKING:
    from app.api.chat import ChatController
    from app.core.auth import DelegationVerifier
    from app.core.management import ManagementService


@dataclass(frozen=True)
class AppResources:
    settings: Settings
    database: ReadinessProbe
    chat: "ChatController | None" = None
    business_clients: BusinessClients | None = None
    tools: ToolRegistry | None = None
    management: "ManagementService | None" = None
    verifier: "DelegationVerifier | None" = None


def get_resources(request: Request) -> AppResources:
    return cast(AppResources, request.app.state.resources)
