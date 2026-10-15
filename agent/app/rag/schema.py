"""RAG 表的 SQLAlchemy 定义；DDL 由迁移负责。"""

from sqlalchemy import BigInteger, Boolean, Column, Integer, String, Table
from sqlalchemy.dialects.mysql import CHAR, DATETIME, JSON

from app.storage.schema import metadata

bases = Table(
    "agent_knowledge_bases",
    metadata,
    Column("id", CHAR(36), primary_key=True),
    Column("name", String(128), nullable=False),
    Column("created_by", BigInteger, nullable=False),
    Column("created_at", DATETIME(fsp=3), nullable=False),
)
documents = Table(
    "agent_knowledge_documents",
    metadata,
    Column("id", CHAR(36), primary_key=True),
    Column("base_id", CHAR(36), nullable=False),
    Column("current_version", CHAR(36)),
    Column("active_version", CHAR(36)),
    Column("enabled", Boolean, nullable=False),
    Column("created_at", DATETIME(fsp=3), nullable=False),
)
versions = Table(
    "agent_knowledge_versions",
    metadata,
    Column("id", CHAR(36), primary_key=True),
    Column("document_id", CHAR(36), nullable=False),
    Column("title", String(128), nullable=False),
    Column("kind", String(24), nullable=False),
    Column("problem_id", BigInteger),
    Column("source_url", String(512), nullable=False),
    Column("tags", JSON, nullable=False),
    Column("filename", String(128), nullable=False),
    Column("object_key", String(255), nullable=False),
    Column("content_hash", CHAR(64), nullable=False),
    Column("size_bytes", Integer, nullable=False),
    Column("model_revision", CHAR(40), nullable=False),
    Column("splitter_version", String(32), nullable=False),
    Column("created_by", BigInteger, nullable=False),
    Column("created_at", DATETIME(fsp=3), nullable=False),
)
tasks = Table(
    "agent_knowledge_tasks",
    metadata,
    Column("version_id", CHAR(36), primary_key=True),
    Column("state", String(16), nullable=False),
    Column("attempts", Integer, nullable=False),
    Column("lease_token", CHAR(36)),
    Column("lease_until", DATETIME(fsp=3)),
    Column("next_attempt", DATETIME(fsp=3), nullable=False),
    Column("chunk_count", Integer, nullable=False),
    Column("error_code", String(64)),
    Column("updated_at", DATETIME(fsp=3), nullable=False),
)
requests = Table(
    "agent_knowledge_requests",
    metadata,
    Column("request_id", CHAR(36), primary_key=True),
    Column("actor_id", BigInteger, nullable=False),
    Column("fingerprint", CHAR(64), nullable=False),
    Column("response", JSON, nullable=False),
    Column("created_at", DATETIME(fsp=3), nullable=False),
)
