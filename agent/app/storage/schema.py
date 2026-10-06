"""与 migrations/agent 一致的 SQLAlchemy Core 表定义。"""

from sqlalchemy import CHAR, JSON, BigInteger, Computed, ForeignKey, MetaData, String, Table
from sqlalchemy.dialects.mysql import DATETIME, MEDIUMTEXT
from sqlalchemy.sql.schema import Column

metadata = MetaData()

agent_conversations = Table(
    "agent_conversations",
    metadata,
    Column("id", CHAR(36), primary_key=True),
    Column("user_id", BigInteger, nullable=False),
    Column("title", String(255)),
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
