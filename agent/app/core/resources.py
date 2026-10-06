"""每个应用实例拥有独立资源，由 FastAPI lifespan 管理。"""

from dataclasses import dataclass
from typing import TYPE_CHECKING, cast

from fastapi import Request

from app.core.database import ReadinessProbe
from app.core.settings import Settings

if TYPE_CHECKING:
    from app.api.chat import ChatController


@dataclass(frozen=True)
class AppResources:
    settings: Settings
    database: ReadinessProbe
    chat: "ChatController | None" = None


def get_resources(request: Request) -> AppResources:
    return cast(AppResources, request.app.state.resources)
