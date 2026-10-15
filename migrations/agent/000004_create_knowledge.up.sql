USE oj_agent;

CREATE TABLE agent_knowledge_bases (
  id CHAR(36) NOT NULL PRIMARY KEY,
  name VARCHAR(128) NOT NULL,
  created_by BIGINT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  INDEX idx_knowledge_base_page(created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE agent_knowledge_documents (
  id CHAR(36) NOT NULL PRIMARY KEY,
  base_id CHAR(36) NOT NULL,
  current_version CHAR(36) NULL,
  active_version CHAR(36) NULL,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  created_at DATETIME(3) NOT NULL,
  FOREIGN KEY (base_id) REFERENCES agent_knowledge_bases(id),
  INDEX idx_knowledge_document_page(base_id, created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE agent_knowledge_versions (
  id CHAR(36) NOT NULL PRIMARY KEY,
  document_id CHAR(36) NOT NULL,
  title VARCHAR(128) NOT NULL,
  kind VARCHAR(24) NOT NULL,
  problem_id BIGINT NULL,
  source_url VARCHAR(512) NOT NULL,
  tags JSON NOT NULL,
  filename VARCHAR(128) NOT NULL,
  object_key VARCHAR(255) NOT NULL,
  content_hash CHAR(64) NOT NULL,
  size_bytes INT NOT NULL,
  model_revision CHAR(40) NOT NULL,
  splitter_version VARCHAR(32) NOT NULL,
  created_by BIGINT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  FOREIGN KEY (document_id) REFERENCES agent_knowledge_documents(id),
  CONSTRAINT chk_knowledge_kind CHECK(kind IN ('editorial','lecture','platform')),
  CONSTRAINT chk_knowledge_problem CHECK(problem_id IS NULL OR problem_id > 0),
  INDEX idx_knowledge_version_page(document_id, created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE agent_knowledge_documents
  ADD CONSTRAINT fk_knowledge_current FOREIGN KEY(current_version) REFERENCES agent_knowledge_versions(id),
  ADD CONSTRAINT fk_knowledge_active FOREIGN KEY(active_version) REFERENCES agent_knowledge_versions(id);

CREATE TABLE agent_knowledge_tasks (
  version_id CHAR(36) NOT NULL PRIMARY KEY,
  state VARCHAR(16) NOT NULL,
  attempts INT NOT NULL DEFAULT 0,
  lease_token CHAR(36) NULL,
  lease_until DATETIME(3) NULL,
  next_attempt DATETIME(3) NOT NULL,
  chunk_count INT NOT NULL DEFAULT 0,
  error_code VARCHAR(64) NULL,
  updated_at DATETIME(3) NOT NULL,
  FOREIGN KEY(version_id) REFERENCES agent_knowledge_versions(id),
  CONSTRAINT chk_knowledge_task_state CHECK(state IN ('PENDING','RUNNING','READY','FAILED')),
  INDEX idx_knowledge_task_claim(state, next_attempt, lease_until)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE agent_knowledge_requests (
  request_id CHAR(36) NOT NULL PRIMARY KEY,
  actor_id BIGINT NOT NULL,
  fingerprint CHAR(64) NOT NULL,
  response JSON NOT NULL,
  created_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
