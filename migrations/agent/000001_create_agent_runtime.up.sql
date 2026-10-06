CREATE DATABASE IF NOT EXISTS oj_agent
  DEFAULT CHARACTER SET utf8mb4
  DEFAULT COLLATE utf8mb4_0900_ai_ci;

USE oj_agent;

CREATE TABLE agent_conversations (
  id CHAR(36) NOT NULL,
  user_id BIGINT NOT NULL,
  title VARCHAR(255) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  CONSTRAINT chk_agent_conversations_owner CHECK (user_id > 0),
  KEY idx_agent_conversations_user_updated_id (user_id, updated_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE agent_runs (
  id CHAR(36) NOT NULL,
  conversation_id CHAR(36) NOT NULL,
  user_id BIGINT NOT NULL,
  request_id VARCHAR(128) NOT NULL,
  config_source VARCHAR(64) NOT NULL,
  config_snapshot JSON NOT NULL,
  status VARCHAR(32) NOT NULL,
  started_at DATETIME(3) NOT NULL,
  finished_at DATETIME(3) NULL,
  deadline_at DATETIME(3) NOT NULL,
  error_code VARCHAR(64) NULL,
  active_conversation_id CHAR(36)
    GENERATED ALWAYS AS (
      CASE WHEN status = 'RUNNING' THEN conversation_id ELSE NULL END
    ) STORED,
  PRIMARY KEY (id),
  KEY idx_agent_runs_conversation_started (conversation_id, started_at, id),
  KEY idx_agent_runs_user_started (user_id, started_at, id),
  KEY idx_agent_runs_status_deadline (status, deadline_at),
  UNIQUE KEY uk_agent_runs_one_active_conversation (active_conversation_id),
  CONSTRAINT chk_agent_runs_owner CHECK (user_id > 0),
  CONSTRAINT chk_agent_runs_status CHECK (
    status IN ('RUNNING', 'COMPLETED', 'FAILED', 'CANCELLED', 'INTERRUPTED')
  ),
  CONSTRAINT fk_agent_runs_conversation
    FOREIGN KEY (conversation_id) REFERENCES agent_conversations(id)
    ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE agent_messages (
  id BIGINT NOT NULL AUTO_INCREMENT,
  conversation_id CHAR(36) NOT NULL,
  run_id CHAR(36) NULL,
  role VARCHAR(32) NOT NULL,
  content MEDIUMTEXT NOT NULL,
  tool_name VARCHAR(128) NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  KEY idx_agent_messages_conversation_id (conversation_id, id),
  KEY idx_agent_messages_run_id (run_id),
  CONSTRAINT chk_agent_messages_role CHECK (role IN ('user', 'assistant', 'tool')),
  CONSTRAINT chk_agent_messages_size CHECK (CHAR_LENGTH(content) BETWEEN 1 AND 32000),
  CONSTRAINT fk_agent_messages_conversation
    FOREIGN KEY (conversation_id) REFERENCES agent_conversations(id)
    ON DELETE CASCADE,
  CONSTRAINT fk_agent_messages_run
    FOREIGN KEY (run_id) REFERENCES agent_runs(id)
    ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
