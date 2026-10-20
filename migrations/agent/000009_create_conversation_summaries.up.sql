USE oj_agent;

CREATE TABLE agent_conversation_summaries (
  conversation_id CHAR(36) NOT NULL,
  user_id BIGINT NOT NULL,
  purpose VARCHAR(16) NOT NULL DEFAULT 'chat',
  summary_json JSON NOT NULL,
  covered_until_message_id BIGINT NULL,
  schema_version INT NOT NULL DEFAULT 1,
  version INT NOT NULL DEFAULT 1,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (conversation_id),
  KEY idx_agent_summaries_owner_purpose (user_id, purpose, updated_at, conversation_id),
  CONSTRAINT chk_agent_summary_owner CHECK (user_id > 0),
  CONSTRAINT chk_agent_summary_purpose CHECK (purpose IN ('chat', 'eval')),
  CONSTRAINT chk_agent_summary_schema_version CHECK (schema_version > 0),
  CONSTRAINT chk_agent_summary_version CHECK (version > 0),
  CONSTRAINT chk_agent_summary_cursor CHECK (
    covered_until_message_id IS NULL OR covered_until_message_id > 0
  ),
  CONSTRAINT fk_agent_summary_conversation FOREIGN KEY (conversation_id)
    REFERENCES agent_conversations(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
