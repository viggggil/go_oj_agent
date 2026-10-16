"""Eval 数据集表定义；DDL 由 migrations/agent 负责。"""

from sqlalchemy import BigInteger, Column, Integer, String, Table
from sqlalchemy.dialects.mysql import CHAR, DATETIME, JSON

from app.storage.schema import metadata

datasets = Table(
    "agent_eval_datasets",
    metadata,
    Column("id", CHAR(36), primary_key=True),
    Column("name", String(128), nullable=False),
    Column("description", String(512), nullable=False),
    Column("format_version", String(16), nullable=False),
    Column("content_hash", CHAR(64), nullable=False, unique=True),
    Column("object_key", String(255), nullable=False),
    Column("case_count", Integer, nullable=False),
    Column("created_by", BigInteger, nullable=False),
    Column("created_at", DATETIME(fsp=3), nullable=False),
    Column("status", String(16), nullable=False),
)

cases = Table(
    "agent_eval_cases",
    metadata,
    Column("dataset_id", CHAR(36), primary_key=True),
    Column("case_id", String(128), primary_key=True),
    Column("category", String(64), nullable=False),
    Column("title", String(256), nullable=False),
    Column("case_json", JSON, nullable=False),
    Column("created_at", DATETIME(fsp=3), nullable=False),
)
