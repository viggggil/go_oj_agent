"""持久化任务租约；失租 worker 不得发布，重复向量写入幂等。"""

import asyncio
import contextlib
import hashlib
import logging
from typing import Any

from app.core.database import DatabaseProbe
from app.core.settings import load_settings
from app.models.configuration import ConfigError
from app.observability import span
from app.rag.clients import RagClients
from app.rag.models import REVISION, SPLITTER
from app.rag.store import KnowledgeStore

logger = logging.getLogger(__name__)


async def process_one(store: KnowledgeStore, clients: RagClients) -> bool:
    value = await store.claim()
    if value is None:
        return False

    async with span("agent.rag.ingest", {"agent.purpose": "rag_ingestion"}) as ingest_span:
        return await _process_claimed(store, clients, value, ingest_span)


async def _process_claimed(
    store: KnowledgeStore, clients: RagClients, value: dict[str, Any], ingest_span: Any
) -> bool:

    async def heartbeat() -> None:
        while True:
            await asyncio.sleep(15)
            if not await store.heartbeat(value["id"], value["lease_token"]):
                return

    heartbeat_task = asyncio.create_task(heartbeat())
    count = 0
    error = None
    try:
        async with asyncio.timeout(240):
            if value["model_revision"] != REVISION or value["splitter_version"] != SPLITTER:
                raise ConfigError("AGENT_RAG_INDEX_VERSION_INVALID", 503)
            async with span("agent.rag.ingest.fetch"):
                content = await clients.get(value["object_key"])
            if hashlib.sha256(content.encode()).hexdigest() != value["content_hash"]:
                raise ConfigError("AGENT_RAG_OBJECT_INVALID", 503)
            async with span("agent.rag.ingest.split"):
                chunks = await clients.split(content)
            ingest_span.set_attribute("rag.chunk_count", len(chunks))
            await clients.ensure_collection()
            for offset in range(0, len(chunks), 8):
                if not await store.heartbeat(value["id"], value["lease_token"]):
                    ingest_span.set_attribute("job.status", "lease_lost")
                    return True
                async with span("agent.rag.ingest.embed_upsert"):
                    await clients.upsert(value, chunks[offset : offset + 8])
            count = len(chunks)
    except asyncio.CancelledError:
        ingest_span.set_attribute("job.status", "cancelled")
        raise  # 进程停止保留租约，重启后过期重领。
    except Exception as exc:
        error = exc.code if isinstance(exc, ConfigError) else "AGENT_RAG_INDEX_FAILED"
        ingest_span.set_attribute("job.status", "failed")
        ingest_span.set_attribute("error.code", error)
        logger.warning("Knowledge index failed", extra={"error_code": error})
    finally:
        heartbeat_task.cancel()
        with contextlib.suppress(asyncio.CancelledError):
            await heartbeat_task
    await store.finish(value, count, error)
    if error is None:
        ingest_span.set_attribute("job.status", "completed")
    return True


async def main() -> None:
    settings = load_settings()
    database = DatabaseProbe(settings)
    assert database.engine is not None
    store, clients = KnowledgeStore(database.engine), RagClients(settings)
    try:
        while True:
            try:
                worked = await process_one(store, clients)
            except Exception as exc:
                logger.warning(
                    "Knowledge worker unavailable", extra={"error_type": type(exc).__name__}
                )
                worked = False
            if not worked:
                await asyncio.sleep(2)
    finally:
        await clients.close()
        await database.close()


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO)
    asyncio.run(main())
