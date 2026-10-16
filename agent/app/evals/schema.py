"""Eval 数据集表定义；DDL 由 migrations/agent 负责。"""

from sqlalchemy import BigInteger, Boolean, Column, Integer, String, Table
from sqlalchemy.dialects.mysql import CHAR, DATETIME, JSON, MEDIUMTEXT

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

runs = Table(
    "agent_eval_runs",
    metadata,
    Column("id", CHAR(36), primary_key=True),
    Column("dataset_id", CHAR(36), nullable=False),
    Column("agent_key", String(64), nullable=False),
    Column("mode", String(32), nullable=False),
    Column("repeat_count", Integer, nullable=False),
    Column("concurrency", Integer, nullable=False),
    Column("status", String(16), nullable=False),
    Column("total_cases", Integer, nullable=False),
    Column("completed_cases", Integer, nullable=False),
    Column("hard_failures", Integer, nullable=False),
    Column("error_count", Integer, nullable=False),
    Column("config_snapshot", JSON, nullable=False),
    Column("created_by", BigInteger, nullable=False),
    Column("request_id", String(128, collation="utf8mb4_bin"), nullable=False, unique=True),
    Column("fingerprint", CHAR(64), nullable=False),
    Column("cancel_request_id", CHAR(36)),
    Column("cancel_requested", Boolean, nullable=False),
    Column("created_at", DATETIME(fsp=3), nullable=False),
    Column("started_at", DATETIME(fsp=3)),
    Column("finished_at", DATETIME(fsp=3)),
    Column("version", Integer, nullable=False),
)

results = Table(
    "agent_eval_results",
    metadata,
    Column("id", CHAR(36), primary_key=True),
    Column("run_id", CHAR(36), nullable=False),
    Column("case_id", String(128, collation="utf8mb4_bin"), nullable=False),
    Column("attempt", Integer, nullable=False),
    Column("status", String(16), nullable=False),
    Column("review_version", Integer, nullable=False, server_default="0"),
    Column("manual_review", JSON),
    Column("answer", MEDIUMTEXT, nullable=True),
    Column("checks", JSON, nullable=False),
    Column("model_summary", JSON),
    Column("error_code", String(64)),
    Column("latency_ms", Integer),
    Column("created_at", DATETIME(fsp=3), nullable=False),
    Column("finished_at", DATETIME(fsp=3)),
)


reviews = Table(
    "agent_eval_reviews",
    metadata,
    Column("id", CHAR(36), primary_key=True),
    Column("result_id", CHAR(36), nullable=False),
    Column("version", Integer, nullable=False),
    Column("reviewed_by", BigInteger, nullable=False),
    Column("request_id", CHAR(36), nullable=False, unique=True),
    Column("fingerprint", CHAR(64), nullable=False),
    Column("old_value", JSON),
    Column("new_value", JSON, nullable=False),
    Column("reason", String(512), nullable=False),
    Column("created_at", DATETIME(fsp=3), nullable=False),
)
