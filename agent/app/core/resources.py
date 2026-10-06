"""每个应用实例拥有独立资源，由 FastAPI lifespan 管理。"""

from dataclasses import dataclass
from typing import cast

from fastapi import Request

from app.core.database import ReadinessProbe
from app.core.settings import Settings


@dataclass(frozen=True)
class AppResources:
    settings: Settings
    database: ReadinessProbe


def get_resources(request: Request) -> AppResources:
    return cast(AppResources, request.app.state.resources)
