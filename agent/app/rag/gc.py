"""有界孤立对象清理，默认 dry-run；不删除已引用原文或任何发布版本。

python -m app.rag.gc BASE_UUID [--apply] [--start-after OBJECT_KEY]
上传与清理持有同一知识库行锁，避免检查后上传/登记与删除竞态。
"""

import argparse
import asyncio
from datetime import UTC, datetime, timedelta
from itertools import islice
from uuid import UUID

from sqlalchemy import select

from app.core.database import DatabaseProbe
from app.core.settings import load_settings
from app.rag.clients import RagClients
from app.rag.schema import bases, versions


async def run(base_id: UUID, apply: bool, start_after: str) -> None:
    database = DatabaseProbe(load_settings())
    assert database.engine is not None
    clients = RagClients(load_settings())
    prefix = f"bases/{base_id}/"
    try:
        candidates = await asyncio.to_thread(
            lambda: list(
                islice(
                    clients.objects.list_objects(
                        clients.bucket,
                        prefix=prefix,
                        recursive=True,
                        start_after=start_after or None,
                    ),
                    500,
                )
            )
        )
        for candidate in candidates:
            key = candidate.object_name
            if key is None:
                continue
            async with database.engine.begin() as conn:
                exists = (
                    await conn.execute(
                        select(bases.c.id)
                        .where(
                            bases.c.id == str(base_id),
                        )
                        .with_for_update()
                    )
                ).scalar_one_or_none()
                if exists is None:
                    raise ValueError("Unknown knowledge base")
                referenced = (
                    await conn.execute(
                        select(versions.c.id)
                        .where(
                            versions.c.object_key == key,
                        )
                        .limit(1)
                    )
                ).scalar_one_or_none()
                if referenced:
                    continue
                stat = await asyncio.to_thread(clients.objects.stat_object, clients.bucket, key)
                if stat.last_modified is None or stat.last_modified > datetime.now(UTC) - timedelta(
                    days=1
                ):
                    continue
                if apply:
                    await asyncio.to_thread(clients.objects.remove_object, clients.bucket, key)
                print(("removed " if apply else "orphan ") + key)
        if candidates:
            print("next start-after: " + str(candidates[-1].object_name))
    finally:
        await clients.close()
        await database.close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("base_id", type=UUID)
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--start-after", default="")
    args = parser.parse_args()
    asyncio.run(run(args.base_id, args.apply, args.start_after))
