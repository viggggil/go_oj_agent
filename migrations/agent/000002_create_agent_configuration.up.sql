USE oj_agent;

CREATE TABLE agent_config_resources (
  id CHAR(36) NOT NULL,
  kind VARCHAR(16) NOT NULL,
  config_key VARCHAR(64) COLLATE utf8mb4_bin NOT NULL,
  current_id CHAR(36) NULL,
  disabled BOOLEAN NOT NULL DEFAULT FALSE,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_agent_config_resource_key (kind, config_key),
  CONSTRAINT chk_agent_config_kind CHECK (kind IN ('agent', 'prompt', 'skill', 'model'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE agent_config_versions (
  id CHAR(36) NOT NULL,
  resource_id CHAR(36) NOT NULL,
  previous_id CHAR(36) NULL,
  content JSON NOT NULL,
  created_by BIGINT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  archived_by BIGINT NULL,
  archived_at DATETIME(3) NULL,
  PRIMARY KEY (id),
  KEY idx_agent_config_versions_resource_created (resource_id, created_at, id),
  CONSTRAINT chk_agent_config_created_by CHECK (created_by > 0),
  CONSTRAINT fk_agent_config_version_resource FOREIGN KEY (resource_id)
    REFERENCES agent_config_resources(id) ON DELETE RESTRICT,
  CONSTRAINT fk_agent_config_version_previous FOREIGN KEY (previous_id)
    REFERENCES agent_config_versions(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE agent_config_resources ADD CONSTRAINT fk_agent_config_current
  FOREIGN KEY (current_id) REFERENCES agent_config_versions(id) ON DELETE RESTRICT;

CREATE TABLE agent_config_links (
  version_id CHAR(36) NOT NULL,
  target_id CHAR(36) NOT NULL,
  PRIMARY KEY (version_id, target_id),
  KEY idx_agent_config_links_target (target_id),
  CONSTRAINT fk_agent_config_link_version FOREIGN KEY (version_id)
    REFERENCES agent_config_versions(id) ON DELETE RESTRICT,
  CONSTRAINT fk_agent_config_link_target FOREIGN KEY (target_id)
    REFERENCES agent_config_versions(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE agent_config_audits (
  request_id VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
  actor_id BIGINT NOT NULL,
  action VARCHAR(16) NOT NULL,
  resource_id CHAR(36) NOT NULL,
  old_id CHAR(36) NULL,
  new_id CHAR(36) NULL,
  fingerprint CHAR(64) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  PRIMARY KEY (request_id),
  KEY idx_agent_config_audits_resource_created (resource_id, created_at, request_id),
  CONSTRAINT chk_agent_config_audit_actor CHECK (actor_id > 0),
  CONSTRAINT chk_agent_config_action CHECK (action IN ('create', 'replace', 'archive', 'disable', 'enable')),
  CONSTRAINT fk_agent_config_audit_resource FOREIGN KEY (resource_id)
    REFERENCES agent_config_resources(id) ON DELETE RESTRICT,
  CONSTRAINT fk_agent_config_audit_old FOREIGN KEY (old_id)
    REFERENCES agent_config_versions(id) ON DELETE RESTRICT,
  CONSTRAINT fk_agent_config_audit_new FOREIGN KEY (new_id)
    REFERENCES agent_config_versions(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE agent_conversations ADD COLUMN agent_key VARCHAR(64) COLLATE utf8mb4_bin NULL;
UPDATE agent_conversations SET agent_key = 'demo' WHERE agent_key IS NULL;
