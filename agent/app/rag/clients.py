"""有界 HTTP 向量服务及 Agent 自有 MinIO 对象；错误不携带第三方正文。"""

import asyncio
import io
import math
from typing import Any
from uuid import NAMESPACE_URL, uuid5

import httpx
from minio import Minio
from minio.error import S3Error
from urllib3 import PoolManager
from urllib3.exceptions import HTTPError
from urllib3.util import Timeout

from app.core.settings import Settings
from app.models.configuration import ConfigError
from app.rag.models import DIMENSIONS, REVISION

COLLECTION = "oj_knowledge_bge_small_zh_v1_5"


class RagClients:
    def __init__(self, settings: Settings) -> None:
        self.http = httpx.AsyncClient(timeout=20, trust_env=False, follow_redirects=False)
        self.embed_url = settings.rag_embedding_url.rstrip("/")
        self.vector_url = settings.rag_qdrant_url.rstrip("/")
        self.bucket = settings.rag_minio_bucket
        self.objects = Minio(
            settings.rag_minio_endpoint,
            access_key=settings.rag_minio_access_key.get_secret_value(),
            secret_key=settings.rag_minio_secret_key.get_secret_value(),
            secure=settings.rag_minio_secure,
            http_client=PoolManager(timeout=Timeout(connect=3, read=15), retries=0),
        )

    async def close(self) -> None:
        await self.http.aclose()

    async def call(self, method: str, url: str, body: Any = None) -> dict[str, Any]:
        try:
            async with self.http.stream(method, url, json=body) as response:
                response.raise_for_status()
                data = bytearray()
                async for chunk in response.aiter_bytes():
                    if len(data) + len(chunk) > 2_000_000:
                        raise ValueError("Oversized response")
                    data.extend(chunk)
                import json

                result: dict[str, Any] = json.loads(data)
                if not isinstance(result, dict):
                    raise ValueError("Invalid response")
                return result
        except (httpx.HTTPError, ValueError):
            raise ConfigError("AGENT_RAG_DEPENDENCY_UNAVAILABLE", 503) from None

    async def embed(self, texts: list[str], *, query: bool = False) -> list[list[float]]:
        response = await self.call(
            "POST", f"{self.embed_url}/embed", {"texts": texts, "query": query}
        )
        vectors = response.get("vectors")
        if (
            response.get("revision") != REVISION
            or not isinstance(vectors, list)
            or len(vectors) != len(texts)
            or any(
                not isinstance(vector, list)
                or len(vector) != DIMENSIONS
                or any(not isinstance(x, (float, int)) or not math.isfinite(x) for x in vector)
                for vector in vectors
            )
        ):
            raise ConfigError("AGENT_RAG_DEPENDENCY_UNAVAILABLE", 503)
        return vectors

    async def split(self, content: str) -> list[dict[str, Any]]:
        response = await self.call("POST", f"{self.embed_url}/split", {"content": content})
        chunks = response.get("chunks")
        if not isinstance(chunks, list) or not 1 <= len(chunks) <= 1000:
            raise ConfigError("AGENT_RAG_DEPENDENCY_UNAVAILABLE", 503)
        return chunks

    async def ensure_collection(self) -> None:
        # Qdrant 冲突表示已存在；仍检查维度/距离，禁止混写不兼容集合。
        result = await self.http.put(
            f"{self.vector_url}/collections/{COLLECTION}",
            json={"vectors": {"size": DIMENSIONS, "distance": "Cosine"}},
        )
        if result.status_code not in (200, 409):
            raise ConfigError("AGENT_RAG_DEPENDENCY_UNAVAILABLE", 503)
        existing = await self.call("GET", f"{self.vector_url}/collections/{COLLECTION}")
        config = existing["result"]["config"]["params"]["vectors"]
        if config != {"size": DIMENSIONS, "distance": "Cosine"}:
            if config.get("size") != DIMENSIONS or config.get("distance") != "Cosine":
                raise ConfigError("AGENT_RAG_DEPENDENCY_UNAVAILABLE", 503)
        for field in ("base_id", "version_id", "document_id"):
            await self.call(
                "PUT",
                f"{self.vector_url}/collections/{COLLECTION}/index",
                {
                    "field_name": field,
                    "field_schema": "keyword",
                },
            )

    async def upsert(self, version: dict[str, Any], chunks: list[dict[str, Any]]) -> None:
        vectors = await self.embed([str(chunk["text"]) for chunk in chunks])
        points = [
            {
                "id": str(uuid5(NAMESPACE_URL, f"oj-rag:{version['id']}:{chunk['number']}")),
                "vector": vector,
                "payload": {
                    "base_id": version["base_id"],
                    "document_id": version["document_id"],
                    "version_id": version["id"],
                    "title": version["title"],
                    "problem_id": version["problem_id"],
                }
                | chunk,
            }
            for chunk, vector in zip(chunks, vectors, strict=True)
        ]
        await self.call(
            "PUT",
            f"{self.vector_url}/collections/{COLLECTION}/points?wait=true",
            {
                "points": points,
            },
        )

    async def search(self, query: str, version_ids: list[str], top_k: int) -> list[dict[str, Any]]:
        if not version_ids:
            return []
        vector = (await self.embed([query], query=True))[0]
        response = await self.call(
            "POST",
            f"{self.vector_url}/collections/{COLLECTION}/points/search",
            {
                "vector": vector,
                "limit": top_k,
                "with_payload": True,
                "filter": {"must": [{"key": "version_id", "match": {"any": version_ids}}]},
            },
        )
        result: list[dict[str, Any]] = response["result"]
        return result

    async def put(self, key: str, content: bytes) -> None:
        try:
            await asyncio.to_thread(
                self.objects.put_object,
                self.bucket,
                key,
                io.BytesIO(content),
                len(content),
                content_type="text/plain; charset=utf-8",
            )
        except (S3Error, HTTPError, OSError):
            raise ConfigError("AGENT_RAG_DEPENDENCY_UNAVAILABLE", 503) from None

    async def get(self, key: str) -> str:
        def read() -> str:
            response = self.objects.get_object(self.bucket, key)
            try:
                data = response.read(120_001)
                if len(data) > 120_000:
                    raise ValueError("Oversized stored document")
                return data.decode("utf-8")
            finally:
                response.close()
                response.release_conn()

        try:
            return await asyncio.to_thread(read)
        except (S3Error, HTTPError, OSError, ValueError):
            raise ConfigError("AGENT_RAG_DEPENDENCY_UNAVAILABLE", 503) from None
