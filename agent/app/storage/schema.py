"""与 migrations/agent 一致的 SQLAlchemy Core 表定义。"""

from sqlalchemy import (
    CHAR,
    JSON,
    BigInteger,
    Boolean,
    Computed,
    ForeignKey,
    MetaData,
    String,
    Table,
)
from sqlalchemy.dialects.mysql import DATETIME, MEDIUMTEXT
from sqlalchemy.sql.schema import Column

metadata = MetaData()

agent_conversations = Table(
    "agent_conversations",
    metadata,
    Column("id", CHAR(36), primary_key=True),
    Column("user_id", BigInteger, nullable=False),
    Column("title", String(255)),
    Column("agent_key", String(64)),
    Column("created_at", DATETIME(fsp=3), nullable=False),
    Column("updated_at", DATETIME(fsp=3), nullable=False),
)

agent_runs = Table(
    "agent_runs",
    metadata,
    Column("id", CHAR(36), primary_key=True),
    Column(
        "conversation_id",
        CHAR(36),
        ForeignKey("agent_conversations.id", ondelete="RESTRICT"),
        nullable=False,
    ),
    Column("user_id", BigInteger, nullable=False),
    Column("request_id", String(128), nullable=False),
    Column("config_source", String(64), nullable=False),
    Column("config_snapshot", JSON, nullable=False),
    Column("status", String(32), nullable=False),
    Column("started_at", DATETIME(fsp=3), nullable=False),
    Column("finished_at", DATETIME(fsp=3)),
    Column("deadline_at", DATETIME(fsp=3), nullable=False),
    Column("error_code", String(64)),
    Column(
        "active_conversation_id",
        CHAR(36),
        Computed("CASE WHEN status = 'RUNNING' THEN conversation_id ELSE NULL END", persisted=True),
    ),
)

agent_messages = Table(
    "agent_messages",
    metadata,
    Column("id", BigInteger, primary_key=True, autoincrement=True),
    Column(
        "conversation_id",
        CHAR(36),
        ForeignKey("agent_conversations.id", ondelete="CASCADE"),
        nullable=False,
    ),
    Column("run_id", CHAR(36), ForeignKey("agent_runs.id", ondelete="SET NULL")),
    Column("role", String(32), nullable=False),
    Column("content", MEDIUMTEXT, nullable=False),
    Column("tool_name", String(128)),
    Column("created_at", DATETIME(fsp=3), nullable=False),
)

agent_config_resources = Table(
    "agent_config_resources",
    metadata,
    Column("id", CHAR(36), primary_key=True),
    Column("kind", String(16), nullable=False),
    Column("config_key", String(64, collation="utf8mb4_bin"), nullable=False),
    Column("current_id", CHAR(36)),
    Column("disabled", Boolean, nullable=False),
    Column("created_at", DATETIME(fsp=3), nullable=False),
)

agent_config_versions = Table(
    "agent_config_versions",
    metadata,
    Column("id", CHAR(36), primary_key=True),
    Column(
        "resource_id",
        CHAR(36),
        ForeignKey("agent_config_resources.id", ondelete="RESTRICT"),
        nullable=False,
    ),
    Column("previous_id", CHAR(36), ForeignKey("agent_config_versions.id", ondelete="RESTRICT")),
    Column("content", JSON, nullable=False),
    Column("created_by", BigInteger, nullable=False),
    Column("created_at", DATETIME(fsp=3), nullable=False),
    Column("archived_by", BigInteger),
    Column("archived_at", DATETIME(fsp=3)),
)

agent_config_links = Table(
    "agent_config_links",
    metadata,
    Column(
        "version_id",
        CHAR(36),
        ForeignKey("agent_config_versions.id", ondelete="RESTRICT"),
        primary_key=True,
    ),
    Column(
        "target_id",
        CHAR(36),
        ForeignKey("agent_config_versions.id", ondelete="RESTRICT"),
        primary_key=True,
    ),
)

agent_config_audits = Table(
    "agent_config_audits",
    metadata,
    Column("request_id", String(128, collation="utf8mb4_bin"), primary_key=True),
    Column("actor_id", BigInteger, nullable=False),
    Column("action", String(16), nullable=False),
    Column(
        "resource_id",
        CHAR(36),
        ForeignKey("agent_config_resources.id", ondelete="RESTRICT"),
        nullable=False,
    ),
    Column("old_id", CHAR(36), ForeignKey("agent_config_versions.id", ondelete="RESTRICT")),
    Column("new_id", CHAR(36), ForeignKey("agent_config_versions.id", ondelete="RESTRICT")),
    Column("fingerprint", String(64), nullable=False),
    Column("created_at", DATETIME(fsp=3), nullable=False),
)
