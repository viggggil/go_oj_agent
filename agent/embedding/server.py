"""固定 revision 的本地中文 Embedding；运行阶段禁止网络下载。"""

import asyncio
import os
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, ConfigDict, Field
from sentence_transformers import SentenceTransformer

MODEL = "BAAI/bge-small-zh-v1.5"
REVISION = "7999e1d3359715c523056ef9478215996d62a620"
QUERY_PREFIX = "为这个句子生成表示以用于检索相关文章："
model = None
lock = asyncio.Lock()


@asynccontextmanager
async def lifespan(app):
    global model
    model = SentenceTransformer(os.environ.get("MODEL_PATH", "/models/bge-small-zh-v1.5"),
                                device="cpu", local_files_only=True)
    model.max_seq_length = 512
    yield


app = FastAPI(lifespan=lifespan, docs_url=None, redoc_url=None, openapi_url=None)


class EmbeddingInput(BaseModel):
    model_config = ConfigDict(extra="forbid")
    texts: list[str] = Field(min_length=1, max_length=8)
    query: bool = False


class SplitInput(BaseModel):
    model_config = ConfigDict(extra="forbid")
    content: str = Field(min_length=1, max_length=120_000)


@app.get("/healthz")
def health():
    return {"status": "ready", "revision": REVISION, "dimensions": 512}


@app.post("/embed")
async def embed(body: EmbeddingInput):
    texts = [(QUERY_PREFIX + text if body.query else text) for text in body.texts]
    if any(not text.strip() or len(text.encode("utf-8")) > 120_000 or len(model.tokenizer.encode(text)) > 512
           for text in texts):
        raise HTTPException(400, "Embedding input exceeds 512 tokens")
    # 单进程有限并发；CPU 线程数由容器环境限制。
    if lock.locked():
        raise HTTPException(503, "Embedding busy")
    async with lock:
        vectors = await asyncio.to_thread(model.encode, texts, batch_size=8,
                                          normalize_embeddings=True, show_progress_bar=False)
    return {"revision": REVISION, "vectors": vectors.tolist()}


def split_document(content, tokenizer):
    """按 tokenizer 偏移切片，保留原文（含代码空白）和章节；不截断长代码。"""
    tokens = tokenizer(content, add_special_tokens=False, return_offsets_mapping=True)["offset_mapping"]
    chunks = []
    offset = 0
    while offset < len(tokens):
        stop = min(offset + 360, len(tokens))
        start_char, end_char = tokens[offset][0], tokens[stop - 1][1]
        # 尽量在当前 token 窗口后半部分的段落/行末切分。
        if stop < len(tokens):
            floor = tokens[offset + 200][0]
            boundary = content.rfind("\n", floor, end_char)
            if boundary >= floor:
                while stop > offset + 200 and tokens[stop - 1][1] > boundary:
                    stop -= 1
                end_char = tokens[stop - 1][1]
        text = content[start_char:end_char]
        headings = [line.strip() for line in content[:start_char].splitlines()
                    if line.startswith("#")]
        chunks.append({"number": len(chunks), "text": text,
                       "section": headings[-1][:128] if headings else "正文",
                       "start": start_char, "end": end_char})
        if stop == len(tokens):
            break
        offset = stop - 48
    return chunks


@app.post("/split")
async def split(body: SplitInput):
    if len(body.content.encode()) > 120_000 or not body.content.strip():
        raise HTTPException(400, "Invalid document")
    return {"chunks": split_document(body.content, model.tokenizer)}
