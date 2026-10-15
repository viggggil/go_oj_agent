"""管理员检索测试；所有来源在返回前重新检查发布/启用状态。"""

from typing import Any

from app.rag.clients import RagClients
from app.rag.models import SearchInput
from app.rag.store import KnowledgeStore


class KnowledgeService:
    def __init__(self, store: KnowledgeStore, clients: RagClients) -> None:
        self.store, self.clients = store, clients

    async def search(self, body: SearchInput) -> list[dict[str, Any]]:
        base_ids = [str(value) for value in body.base_ids]
        active = await self.store.active(base_ids, body.problem_id)
        rows = await self.clients.search(body.query, [row["id"] for row in active], body.top_k)
        allowed = {row["id"] for row in await self.store.active(base_ids, body.problem_id)}
        result = []
        for row in rows:
            payload = row["payload"]
            if payload["version_id"] not in allowed:
                continue
            result.append(
                {
                    "score": row["score"],
                    **{
                        key: payload[key]
                        for key in (
                            "base_id",
                            "document_id",
                            "version_id",
                            "title",
                            "problem_id",
                            "number",
                            "text",
                            "section",
                            "start",
                            "end",
                        )
                    },
                }
            )
        return result
